package main

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

type SystemSubnets struct {
	Interfaces []InterfaceSubnet
	Routes     []RouteSubnet
}

type SubnetConflict struct {
	CIDR          string `json:"cidr"`
	ConflictsWith string `json:"conflictsWith"`
	Interface     string `json:"interface,omitempty"`
	Severity      string `json:"severity"`
	Message       string `json:"message"`
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
		if rt.Table != unix.RT_TABLE_MAIN {
			continue
		}
		if rt.Attributes.Dst == nil {
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
		sys.Routes = append(sys.Routes, RouteSubnet{
			CIDR:      cidr,
			Interface: ifName,
			Gateway:   gateway,
			Protocol:  rtProtocolName(rt.Protocol),
		})
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

		blocked := false
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
				blocked = true
				break
			}
		}
		if blocked {
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
