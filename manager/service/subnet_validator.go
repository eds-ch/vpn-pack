package service

import (
	"fmt"
	"log/slog"
	"net"

	"github.com/jsimonetti/rtnetlink"
	"golang.org/x/sys/unix"
)

type InterfaceSubnet struct {
	CIDR      string
	Interface string
}

type RouteSubnet struct {
	CIDR      string
	Interface string
	Gateway   string
	Protocol  string
}

// tailscaleRouteTable is Tailscale's policy routing table (ip rule 5270, priority above main).
const tailscaleRouteTable = 52

const (
	pbrPriorityMin = 32500
	pbrPriorityMax = 32766
)

type PBRInfo struct {
	RulePriority uint32
	FwMark       uint32
	FwMask       uint32
	Table        uint32
}

type SystemSubnets struct {
	Interfaces    []InterfaceSubnet
	Routes        []RouteSubnet
	Table52Routes []RouteSubnet
	PBRRules      []PBRInfo
}

type ValidationResult struct {
	Blocked  []SubnetConflict `json:"blocked,omitempty"`
	Warnings []SubnetConflict `json:"warnings,omitempty"`
}

func (r *ValidationResult) HasBlocks() bool {
	return len(r.Blocked) > 0
}

func (r *ValidationResult) IsClean() bool {
	return len(r.Blocked) == 0 && len(r.Warnings) == 0
}

func CollectSystemSubnets(excludeIfaces ...string) (*SystemSubnets, error) {
	excludeSet := make(map[string]bool, len(excludeIfaces))
	for _, name := range excludeIfaces {
		if name != "" {
			excludeSet[name] = true
		}
	}

	sys := &SystemSubnets{}

	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, fmt.Errorf("list interfaces: %w", err)
	}
	for _, iface := range ifaces {
		if excludeSet[iface.Name] || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			_, ipNet, err := net.ParseCIDR(addr.String())
			if err != nil {
				continue
			}
			sys.Interfaces = append(sys.Interfaces, InterfaceSubnet{
				CIDR:      ipNet.String(),
				Interface: iface.Name,
			})
		}
	}

	conn, err := rtnetlink.Dial(nil)
	if err != nil {
		slog.Warn("rtnetlink dial failed, route validation skipped", "err", err)
		return sys, nil
	}
	defer func() { _ = conn.Close() }()

	routes, err := conn.Route.List()
	if err != nil {
		slog.Debug("route list failed", "err", err)
		return sys, nil
	}
	for _, rt := range routes {
		if rt.Attributes.Dst == nil {
			continue
		}
		if rt.Table != unix.RT_TABLE_MAIN && rt.Table != tailscaleRouteTable {
			continue
		}
		ifName := ifIndexToName(rt.Attributes.OutIface)
		if excludeSet[ifName] {
			continue
		}
		cidr := fmt.Sprintf("%s/%d", rt.Attributes.Dst, rt.DstLength)
		gateway := ""
		if rt.Attributes.Gateway != nil {
			gateway = rt.Attributes.Gateway.String()
		}
		rs := RouteSubnet{
			CIDR:      cidr,
			Interface: ifName,
			Gateway:   gateway,
			Protocol:  rtProtocolName(rt.Protocol),
		}
		if rt.Table == tailscaleRouteTable {
			sys.Table52Routes = append(sys.Table52Routes, rs)
		} else {
			sys.Routes = append(sys.Routes, rs)
		}
	}

	rules, err := conn.Rule.List()
	if err != nil {
		slog.Debug("ip rule list failed, PBR detection skipped", "err", err)
	} else {
		for _, rule := range rules {
			prio := rulePriority(rule)
			if prio < pbrPriorityMin || prio >= pbrPriorityMax {
				continue
			}
			if rule.Attributes == nil || rule.Attributes.FwMark == nil {
				continue
			}
			tbl := ruleTable(rule)
			if tbl == 0 {
				continue
			}
			sys.PBRRules = append(sys.PBRRules, PBRInfo{
				RulePriority: prio,
				FwMark:       *rule.Attributes.FwMark,
				FwMask:       derefUint32(rule.Attributes.FwMask),
				Table:        tbl,
			})
		}
	}

	return sys, nil
}

