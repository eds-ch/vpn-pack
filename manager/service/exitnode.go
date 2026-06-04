package service

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"unifi-tailscale/manager/domain"
	"unifi-tailscale/manager/ops"
)

const (
	ExitRouteTable   = 53
	ExitRuleBasePrio = 5280
	ExitRuleMaxPrio  = 5300
	ExitMasqComment  = "vpn-pack-exit-masq"

	bypassMark     = 0x80000
	bypassMask     = 0xff0000
	maxExitClients = 20
	tsInterface    = "tailscale0"
	tsCGNATv4      = "100.64.0.0/10"
	tsCGNATv6      = "fd7a:115c:a1e0::/48"
)

type ExitNodeManifest interface {
	GetExitNodePolicy() domain.ExitNodePolicy
	SetExitNodePolicy(p domain.ExitNodePolicy) error
}

type CmdRunner func(ctx context.Context, name string, args ...string) ([]byte, error)

type ExitNodeService struct {
	manifest        ExitNodeManifest
	run             CmdRunner
	discoverBridges func() ([]string, error)
	mu              sync.Mutex
}

func NewExitNodeService(manifest ExitNodeManifest, runner CmdRunner) *ExitNodeService {
	if runner == nil {
		runner = defaultCmdRunner
	}
	return &ExitNodeService{manifest: manifest, run: runner, discoverBridges: defaultDiscoverBridges}
}

func defaultCmdRunner(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

type exitRule struct {
	Priority int
	Family   string // "-4" or "-6"
	Src      string // client IP/CIDR for selective mode
	Iif      string // input interface for mode=all (e.g. "br0")
}

func (s *ExitNodeService) Apply(ctx context.Context, policy domain.ExitNodePolicy) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.applyLocked(ctx, policy)
}

func (s *ExitNodeService) applyLocked(ctx context.Context, policy domain.ExitNodePolicy) error {
	if err := s.cleanupLocked(ctx); err != nil {
		slog.Warn("exit node cleanup before apply", "err", err)
	}
	defer s.flushConntrack(ctx)

	if policy.Mode == domain.ExitNodeOff || policy.Mode == "" {
		return s.manifest.SetExitNodePolicy(domain.ExitNodePolicy{Mode: domain.ExitNodeOff})
	}

	steps, err := s.buildApplyOps(policy)
	if err != nil {
		return err
	}
	return ops.Run(ctx, steps)
}

// isCatchAllPrefix returns true for 0.0.0.0/0, ::/0, or any prefix with
// Bits()==0. Used to enforce SEC-C19: selective mode must never accept a
// catch-all client (that bypasses the explicit "all clients" confirmation).
func isCatchAllPrefix(s string) bool {
	if p, err := netip.ParsePrefix(s); err == nil {
		return p.Bits() == 0
	}
	return false
}

func (s *ExitNodeService) buildApplyOps(policy domain.ExitNodePolicy) ([]ops.Op, error) {
	var out []ops.Op
	switch policy.Mode {
	case domain.ExitNodeAll:
		bridges, err := s.discoverBridges()
		if err != nil {
			return nil, fmt.Errorf("discover LAN bridges: %w", err)
		}
		if len(bridges) == 0 {
			return nil, fmt.Errorf("no LAN bridge interfaces found")
		}
		prio := ExitRuleBasePrio
		for _, br := range bridges {
			for _, fam := range []string{"-4", "-6"} {
				fam, br, prio := fam, br, prio
				out = append(out, ops.Op{
					Name: fmt.Sprintf("add rule %s iif %s prio %d", fam, br, prio),
					Do:   func(ctx context.Context) error { return s.addRule(ctx, fam, "", prio, br) },
					Undo: func(ctx context.Context) error { return s.delRule(ctx, fam, prio) },
				})
			}
			prio++
			if prio > ExitRuleMaxPrio {
				slog.Warn("exit node bridge limit reached", "max", ExitRuleMaxPrio-ExitRuleBasePrio)
				break
			}
		}

	case domain.ExitNodeSelective:
		if len(policy.Clients) == 0 {
			return []ops.Op{ops.Noop("persist empty selective", func(_ context.Context) error {
				return s.manifest.SetExitNodePolicy(policy)
			})}, nil
		}
		prio := ExitRuleBasePrio + 1
		for _, c := range policy.Clients {
			if isCatchAllPrefix(c.IP) {
				return nil, validationError(fmt.Sprintf(
					"selective client %q is a catch-all prefix; use mode=all explicitly", c.IP))
			}
			fam := familyForAddr(c.IP)
			if fam == "" {
				slog.Warn("skip invalid exit client address", "ip", c.IP)
				continue
			}
			famCap, srcCap, prioCap := fam, c.IP, prio
			out = append(out, ops.Op{
				Name: fmt.Sprintf("add rule %s from %s prio %d", famCap, srcCap, prioCap),
				Do:   func(ctx context.Context) error { return s.addRule(ctx, famCap, srcCap, prioCap, "") },
				Undo: func(ctx context.Context) error { return s.delRule(ctx, famCap, prioCap) },
			})
			prio++
			if prio > ExitRuleMaxPrio {
				slog.Warn("exit node client limit reached", "max", maxExitClients)
				break
			}
		}

	default:
		return nil, validationError(fmt.Sprintf("unknown exit node mode: %s", policy.Mode))
	}

	// MASQUERADE is split into per-family ops so a non-tolerated IPv6 failure
	// triggers proper rollback of the already-installed IPv4 rule. The v6 op
	// internally tolerates "ip6tables unavailable" — those install zero rules
	// so the Undo is a no-op.
	out = append(out, ops.Op{
		Name: "add v4 masquerade",
		Do:   func(ctx context.Context) error { return s.addV4Masquerade(ctx) },
		Undo: func(ctx context.Context) error { s.delV4Masquerade(ctx); return nil },
	})
	v6Installed := false
	out = append(out, ops.Op{
		Name: "add v6 masquerade (best-effort)",
		Do: func(ctx context.Context) error {
			installed, err := s.addV6Masquerade(ctx)
			v6Installed = installed
			return err
		},
		Undo: func(ctx context.Context) error {
			if v6Installed {
				s.delV6Masquerade(ctx)
			}
			return nil
		},
	})
	out = append(out, ops.Noop("persist manifest", func(_ context.Context) error {
		return s.manifest.SetExitNodePolicy(policy)
	}))
	return out, nil
}

