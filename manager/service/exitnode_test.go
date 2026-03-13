package service

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"unifi-tailscale/manager/domain"
)

type mockExitManifest struct {
	mu     sync.Mutex
	policy domain.ExitNodePolicy
}

func (m *mockExitManifest) GetExitNodePolicy() domain.ExitNodePolicy {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.policy
}

func (m *mockExitManifest) SetExitNodePolicy(p domain.ExitNodePolicy) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.policy = p
	return nil
}

type fakeIPRuleState struct {
	mu        sync.Mutex
	rules     map[string][]string // family -> list of rule lines
	cmds      []string
	masqRules map[string]bool // "iptables" / "ip6tables" -> exists
}

func newFakeIPRuleState() *fakeIPRuleState {
	return &fakeIPRuleState{
		rules:     map[string][]string{"-4": {}, "-6": {}},
		masqRules: make(map[string]bool),
	}
}

func (f *fakeIPRuleState) runner() CmdRunner {
	return func(ctx context.Context, name string, args ...string) ([]byte, error) {
		f.mu.Lock()
		defer f.mu.Unlock()
		full := strings.Join(append([]string{name}, args...), " ")
		f.cmds = append(f.cmds, full)

		if name == "conntrack" {
			return nil, nil
		}

		if name == "iptables" || name == "ip6tables" {
			return f.handleIptablesLocked(name, args)
		}

		if len(args) < 3 {
			return nil, fmt.Errorf("too few args")
		}
		family := args[0]
		action := args[2] // "add", "del", "show"

		switch action {
		case "show":
			lines := f.rules[family]
			return []byte(strings.Join(lines, "\n") + "\n"), nil

		case "add":
			line := buildFakeRuleLine(args[3:])
			f.rules[family] = append(f.rules[family], line)
			return nil, nil

		case "del":
			prio := extractPrio(args[3:])
			var kept []string
			for _, l := range f.rules[family] {
				if !strings.HasPrefix(l, prio+":") {
					kept = append(kept, l)
				}
			}
			f.rules[family] = kept
			return nil, nil

		default:
			return nil, fmt.Errorf("unknown action: %s", action)
		}
	}
}

func buildFakeRuleLine(args []string) string {
	prio := ""
	src := "all"
	lookup := ""
	iif := ""
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "prio":
			if i+1 < len(args) {
				prio = args[i+1]
				i++
			}
		case "from":
			if i+1 < len(args) {
				src = args[i+1]
				i++
			}
		case "lookup":
			if i+1 < len(args) {
				lookup = args[i+1]
				i++
			}
		case "iif":
			if i+1 < len(args) {
				iif = args[i+1]
				i++
			}
		}
	}
	line := fmt.Sprintf("%s:\tfrom %s", prio, src)
	if iif != "" {
		line += fmt.Sprintf(" iif %s", iif)
	}
	line += fmt.Sprintf(" lookup %s", lookup)
	return line
}

func extractPrio(args []string) string {
	for i, a := range args {
		if a == "prio" && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

func (f *fakeIPRuleState) handleIptablesLocked(cmd string, args []string) ([]byte, error) {
	action := ""
	for _, a := range args {
		switch a {
		case "-A", "-D", "-C":
			action = a
		}
	}
	switch action {
	case "-A":
		f.masqRules[cmd] = true
		return nil, nil
	case "-D":
		if !f.masqRules[cmd] {
			return nil, fmt.Errorf("rule not found")
		}
		delete(f.masqRules, cmd)
		return nil, nil
	case "-C":
		if f.masqRules[cmd] {
			return nil, nil
		}
		return nil, fmt.Errorf("rule not found")
	default:
		return nil, fmt.Errorf("unknown iptables action: %s", action)
	}
}

func (f *fakeIPRuleState) masqCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.masqRules)
}

func (f *fakeIPRuleState) ruleCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, rules := range f.rules {
		n += len(rules)
	}
	return n
}

func (f *fakeIPRuleState) commandCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.cmds)
}

