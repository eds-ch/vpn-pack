package service

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jsimonetti/rtnetlink"

	"unifi-tailscale/manager/config"
	"unifi-tailscale/manager/domain"
)

const (
	routingHealthTTL     = 60 * time.Second
	tailscalePriorityMin = 5210
	tailscalePriorityMax = 5310
)

type RoutingHealthChecker struct {
	readRPFilter  func(iface string) (int, error)
	listFwRules   func() ([]PBRInfo, error)
	checkIP6Chain func(chain string) bool
	ifaceExists   func(name string) bool

	mu       sync.Mutex
	cached   *domain.RoutingHealth
	cachedAt time.Time
	hasCache bool
}

func NewRoutingHealthChecker() *RoutingHealthChecker {
	return &RoutingHealthChecker{
		readRPFilter:  defaultReadRPFilter,
		listFwRules:   defaultListFwRules,
		checkIP6Chain: defaultCheckIP6Chain,
		ifaceExists:   defaultIfaceExists,
	}
}

func (c *RoutingHealthChecker) Check() *domain.RoutingHealth {
	if c == nil {
		return nil
	}
	if !c.ifaceExists(config.TailscaleInterface) {
		return nil
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.hasCache && time.Since(c.cachedAt) < routingHealthTTL {
		return c.cached
	}

	var warnings []domain.RoutingWarning

	if w := c.checkRPFilter(); w != nil {
		slog.Warn("routing health: "+w.Message, "check", w.Check, "value", w.Value)
		warnings = append(warnings, *w)
	}
	if w := c.checkBypassMarkConflict(); w != nil {
		slog.Warn("routing health: "+w.Message, "check", w.Check, "value", w.Value)
		warnings = append(warnings, *w)
	}
	if w := c.checkIPv6TsForward(); w != nil {
		slog.Warn("routing health: "+w.Message, "check", w.Check)
		warnings = append(warnings, *w)
	}

	var result *domain.RoutingHealth
	if len(warnings) > 0 {
		result = &domain.RoutingHealth{Warnings: warnings}
	}
	c.cached = result
	c.cachedAt = time.Now()
	c.hasCache = true
	return result
}

func (c *RoutingHealthChecker) checkRPFilter() *domain.RoutingWarning {
	val, err := c.readRPFilter(config.TailscaleInterface)
	if err != nil {
		return nil
	}
	if val == 1 {
		return &domain.RoutingWarning{
			Check:    "rp_filter",
			Severity: "warning",
			Message: fmt.Sprintf(
				"rp_filter=1 (strict) on %s: CGNAT return traffic (100.64.x.x) may be silently dropped by kernel reverse path filtering. Expected rp_filter=2 (loose).",
				config.TailscaleInterface,
			),
			Value: "1",
		}
	}
	return nil
}

func (c *RoutingHealthChecker) checkBypassMarkConflict() *domain.RoutingWarning {
	rules, err := c.listFwRules()
	if err != nil {
		return nil
	}
	for _, r := range rules {
		if r.RulePriority >= tailscalePriorityMin && r.RulePriority <= tailscalePriorityMax {
			continue
		}
		if r.FwMark != 0 && (r.FwMark&bypassMask) == bypassMark {
			return &domain.RoutingWarning{
				Check:    "bypass_mark",
				Severity: "critical",
				Message: fmt.Sprintf(
					"PBR rule at priority %d uses fwmark 0x%x which collides with Tailscale BypassMark 0x%x/0x%x. "+
						"Traffic with this mark will bypass Tailscale routing table 52.",
					r.RulePriority, r.FwMark, bypassMark, bypassMask,
				),
				Value: fmt.Sprintf("0x%x", r.FwMark),
			}
		}
	}
	return nil
}

func (c *RoutingHealthChecker) checkIPv6TsForward() *domain.RoutingWarning {
	if !c.checkIP6Chain("ts-forward") {
		return &domain.RoutingWarning{
			Check:    "ipv6_ts_forward",
			Severity: "warning",
			Message:  "ip6tables ts-forward chain not found. IPv6 traffic through tailscale0 may not be forwarded correctly.",
		}
	}
	return nil
}

// --- default implementations ---

func defaultReadRPFilter(iface string) (int, error) {
	path := fmt.Sprintf("/proc/sys/net/ipv4/conf/%s/rp_filter", iface)
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(string(data)))
}

func defaultListFwRules() ([]PBRInfo, error) {
	conn, err := rtnetlink.Dial(nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = conn.Close() }()

	rules, err := conn.Rule.List()
	if err != nil {
		return nil, err
	}

	var result []PBRInfo
	for _, rule := range rules {
		if rule.Attributes == nil || rule.Attributes.FwMark == nil {
			continue
		}
		prio := rulePriority(rule)
		result = append(result, PBRInfo{
			RulePriority: prio,
			FwMark:       *rule.Attributes.FwMark,
			FwMask:       derefUint32(rule.Attributes.FwMask),
			Table:        ruleTable(rule),
		})
	}
	return result, nil
}

func defaultCheckIP6Chain(chain string) bool {
	return exec.Command("ip6tables", "-w", "2", "-L", chain, "-n").Run() == nil
}

func defaultIfaceExists(name string) bool {
	_, err := os.Stat("/sys/class/net/" + name)
	return err == nil
}