func (s *ExitNodeService) Cleanup(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	err := s.cleanupLocked(ctx)
	s.flushConntrack(ctx)
	return err
}

func (s *ExitNodeService) cleanupLocked(ctx context.Context) error {
	s.delMasquerade(ctx)
	var errs []string
	for _, fam := range []string{"-4", "-6"} {
		rules, err := s.listRules(ctx, fam)
		if err != nil {
			errs = append(errs, err.Error())
			continue
		}
		for _, r := range rules {
			if err := s.delRule(ctx, fam, r.Priority); err != nil {
				errs = append(errs, err.Error())
			}
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("cleanup errors: %s", strings.Join(errs, "; "))
	}
	return nil
}

func (s *ExitNodeService) Reconcile(ctx context.Context, policy domain.ExitNodePolicy) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var bridges []string
	if policy.Mode == domain.ExitNodeAll {
		var bErr error
		bridges, bErr = s.discoverBridges()
		if bErr != nil {
			return fmt.Errorf("discover bridges for reconcile: %w", bErr)
		}
	}

	desired := buildDesiredRules(policy, bridges)
	current, err := s.allCurrentRules(ctx)
	if err != nil {
		return fmt.Errorf("list current exit rules: %w", err)
	}

	needsMasq := len(desired) > 0
	if rulesMatch(current, desired) && needsMasq == s.hasMasquerade(ctx) {
		return nil
	}

	slog.Info("exit node rules drifted, re-applying", "current", len(current), "desired", len(desired))
	return s.applyLocked(ctx, policy)
}