func (f *fakeIPRuleState) conntrackFlushCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, cmd := range f.cmds {
		if cmd == "conntrack -F" {
			n++
		}
	}
	return n
}

func stubBridges(svc *ExitNodeService, bridges ...string) {
	svc.discoverBridges = func() ([]string, error) { return bridges, nil }
}

func TestApplyOff(t *testing.T) {
	state := newFakeIPRuleState()
	manifest := &mockExitManifest{}
	svc := NewExitNodeService(manifest, state.runner())

	err := svc.Apply(context.Background(), domain.ExitNodePolicy{Mode: domain.ExitNodeOff})
	require.NoError(t, err)
	assert.Equal(t, 0, state.ruleCount())
	assert.Equal(t, domain.ExitNodeOff, manifest.policy.Mode)
}

func TestApplyAll(t *testing.T) {
	state := newFakeIPRuleState()
	manifest := &mockExitManifest{}
	svc := NewExitNodeService(manifest, state.runner())
	stubBridges(svc, "br0")

	err := svc.Apply(context.Background(), domain.ExitNodePolicy{Mode: domain.ExitNodeAll})
	require.NoError(t, err)
	assert.Equal(t, 2, state.ruleCount()) // IPv4 + IPv6
	assert.Equal(t, domain.ExitNodeAll, manifest.policy.Mode)

	state.mu.Lock()
	for _, fam := range []string{"-4", "-6"} {
		require.Len(t, state.rules[fam], 1, "expected 1 rule for %s", fam)
		assert.Contains(t, state.rules[fam][0], "lookup 53")
		assert.Contains(t, state.rules[fam][0], "5280:")
		assert.Contains(t, state.rules[fam][0], "iif br0")
	}
	state.mu.Unlock()
}

func TestApplySelective(t *testing.T) {
	state := newFakeIPRuleState()
	manifest := &mockExitManifest{}
	svc := NewExitNodeService(manifest, state.runner())

	policy := domain.ExitNodePolicy{
		Mode: domain.ExitNodeSelective,
		Clients: []domain.ExitNodeClient{
			{IP: "192.168.1.100", Label: "Office PC"},
			{IP: "192.168.2.0/24", Label: "Guest VLAN"},
			{IP: "fd00::1", Label: "IPv6 host"},
		},
	}

	err := svc.Apply(context.Background(), policy)
	require.NoError(t, err)
	assert.Equal(t, 3, state.ruleCount()) // 2 IPv4 + 1 IPv6

	state.mu.Lock()
	assert.Len(t, state.rules["-4"], 2)
	assert.Contains(t, state.rules["-4"][0], "from 192.168.1.100")
	assert.Contains(t, state.rules["-4"][0], "5281:")
	assert.Contains(t, state.rules["-4"][1], "from 192.168.2.0/24")
	assert.Contains(t, state.rules["-4"][1], "5282:")
	assert.Len(t, state.rules["-6"], 1)
	assert.Contains(t, state.rules["-6"][0], "from fd00::1")
	assert.Contains(t, state.rules["-6"][0], "5283:")
	state.mu.Unlock()

	assert.Equal(t, domain.ExitNodeSelective, manifest.policy.Mode)
}

func TestApplySelectiveEmptyClients(t *testing.T) {
	state := newFakeIPRuleState()
	manifest := &mockExitManifest{}
	svc := NewExitNodeService(manifest, state.runner())

	policy := domain.ExitNodePolicy{
		Mode:    domain.ExitNodeSelective,
		Clients: nil,
	}
	err := svc.Apply(context.Background(), policy)
	require.NoError(t, err)
	assert.Equal(t, 0, state.ruleCount())
}

func TestApplyReplacesExisting(t *testing.T) {
	state := newFakeIPRuleState()
	manifest := &mockExitManifest{}
	svc := NewExitNodeService(manifest, state.runner())
	stubBridges(svc, "br0")

	// Apply "all" first
	err := svc.Apply(context.Background(), domain.ExitNodePolicy{Mode: domain.ExitNodeAll})
	require.NoError(t, err)
	assert.Equal(t, 2, state.ruleCount())

	// Switch to selective — old rules should be cleaned first
	policy := domain.ExitNodePolicy{
		Mode:    domain.ExitNodeSelective,
		Clients: []domain.ExitNodeClient{{IP: "10.0.0.1"}},
	}
	err = svc.Apply(context.Background(), policy)
	require.NoError(t, err)
	assert.Equal(t, 1, state.ruleCount()) // only the selective rule
}