func ValidateAllowedIPs(cidrs []string, sys *SystemSubnets) *ValidationResult {
	result := &ValidationResult{}

	for _, cidr := range cidrs {
		_, candidateNet, err := net.ParseCIDR(cidr)
		if err != nil {
			continue
		}

		matched := false
		for _, ifSub := range sys.Interfaces {
			_, ifNet, err := net.ParseCIDR(ifSub.CIDR)
			if err != nil {
				continue
			}
			if subnetsOverlap(candidateNet, ifNet) {
				result.Blocked = append(result.Blocked, SubnetConflict{
					CIDR:          cidr,
					ConflictsWith: ifSub.CIDR,
					Interface:     ifSub.Interface,
					Severity:      "block",
					Message:       fmt.Sprintf("%s overlaps with %s (%s)", cidr, ifSub.CIDR, ifSub.Interface),
				})
				matched = true
				break
			}
		}
		if matched {
			continue
		}

		for _, rtSub := range sys.Table52Routes {
			_, rtNet, err := net.ParseCIDR(rtSub.CIDR)
			if err != nil {
				continue
			}
			if subnetsOverlap(candidateNet, rtNet) {
				via := rtSub.Interface
				if rtSub.Gateway != "" {
					via = rtSub.Gateway
				}
				result.Warnings = append(result.Warnings, SubnetConflict{
					CIDR:          cidr,
					ConflictsWith: fmt.Sprintf("%s (table 52 via %s)", rtSub.CIDR, via),
					Interface:     rtSub.Interface,
					Severity:      "warn",
					Message: fmt.Sprintf(
						"Tailscale route %s in table 52 (priority 5270) overrides S2S routes in main table (priority 32000). "+
							"Traffic will go through Tailscale instead of this tunnel.",
						rtSub.CIDR,
					),
				})
				matched = true
				break
			}
		}
		if matched {
			continue
		}

		if len(sys.PBRRules) > 0 {
			result.Warnings = append(result.Warnings, SubnetConflict{
				CIDR:          cidr,
				ConflictsWith: "Traffic Routes (PBR)",
				Severity:      "warn",
				Message: fmt.Sprintf(
					"Traffic Routes are configured on this device (ip rules at priority 32500+). "+
						"S2S route %s in main table (priority 32000) takes precedence over Traffic Routes (priority 32500+). "+
						"If any Traffic Route targets destinations overlapping with %s, it will stop working.",
					cidr, cidr,
				),
			})
			continue
		}

		for _, rtSub := range sys.Routes {
			_, rtNet, err := net.ParseCIDR(rtSub.CIDR)
			if err != nil {
				continue
			}
			if subnetsOverlap(candidateNet, rtNet) {
				via := rtSub.Interface
				if rtSub.Gateway != "" {
					via = rtSub.Gateway
				}
				result.Warnings = append(result.Warnings, SubnetConflict{
					CIDR:          cidr,
					ConflictsWith: fmt.Sprintf("%s (%s route via %s)", rtSub.CIDR, rtSub.Protocol, via),
					Interface:     rtSub.Interface,
					Severity:      "warn",
					Message:       fmt.Sprintf("Overlaps with existing route %s. Traffic may not be routed through tunnel.", rtSub.CIDR),
				})
				break
			}
		}
	}

	return result
}

func subnetsOverlap(a, b *net.IPNet) bool {
	return a.Contains(b.IP) || b.Contains(a.IP)
}

func ifIndexToName(idx uint32) string {
	if idx == 0 {
		return ""
	}
	iface, err := net.InterfaceByIndex(int(idx))
	if err != nil {
		return fmt.Sprintf("if%d", idx)
	}
	return iface.Name
}

func rtProtocolName(proto uint8) string {
	switch proto {
	case unix.RTPROT_KERNEL:
		return "kernel"
	case unix.RTPROT_BOOT:
		return "boot"
	case unix.RTPROT_STATIC:
		return "static"
	case unix.RTPROT_DHCP:
		return "dhcp"
	default:
		return fmt.Sprintf("proto-%d", proto)
	}
}

func rulePriority(r rtnetlink.RuleMessage) uint32 {
	if r.Attributes != nil && r.Attributes.Priority != nil {
		return *r.Attributes.Priority
	}
	return 0
}

func ruleTable(r rtnetlink.RuleMessage) uint32 {
	if r.Attributes != nil && r.Attributes.Table != nil {
		return *r.Attributes.Table
	}
	if r.Table != 0 {
		return uint32(r.Table)
	}
	return 0
}

func derefUint32(p *uint32) uint32 {
	if p != nil {
		return *p
	}
	return 0
}