func (s *ExitNodeService) addRule(ctx context.Context, family, src string, prio int, iif string) error {
	args := []string{family, "rule", "add"}
	if iif != "" {
		args = append(args, "iif", iif)
	} else if src != "" {
		args = append(args, "from", src)
	}
	args = append(args, "lookup", strconv.Itoa(ExitRouteTable), "prio", strconv.Itoa(prio))

	out, err := s.run(ctx, "ip", args...)
	if err != nil {
		return fmt.Errorf("ip %s: %w (%s)", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (s *ExitNodeService) flushConntrack(ctx context.Context) {
	out, err := s.run(ctx, "conntrack", "-F")
	if err != nil {
		slog.Warn("conntrack flush", "err", err, "out", strings.TrimSpace(string(out)))
		return
	}
	slog.Info("conntrack flushed after exit node routing change")
}

func (s *ExitNodeService) masqArgs(action string) [][]string {
	return [][]string{
		{"-t", "nat", action, "POSTROUTING",
			"-o", tsInterface, "!", "-s", tsCGNATv4,
			"-j", "MASQUERADE", "-m", "comment", "--comment", ExitMasqComment},
		{"-t", "nat", action, "POSTROUTING",
			"-o", tsInterface, "!", "-s", tsCGNATv6,
			"-j", "MASQUERADE", "-m", "comment", "--comment", ExitMasqComment},
	}
}

// addV4Masquerade installs the IPv4 MASQUERADE rule. Failure is always fatal.
func (s *ExitNodeService) addV4Masquerade(ctx context.Context) error {
	args := s.masqArgs("-A")[0]
	if out, err := s.run(ctx, "iptables", args...); err != nil {
		return fmt.Errorf("iptables masquerade add: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// delV4Masquerade removes the IPv4 MASQUERADE rule, ignoring "rule absent" errors.
func (s *ExitNodeService) delV4Masquerade(ctx context.Context) {
	args := s.masqArgs("-D")[0]
	_, _ = s.run(ctx, "iptables", args...)
}

// addV6Masquerade attempts the IPv6 MASQUERADE install. Returns (installed,
// err): installed=true means a rule actually landed and the caller must
// delete it on rollback; installed=false means the system reported IPv6 as
// unavailable (tolerated — no rule landed, nothing to roll back).
// A non-tolerated error returns (false, err) so the saga can abort.
func (s *ExitNodeService) addV6Masquerade(ctx context.Context) (bool, error) {
	args := s.masqArgs("-A")[1]
	out, err := s.run(ctx, "ip6tables", args...)
	if err == nil {
		return true, nil
	}
	if isIP6Unavailable(err, out) {
		slog.Warn("ip6tables masquerade add (IPv6 unavailable, tolerated)",
			"err", err, "out", strings.TrimSpace(string(out)))
		return false, nil
	}
	return false, fmt.Errorf("ip6tables masquerade add: %w (%s)", err, strings.TrimSpace(string(out)))
}

// delV6Masquerade removes the IPv6 MASQUERADE rule, ignoring errors.
func (s *ExitNodeService) delV6Masquerade(ctx context.Context) {
	args := s.masqArgs("-D")[1]
	_, _ = s.run(ctx, "ip6tables", args...)
}

// isIP6Unavailable returns true when the ip6tables error indicates the
// binary is missing or the IPv6 stack is disabled — situations the plan
// requires us to tolerate. All other errors (including "rule not present",
// which ip6tables reports as "No chain/target/match by that name.")
// propagate so a real IPv6 firewall failure is not silently swallowed.
//
// Markers are narrow on purpose: the bare token "not found" would also
// match exec.LookPath failures, which is desirable, but must not bleed
// into rule-presence checks. The full Go exec error contains
// "executable file not found in $PATH" which both markers below cover.
func isIP6Unavailable(err error, out []byte) bool {
	msg := strings.ToLower(err.Error() + " " + string(out))
	for _, marker := range []string{
		"executable file not found",
		"no such file or directory",
		"address family not supported",
		"protocol not supported",
		"module is not loaded",
	} {
		if strings.Contains(msg, marker) {
			return true
		}
	}
	return false
}

func (s *ExitNodeService) delMasquerade(ctx context.Context) {
	cmds := []string{"iptables", "ip6tables"}
	for i, args := range s.masqArgs("-D") {
		_, _ = s.run(ctx, cmds[i], args...)
	}
}

// hasMasquerade reports whether the masquerade install we expect is
// currently present. v4 is mandatory; a v6 check failure caused by the
// IPv6 stack being absent / disabled is tolerated and treated as "no
// drift" — otherwise Reconcile would churn forever on IPv6-disabled
// systems. A v6 failure on a healthy stack (e.g. "no chain by that
// name") is real drift and must trigger reapply.
func (s *ExitNodeService) hasMasquerade(ctx context.Context) bool {
	all := s.masqArgs("-C")
	if _, err := s.run(ctx, "iptables", all[0]...); err != nil {
		return false
	}
	out, err := s.run(ctx, "ip6tables", all[1]...)
	if err != nil {
		return isIP6Unavailable(err, out)
	}
	return true
}

func (s *ExitNodeService) delRule(ctx context.Context, family string, prio int) error {
	args := []string{family, "rule", "del", "prio", strconv.Itoa(prio)}
	out, err := s.run(ctx, "ip", args...)
	if err != nil {
		return fmt.Errorf("ip %s: %w (%s)", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (s *ExitNodeService) listRules(ctx context.Context, family string) ([]exitRule, error) {
	out, err := s.run(ctx, "ip", family, "rule", "show")
	if err != nil {
		return nil, fmt.Errorf("ip %s rule show: %w", family, err)
	}
	return parseRules(string(out), family), nil
}

func (s *ExitNodeService) allCurrentRules(ctx context.Context) ([]exitRule, error) {
	var all []exitRule
	for _, fam := range []string{"-4", "-6"} {
		rules, err := s.listRules(ctx, fam)
		if err != nil {
			return nil, err
		}
		all = append(all, rules...)
	}
	return all, nil
}

func parseRules(output, family string) []exitRule {
	var rules []exitRule
	scanner := bufio.NewScanner(strings.NewReader(output))
	lookupStr := fmt.Sprintf("lookup %d", ExitRouteTable)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.Contains(line, lookupStr) {
			continue
		}
		prio, ok := parseRulePriority(line)
		if !ok || prio < ExitRuleBasePrio || prio > ExitRuleMaxPrio {
			continue
		}
		src := parseRuleFrom(line)
		iif := parseRuleIif(line)
		rules = append(rules, exitRule{Priority: prio, Family: family, Src: src, Iif: iif})
	}
	return rules
}

func parseRulePriority(line string) (int, bool) {
	idx := strings.IndexByte(line, ':')
	if idx <= 0 {
		return 0, false
	}
	n, err := strconv.Atoi(strings.TrimSpace(line[:idx]))
	if err != nil {
		return 0, false
	}
	return n, true
}

func parseRuleFrom(line string) string {
	const fromPrefix = "from "
	idx := strings.Index(line, fromPrefix)
	if idx < 0 {
		return ""
	}
	rest := line[idx+len(fromPrefix):]
	end := strings.IndexByte(rest, ' ')
	if end < 0 {
		return rest
	}
	src := rest[:end]
	if src == "all" {
		return ""
	}
	return src
}

func parseRuleIif(line string) string {
	const prefix = "iif "
	idx := strings.Index(line, prefix)
	if idx < 0 {
		return ""
	}
	rest := line[idx+len(prefix):]
	end := strings.IndexByte(rest, ' ')
	if end < 0 {
		return rest
	}
	return rest[:end]
}

func buildDesiredRules(policy domain.ExitNodePolicy, bridges []string) []exitRule {
	switch policy.Mode {
	case domain.ExitNodeAll:
		var rules []exitRule
		prio := ExitRuleBasePrio
		for _, br := range bridges {
			for _, fam := range []string{"-4", "-6"} {
				rules = append(rules, exitRule{Priority: prio, Family: fam, Iif: br})
			}
			prio++
			if prio > ExitRuleMaxPrio {
				break
			}
		}
		return rules
	case domain.ExitNodeSelective:
		var rules []exitRule
		prio := ExitRuleBasePrio + 1
		for _, c := range policy.Clients {
			fam := familyForAddr(c.IP)
			if fam == "" {
				continue
			}
			rules = append(rules, exitRule{Priority: prio, Family: fam, Src: normalizeRuleSrc(c.IP)})
			prio++
			if prio > ExitRuleMaxPrio {
				break
			}
		}
		return rules
	default:
		return nil
	}
}

func rulesMatch(current, desired []exitRule) bool {
	if len(current) != len(desired) {
		return false
	}
	cm := make(map[string]bool, len(current))
	for _, r := range current {
		cm[ruleKey(r)] = true
	}
	for _, r := range desired {
		if !cm[ruleKey(r)] {
			return false
		}
	}
	return true
}

func ruleKey(r exitRule) string {
	return fmt.Sprintf("%d/%s/%s/%s", r.Priority, r.Family, r.Src, r.Iif)
}

func normalizeRuleSrc(s string) string {
	if p, err := netip.ParsePrefix(s); err == nil {
		if (p.Addr().Is4() && p.Bits() == 32) || (p.Addr().Is6() && p.Bits() == 128) {
			return p.Addr().String()
		}
	}
	return s
}

func familyForAddr(s string) string {
	if p, err := netip.ParsePrefix(s); err == nil {
		if p.Addr().Is4() {
			return "-4"
		}
		return "-6"
	}
	if a, err := netip.ParseAddr(s); err == nil {
		if a.Is4() {
			return "-4"
		}
		return "-6"
	}
	return ""
}

func defaultDiscoverBridges() ([]string, error) {
	entries, err := os.ReadDir("/sys/class/net")
	if err != nil {
		return nil, fmt.Errorf("read /sys/class/net: %w", err)
	}
	var bridges []string
	for _, e := range entries {
		bridgeDir := filepath.Join("/sys/class/net", e.Name(), "bridge")
		if fi, err := os.Stat(bridgeDir); err == nil && fi.IsDir() {
			bridges = append(bridges, e.Name())
		}
	}
	sort.Strings(bridges)
	return bridges, nil
}

func ValidateExitNodePolicy(policy domain.ExitNodePolicy) error {
	switch policy.Mode {
	case domain.ExitNodeOff, domain.ExitNodeAll:
		return nil
	case domain.ExitNodeSelective:
		if len(policy.Clients) > maxExitClients {
			return validationError(fmt.Sprintf("too many exit clients: %d (max %d)", len(policy.Clients), maxExitClients))
		}
		for _, c := range policy.Clients {
			if familyForAddr(c.IP) == "" {
				return validationError(fmt.Sprintf("invalid client IP/CIDR: %s", c.IP))
			}
		}
		return nil
	default:
		return validationError(fmt.Sprintf("unknown exit node mode: %s", policy.Mode))
	}
}