func TestCleanup(t *testing.T) {
	state := newFakeIPRuleState()
	manifest := &mockExitManifest{}
	svc := NewExitNodeService(manifest, state.runner())
	stubBridges(svc, "br0")

	_ = svc.Apply(context.Background(), domain.ExitNodePolicy{Mode: domain.ExitNodeAll})
	assert.Equal(t, 2, state.ruleCount())

	err := svc.Cleanup(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, state.ruleCount())
}

func TestReconcileNoDrift(t *testing.T) {
	state := newFakeIPRuleState()
	manifest := &mockExitManifest{}
	svc := NewExitNodeService(manifest, state.runner())
	stubBridges(svc, "br0")

	policy := domain.ExitNodePolicy{Mode: domain.ExitNodeAll}
	_ = svc.Apply(context.Background(), policy)
	cmdsBefore := state.commandCount()

	err := svc.Reconcile(context.Background(), policy)
	require.NoError(t, err)
	// Reconcile should only have done "show" + "check" commands (no adds/deletes)
	cmdsAfter := state.commandCount()
	// 2 show (ip rule) + 2 check (iptables -C)
	assert.Equal(t, cmdsBefore+4, cmdsAfter)
}

func TestReconcileDrift(t *testing.T) {
	state := newFakeIPRuleState()
	manifest := &mockExitManifest{}
	svc := NewExitNodeService(manifest, state.runner())
	stubBridges(svc, "br0")

	policy := domain.ExitNodePolicy{Mode: domain.ExitNodeAll}
	_ = svc.Apply(context.Background(), policy)

	// Simulate drift: remove IPv4 rule
	state.mu.Lock()
	state.rules["-4"] = nil
	state.mu.Unlock()

	err := svc.Reconcile(context.Background(), policy)
	require.NoError(t, err)
	assert.Equal(t, 2, state.ruleCount()) // restored
}

