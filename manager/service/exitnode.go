package service

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"net/netip"
	"os/exec"
	"strconv"
	"strings"
	"sync"

	"unifi-tailscale/manager/domain"
)

const (
	exitRouteTable   = 53
	exitRuleBasePrio = 5280
	exitRuleMaxPrio  = 5299
	bypassMark       = 0x80000
	bypassMask       = 0xff0000
	maxExitClients   = 20
)

type ExitNodeManifest interface {
	GetExitNodePolicy() domain.ExitNodePolicy
	SetExitNodePolicy(p domain.ExitNodePolicy) error
}

type CmdRunner func(ctx context.Context, name string, args ...string) ([]byte, error)

type ExitNodeService struct {
	manifest ExitNodeManifest
	run      CmdRunner
	mu       sync.Mutex
}

func NewExitNodeService(manifest ExitNodeManifest, runner CmdRunner) *ExitNodeService {
	if runner == nil {
		runner = defaultCmdRunner
	}
	return &ExitNodeService{manifest: manifest, run: runner}
}

func defaultCmdRunner(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

type exitRule struct {
	Priority int
	Family   string // "-4" or "-6"
	Src      string // "" for catchall
}

func (s *ExitNodeService) Apply(ctx context.Context, policy domain.ExitNodePolicy) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.cleanupLocked(ctx); err != nil {
		slog.Warn("exit node cleanup before apply", "err", err)
	}

	switch policy.Mode {
	case domain.ExitNodeOff, "":
		return s.manifest.SetExitNodePolicy(domain.ExitNodePolicy{Mode: domain.ExitNodeOff})

	case domain.ExitNodeAll:
		for _, fam := range []string{"-4", "-6"} {
			if err := s.addRule(ctx, fam, "", exitRuleBasePrio); err != nil {
				return fmt.Errorf("add %s catchall exit rule: %w", fam, err)
			}
		}

	case domain.ExitNodeSelective:
		if len(policy.Clients) == 0 {
			return s.manifest.SetExitNodePolicy(policy)
		}
		prio := exitRuleBasePrio + 1
		for _, c := range policy.Clients {
			fam := familyForAddr(c.IP)
			if fam == "" {
				slog.Warn("skip invalid exit client address", "ip", c.IP)
				continue
			}
			if err := s.addRule(ctx, fam, c.IP, prio); err != nil {
				return fmt.Errorf("add exit rule for %s: %w", c.IP, err)
			}
			prio++
			if prio > exitRuleMaxPrio {
				slog.Warn("exit node client limit reached", "max", maxExitClients)
				break
			}
		}

	default:
		return validationError(fmt.Sprintf("unknown exit node mode: %s", policy.Mode))
	}

	return s.manifest.SetExitNodePolicy(policy)
}

func (s *ExitNodeService) Cleanup(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cleanupLocked(ctx)
}

func (s *ExitNodeService) cleanupLocked(ctx context.Context) error {
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

	desired := buildDesiredRules(policy)
	current, err := s.allCurrentRules(ctx)
	if err != nil {
		return fmt.Errorf("list current exit rules: %w", err)
	}

	if rulesMatch(current, desired) {
		return nil
	}

	slog.Info("exit node rules drifted, re-applying", "current", len(current), "desired", len(desired))
	s.mu.Unlock()
	err = s.Apply(ctx, policy)
	s.mu.Lock()
	return err
}

func (s *ExitNodeService) addRule(ctx context.Context, family, src string, prio int) error {
	args := []string{family, "rule", "add",
		"not", "fwmark", fmt.Sprintf("0x%x/0x%x", bypassMark, bypassMask),
	}
	if src != "" {
		args = append(args, "from", src)
	}
	args = append(args, "lookup", strconv.Itoa(exitRouteTable), "prio", strconv.Itoa(prio))

	out, err := s.run(ctx, "ip", args...)
	if err != nil {
		return fmt.Errorf("ip %s: %w (%s)", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
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
	lookupStr := fmt.Sprintf("lookup %d", exitRouteTable)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.Contains(line, lookupStr) {
			continue
		}
		prio, ok := parseRulePriority(line)
		if !ok || prio < exitRuleBasePrio || prio > exitRuleMaxPrio {
			continue
		}
		src := parseRuleFrom(line)
		rules = append(rules, exitRule{Priority: prio, Family: family, Src: src})
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

func buildDesiredRules(policy domain.ExitNodePolicy) []exitRule {
	switch policy.Mode {
	case domain.ExitNodeAll:
		return []exitRule{
			{Priority: exitRuleBasePrio, Family: "-4"},
			{Priority: exitRuleBasePrio, Family: "-6"},
		}
	case domain.ExitNodeSelective:
		var rules []exitRule
		prio := exitRuleBasePrio + 1
		for _, c := range policy.Clients {
			fam := familyForAddr(c.IP)
			if fam == "" {
				continue
			}
			rules = append(rules, exitRule{Priority: prio, Family: fam, Src: c.IP})
			prio++
			if prio > exitRuleMaxPrio {
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
	return fmt.Sprintf("%d/%s/%s", r.Priority, r.Family, r.Src)
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