func TestValidateExitNodePolicy(t *testing.T) {
	tests := []struct {
		name    string
		policy  domain.ExitNodePolicy
		wantErr bool
	}{
		{"off", domain.ExitNodePolicy{Mode: domain.ExitNodeOff}, false},
		{"all", domain.ExitNodePolicy{Mode: domain.ExitNodeAll}, false},
		{"selective valid", domain.ExitNodePolicy{
			Mode:    domain.ExitNodeSelective,
			Clients: []domain.ExitNodeClient{{IP: "192.168.1.0/24"}},
		}, false},
		{"selective invalid IP", domain.ExitNodePolicy{
			Mode:    domain.ExitNodeSelective,
			Clients: []domain.ExitNodeClient{{IP: "not-an-ip"}},
		}, true},
		{"unknown mode", domain.ExitNodePolicy{Mode: "unknown"}, true},
		{"too many clients", domain.ExitNodePolicy{
			Mode:    domain.ExitNodeSelective,
			Clients: make([]domain.ExitNodeClient, maxExitClients+1),
		}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Fill IPs for too-many-clients test
			for i := range tt.policy.Clients {
				if tt.policy.Clients[i].IP == "" {
					tt.policy.Clients[i].IP = fmt.Sprintf("10.0.0.%d", i+1)
				}
			}
			err := ValidateExitNodePolicy(tt.policy)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestParseRules(t *testing.T) {
	t.Run("iif rules", func(t *testing.T) {
		output := `0:	from all lookup local
5270:	not from all fwmark 0x80000/0xff0000 lookup 52
5280:	from all iif br0 lookup 53
5281:	from all iif br2 lookup 53
32766:	from all lookup main
32767:	from all lookup default
`
		rules := parseRules(output, "-4")
		assert.Len(t, rules, 2)
		assert.Equal(t, 5280, rules[0].Priority)
		assert.Equal(t, "", rules[0].Src)
		assert.Equal(t, "br0", rules[0].Iif)
		assert.Equal(t, 5281, rules[1].Priority)
		assert.Equal(t, "br2", rules[1].Iif)
	})

	t.Run("selective rules", func(t *testing.T) {
		output := `0:	from all lookup local
5281:	from 192.168.1.100 lookup 53
5282:	from 10.0.0.0/24 lookup 53
32766:	from all lookup main
`
		rules := parseRules(output, "-4")
		assert.Len(t, rules, 2)
		assert.Equal(t, "192.168.1.100", rules[0].Src)
		assert.Equal(t, "", rules[0].Iif)
		assert.Equal(t, "10.0.0.0/24", rules[1].Src)
	})
}

func TestConntrackFlush(t *testing.T) {
	state := newFakeIPRuleState()
	manifest := &mockExitManifest{}
	svc := NewExitNodeService(manifest, state.runner())
	stubBridges(svc, "br0")

	// Enable exit node — flush after routing change
	_ = svc.Apply(context.Background(), domain.ExitNodePolicy{Mode: domain.ExitNodeAll})
	assert.Equal(t, 1, state.conntrackFlushCount())

	// Disable exit node — flush after routing reverts
	_ = svc.Apply(context.Background(), domain.ExitNodePolicy{Mode: domain.ExitNodeOff})
	assert.Equal(t, 2, state.conntrackFlushCount())

	// Switch to selective — flush
	_ = svc.Apply(context.Background(), domain.ExitNodePolicy{
		Mode:    domain.ExitNodeSelective,
		Clients: []domain.ExitNodeClient{{IP: "10.0.0.1"}},
	})
	assert.Equal(t, 3, state.conntrackFlushCount())

	// Cleanup (shutdown) — flush
	_ = svc.Cleanup(context.Background())
	assert.Equal(t, 4, state.conntrackFlushCount())
}

func TestReconcileNoDriftSkipsFlush(t *testing.T) {
	state := newFakeIPRuleState()
	manifest := &mockExitManifest{}
	svc := NewExitNodeService(manifest, state.runner())
	stubBridges(svc, "br0")

	policy := domain.ExitNodePolicy{Mode: domain.ExitNodeAll}
	_ = svc.Apply(context.Background(), policy)
	flushBefore := state.conntrackFlushCount()

	// Reconcile with no drift — should NOT flush
	_ = svc.Reconcile(context.Background(), policy)
	assert.Equal(t, flushBefore, state.conntrackFlushCount())
}

func TestFamilyForAddr(t *testing.T) {
	assert.Equal(t, "-4", familyForAddr("192.168.1.1"))
	assert.Equal(t, "-4", familyForAddr("10.0.0.0/8"))
	assert.Equal(t, "-6", familyForAddr("fd00::1"))
	assert.Equal(t, "-6", familyForAddr("fd00::/64"))
	assert.Equal(t, "", familyForAddr("invalid"))
}

func TestMasqueradeOnApplyAll(t *testing.T) {
	state := newFakeIPRuleState()
	manifest := &mockExitManifest{}
	svc := NewExitNodeService(manifest, state.runner())
	stubBridges(svc, "br0")

	err := svc.Apply(context.Background(), domain.ExitNodePolicy{Mode: domain.ExitNodeAll})
	require.NoError(t, err)
	assert.Equal(t, 2, state.masqCount())

	err = svc.Apply(context.Background(), domain.ExitNodePolicy{Mode: domain.ExitNodeOff})
	require.NoError(t, err)
	assert.Equal(t, 0, state.masqCount())
}

func TestMasqueradeOnApplySelective(t *testing.T) {
	state := newFakeIPRuleState()
	manifest := &mockExitManifest{}
	svc := NewExitNodeService(manifest, state.runner())

	policy := domain.ExitNodePolicy{
		Mode:    domain.ExitNodeSelective,
		Clients: []domain.ExitNodeClient{{IP: "192.168.1.100"}},
	}
	err := svc.Apply(context.Background(), policy)
	require.NoError(t, err)
	assert.Equal(t, 2, state.masqCount())
}

func TestMasqueradeSelectiveEmptyClients(t *testing.T) {
	state := newFakeIPRuleState()
	manifest := &mockExitManifest{}
	svc := NewExitNodeService(manifest, state.runner())

	policy := domain.ExitNodePolicy{
		Mode:    domain.ExitNodeSelective,
		Clients: nil,
	}
	err := svc.Apply(context.Background(), policy)
	require.NoError(t, err)
	assert.Equal(t, 0, state.masqCount())
}

func TestMasqueradeCleanup(t *testing.T) {
	state := newFakeIPRuleState()
	manifest := &mockExitManifest{}
	svc := NewExitNodeService(manifest, state.runner())
	stubBridges(svc, "br0")

	_ = svc.Apply(context.Background(), domain.ExitNodePolicy{Mode: domain.ExitNodeAll})
	assert.Equal(t, 2, state.masqCount())

	err := svc.Cleanup(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, state.masqCount())
}

func TestReconcileMasqDrift(t *testing.T) {
	state := newFakeIPRuleState()
	manifest := &mockExitManifest{}
	svc := NewExitNodeService(manifest, state.runner())
	stubBridges(svc, "br0")

	policy := domain.ExitNodePolicy{Mode: domain.ExitNodeAll}
	_ = svc.Apply(context.Background(), policy)
	assert.Equal(t, 2, state.masqCount())

	// Simulate drift: someone removed masquerade rules
	state.mu.Lock()
	state.masqRules = make(map[string]bool)
	state.mu.Unlock()

	err := svc.Reconcile(context.Background(), policy)
	require.NoError(t, err)
	assert.Equal(t, 2, state.masqCount())
}

func TestApplyAllMultipleBridges(t *testing.T) {
	state := newFakeIPRuleState()
	manifest := &mockExitManifest{}
	svc := NewExitNodeService(manifest, state.runner())
	stubBridges(svc, "br0", "br2", "br5")

	err := svc.Apply(context.Background(), domain.ExitNodePolicy{Mode: domain.ExitNodeAll})
	require.NoError(t, err)
	assert.Equal(t, 6, state.ruleCount()) // 3 bridges × 2 families

	state.mu.Lock()
	assert.Len(t, state.rules["-4"], 3)
	assert.Contains(t, state.rules["-4"][0], "iif br0")
	assert.Contains(t, state.rules["-4"][0], "5280:")
	assert.Contains(t, state.rules["-4"][1], "iif br2")
	assert.Contains(t, state.rules["-4"][1], "5281:")
	assert.Contains(t, state.rules["-4"][2], "iif br5")
	assert.Contains(t, state.rules["-4"][2], "5282:")
	state.mu.Unlock()
}

func TestReconcileDetectsNewBridge(t *testing.T) {
	state := newFakeIPRuleState()
	manifest := &mockExitManifest{}
	svc := NewExitNodeService(manifest, state.runner())

	bridges := []string{"br0"}
	svc.discoverBridges = func() ([]string, error) { return bridges, nil }

	policy := domain.ExitNodePolicy{Mode: domain.ExitNodeAll}
	_ = svc.Apply(context.Background(), policy)
	assert.Equal(t, 2, state.ruleCount()) // 1 bridge × 2 families

	// New VLAN added — bridge appears
	bridges = []string{"br0", "br2"}

	err := svc.Reconcile(context.Background(), policy)
	require.NoError(t, err)
	assert.Equal(t, 4, state.ruleCount()) // 2 bridges × 2 families
}

func TestApplyAllNoBridgesError(t *testing.T) {
	state := newFakeIPRuleState()
	manifest := &mockExitManifest{}
	svc := NewExitNodeService(manifest, state.runner())
	stubBridges(svc)

	err := svc.Apply(context.Background(), domain.ExitNodePolicy{Mode: domain.ExitNodeAll})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no LAN bridge")
}
