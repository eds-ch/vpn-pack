package main

import (
	"context"
	"fmt"
	"net/netip"
	"sync/atomic"
	"testing"
	"time"

	"unifi-tailscale/manager/domain"
	"unifi-tailscale/manager/internal/wgs2s"
	"unifi-tailscale/manager/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"tailscale.com/health"
	"tailscale.com/ipn"
	"tailscale.com/ipn/ipnstate"
	"tailscale.com/tailcfg"
	"tailscale.com/types/key"
)

// BUG-M7: applyRefreshState must compute I/O-bound enrichment (wgManager
// status, firewall health, routing health) OUTSIDE the state mutex so a
// concurrent Snapshot() is not blocked behind subprocess / interface I/O.
func TestApplyRefreshState_DoesNotHoldMutexOverWgStatusIO(t *testing.T) {
	block := make(chan struct{})
	started := make(chan struct{})
	t.Cleanup(func() { close(block) })

	wgMock := &mockWgS2sControl{
		getStatusesFn: func() []wgs2s.WgS2sStatus {
			close(started)
			<-block
			return nil
		},
	}

	s := newTestServer(func(s *Server) {
		s.wgManager = wgMock
	})

	go s.applyRefreshState(context.Background(), nil, nil)

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("wgManager.GetStatuses was not invoked from applyRefreshState")
	}

	done := make(chan struct{})
	go func() {
		_ = s.state.Snapshot()
		close(done)
	}()

	select {
	case <-done:
		// Snapshot returned promptly — state mutex was free.
	case <-time.After(200 * time.Millisecond):
		t.Fatal("state.Snapshot blocked while applyRefreshState held the state mutex over wgManager.GetStatuses I/O (BUG-M7)")
	}
}

// BUG-M15: tailscale logout / backend transition to NeedsLogin should
// invalidate any previously-cached AuthURL. Without this, the UI keeps
// offering the now-defunct login link from a prior session until the
// next BrowseToURL/LoginFinished notification.
func TestUpdateStateFromNotify_ClearsAuthURLOnLogout(t *testing.T) {
	s := newTestServer()

	loginURL := "https://login.tailscale.com/admin/?key=tskey-auth-stale"
	s.updateStateFromNotify(&ipn.Notify{BrowseToURL: &loginURL})
	require.Equal(t, loginURL, s.state.Snapshot().AuthURL, "precondition: AuthURL must be set after BrowseToURL")

	needsLogin := ipn.NeedsLogin
	s.updateStateFromNotify(&ipn.Notify{State: &needsLogin})

	if got := s.state.Snapshot().AuthURL; got != "" {
		t.Fatalf("AuthURL must be cleared on logout/NeedsLogin transition; got %q", got)
	}
}

// Logout via Stopped state (e.g. tailscale down) should also drop the
// stale AuthURL.
func TestUpdateStateFromNotify_ClearsAuthURLOnStopped(t *testing.T) {
	s := newTestServer()

	loginURL := "https://login.tailscale.com/admin/?key=tskey-auth-stale"
	s.updateStateFromNotify(&ipn.Notify{BrowseToURL: &loginURL})

	stopped := ipn.Stopped
	s.updateStateFromNotify(&ipn.Notify{State: &stopped})

	if got := s.state.Snapshot().AuthURL; got != "" {
		t.Fatalf("AuthURL must be cleared on Stopped transition; got %q", got)
	}
}

// Sanity: the Running transition (mid-login) must not eat the AuthURL
// before the user has had a chance to follow it.
func TestUpdateStateFromNotify_PreservesAuthURLWhileRunning(t *testing.T) {
	s := newTestServer()

	loginURL := "https://login.tailscale.com/admin/?key=tskey-auth-live"
	s.updateStateFromNotify(&ipn.Notify{BrowseToURL: &loginURL})

	running := ipn.Running
	s.updateStateFromNotify(&ipn.Notify{State: &running})

	if got := s.state.Snapshot().AuthURL; got != loginURL {
		t.Fatalf("AuthURL must survive Running state; got %q want %q", got, loginURL)
	}
}

func TestExtractPeers_IncludesExitNodeFields(t *testing.T) {
	k1 := key.NewNode().Public()
	k2 := key.NewNode().Public()

	st := &ipnstate.Status{
		Peer: map[key.NodePublic]*ipnstate.PeerStatus{
			k1: {
				ID:             "peer-abc",
				HostName:       "exit-server",
				DNSName:        "exit-server.ts.net.",
				Online:         true,
				ExitNodeOption: true,
				ExitNode:       true,
			},
			k2: {
				ID:             "peer-xyz",
				HostName:       "laptop",
				DNSName:        "laptop.ts.net.",
				Online:         false,
				ExitNodeOption: false,
				ExitNode:       false,
			},
		},
	}

	peers := extractPeers(st)
	require.Len(t, peers, 2)

	byID := make(map[string]PeerInfo)
	for _, p := range peers {
		byID[p.ID] = p
	}

	exit := byID["peer-abc"]
	assert.Equal(t, "exit-server", exit.HostName)
	assert.True(t, exit.ExitNodeOption)
	assert.True(t, exit.ExitNode)

	laptop := byID["peer-xyz"]
	assert.Equal(t, "laptop", laptop.HostName)
	assert.False(t, laptop.ExitNodeOption)
	assert.False(t, laptop.ExitNode)
}

func TestExtractPeers_NilStatus(t *testing.T) {
	peers := extractPeers(nil)
	assert.Empty(t, peers)
}

func TestExtractPeers_SkipsShareeNodes(t *testing.T) {
	k1 := key.NewNode().Public()

	st := &ipnstate.Status{
		Peer: map[key.NodePublic]*ipnstate.PeerStatus{
			k1: {
				ID:         "peer-shared",
				HostName:   "shared-node",
				ShareeNode: true,
			},
		},
	}

	peers := extractPeers(st)
	assert.Empty(t, peers)
}

func TestBuildUsingExitNode_NoExitNodeStatus(t *testing.T) {
	s := newTestServer()
	st := &ipnstate.Status{}

	result := s.buildUsingExitNode(st)
	assert.Nil(t, result)
}

func TestBuildUsingExitNode_NoManifestRemoteExitNode(t *testing.T) {
	s := newTestServer()

	st := &ipnstate.Status{
		ExitNodeStatus: &ipnstate.ExitNodeStatus{
			ID:     "peer-abc",
			Online: true,
		},
	}

	result := s.buildUsingExitNode(st)
	assert.Nil(t, result, "should return nil when manifest has no remote exit node")
}

func TestBuildUsingExitNode_Active(t *testing.T) {
	k1 := key.NewNode().Public()

	s := newTestServer(func(s *Server) {
		s.manifest = &mockManifestStore{
			getRemoteExitNodeFn: func() *domain.RemoteExitNode {
				return &domain.RemoteExitNode{
					PeerID: "peer-abc",
					Mode:   domain.ExitNodeAll,
				}
			},
		}
	})

	st := &ipnstate.Status{
		ExitNodeStatus: &ipnstate.ExitNodeStatus{
			ID:     "peer-abc",
			Online: true,
		},
		Peer: map[key.NodePublic]*ipnstate.PeerStatus{
			k1: {
				ID:       "peer-abc",
				HostName: "exit-server",
				Online:   true,
			},
		},
	}

	result := s.buildUsingExitNode(st)
	require.NotNil(t, result)
	assert.Equal(t, "peer-abc", result.PeerID)
	assert.Equal(t, "exit-server", result.HostName)
	assert.True(t, result.Online)
	assert.Equal(t, "all", result.Mode)
}

func TestBuildUsingExitNode_PeerOffline(t *testing.T) {
	k1 := key.NewNode().Public()

	s := newTestServer(func(s *Server) {
		s.manifest = &mockManifestStore{
			getRemoteExitNodeFn: func() *domain.RemoteExitNode {
				return &domain.RemoteExitNode{
					PeerID: "peer-abc",
					Mode:   domain.ExitNodeSelective,
				}
			},
		}
	})

	st := &ipnstate.Status{
		ExitNodeStatus: &ipnstate.ExitNodeStatus{
			ID:     "peer-abc",
			Online: false,
		},
		Peer: map[key.NodePublic]*ipnstate.PeerStatus{
			k1: {
				ID:       "peer-abc",
				HostName: "exit-server",
				Online:   false,
			},
		},
	}

	result := s.buildUsingExitNode(st)
	require.NotNil(t, result)
	assert.False(t, result.Online)
	assert.Equal(t, "selective", result.Mode)
}

func TestBuildUsingExitNode_PeerNotInPeerList(t *testing.T) {
	s := newTestServer(func(s *Server) {
		s.manifest = &mockManifestStore{
			getRemoteExitNodeFn: func() *domain.RemoteExitNode {
				return &domain.RemoteExitNode{
					PeerID: "peer-abc",
					Mode:   domain.ExitNodeAll,
				}
			},
		}
	})

	st := &ipnstate.Status{
		ExitNodeStatus: &ipnstate.ExitNodeStatus{
			ID:     "peer-abc",
			Online: true,
		},
		Peer: map[key.NodePublic]*ipnstate.PeerStatus{},
	}

	result := s.buildUsingExitNode(st)
	require.NotNil(t, result)
	assert.Equal(t, "peer-abc", result.HostName, "falls back to peer ID as hostname")
	assert.True(t, result.Online, "uses ExitNodeStatus.Online when peer not found")
}

func TestBuildUsingExitNode_EmptyExitNodeID(t *testing.T) {
	s := newTestServer(func(s *Server) {
		s.manifest = &mockManifestStore{
			getRemoteExitNodeFn: func() *domain.RemoteExitNode {
				return &domain.RemoteExitNode{PeerID: "peer-abc", Mode: domain.ExitNodeAll}
			},
		}
	})

	st := &ipnstate.Status{
		ExitNodeStatus: &ipnstate.ExitNodeStatus{ID: "", Online: false},
	}

	result := s.buildUsingExitNode(st)
	assert.Nil(t, result, "empty exit node ID means no active exit node")
}

func TestRestoreExitNodeRules_NilRemoteExitNode_NoFlush(t *testing.T) {
	var conntrackFlushed atomic.Bool

	runner := func(_ context.Context, name string, args ...string) ([]byte, error) {
		if name == "conntrack" {
			conntrackFlushed.Store(true)
		}
		if name == "iptables" || name == "ip6tables" {
			return []byte(""), fmt.Errorf("rule not found")
		}
		return []byte(""), nil
	}

	manifest := &mockManifestStore{
		getRemoteExitNodeFn: func() *domain.RemoteExitNode { return nil },
	}

	s := newTestServer(func(s *Server) {
		s.manifest = manifest
	})
	s.exitSvc = service.NewExitNodeService(manifest, runner)

	s.restoreExitNodeRules(context.Background())
	assert.False(t, conntrackFlushed.Load(), "should not flush conntrack when no rules to clean up")
}

func TestRestoreExitNodeRules_ActiveRemote_Reconciles(t *testing.T) {
	var reconcileCalled atomic.Bool

	runner := func(_ context.Context, name string, args ...string) ([]byte, error) {
		reconcileCalled.Store(true)
		return []byte(""), nil
	}

	manifest := &mockManifestStore{
		getRemoteExitNodeFn: func() *domain.RemoteExitNode {
			return &domain.RemoteExitNode{
				PeerID: "peer-abc",
				Mode:   domain.ExitNodeAll,
			}
		},
		getExitNodePolicyFn: func() domain.ExitNodePolicy {
			return domain.ExitNodePolicy{Mode: domain.ExitNodeOff}
		},
	}

	ts := &mockTailscaleControl{
		getPrefsFn: func(ctx context.Context) (*ipn.Prefs, error) {
			return &ipn.Prefs{
				ExitNodeID: tailcfg.StableNodeID("peer-abc"),
			}, nil
		},
	}

	s := newTestServer(func(s *Server) {
		s.manifest = manifest
		s.ts = ts
	})
	s.exitSvc = service.NewExitNodeService(manifest, runner)
	s.remoteExitSvc = service.NewRemoteExitService(ts, s.exitSvc, manifest)

	s.restoreExitNodeRules(context.Background())
	assert.True(t, reconcileCalled.Load(), "should call Reconcile for active remote exit node")
}

func TestRestoreExitNodeRules_ExitSvcNil_Noop(t *testing.T) {
	s := newTestServer()
	s.exitSvc = nil

	s.restoreExitNodeRules(context.Background())
}

func TestRestoreExitNodeRules_TsNoExitNode_ClearsManifest(t *testing.T) {
	var editPrefsCalled atomic.Bool
	var setRemoteNode *domain.RemoteExitNode
	setRemoteCalled := false

	remoteNode := &domain.RemoteExitNode{
		PeerID: "peer-abc",
		Mode:   domain.ExitNodeAll,
	}

	manifest := &mockManifestStore{
		getRemoteExitNodeFn: func() *domain.RemoteExitNode {
			if setRemoteCalled {
				return nil
			}
			return remoteNode
		},
		setRemoteExitNodeFn: func(r *domain.RemoteExitNode) error {
			setRemoteCalled = true
			setRemoteNode = r
			return nil
		},
		getExitNodePolicyFn: func() domain.ExitNodePolicy {
			return domain.ExitNodePolicy{Mode: domain.ExitNodeOff}
		},
	}

	ts := &mockTailscaleControl{
		getPrefsFn: func(ctx context.Context) (*ipn.Prefs, error) {
			return &ipn.Prefs{ExitNodeID: ""}, nil
		},
		editPrefsFn: func(_ context.Context, mp *ipn.MaskedPrefs) (*ipn.Prefs, error) {
			editPrefsCalled.Store(true)
			return &ipn.Prefs{}, nil
		},
	}

	runner := func(_ context.Context, name string, args ...string) ([]byte, error) {
		return []byte(""), nil
	}

	s := newTestServer(func(s *Server) {
		s.manifest = manifest
		s.ts = ts
	})
	s.exitSvc = service.NewExitNodeService(manifest, runner)
	s.remoteExitSvc = service.NewRemoteExitService(ts, s.exitSvc, manifest)

	s.restoreExitNodeRules(context.Background())
	assert.False(t, editPrefsCalled.Load(), "should NOT call EditPrefs (reverse sync clears manifest, not Tailscale)")
	assert.True(t, setRemoteCalled, "should call SetRemoteExitNode")
	assert.Nil(t, setRemoteNode, "should clear manifest (SetRemoteExitNode(nil))")
}

// BUG-TS1102: tailscaled >= 1.100 gates Notify.NetMap behind
// goosGetsLegacyNetmapNotify (Windows only). On Linux the netmap arrives once,
// in the initial notify, and never again — self-node changes are delivered as
// Notify.SelfChange instead. Without handling SelfChange, a login completed
// after the manager started leaves TailscaleIPs / Self / AllowedIPs empty for
// the lifetime of the IPN bus session, so the UI shows a Running node with no
// address and every advertised route as unapproved.
func TestUpdateStateFromNotify_SelfChangeUpdatesSelfState(t *testing.T) {
	s := newTestServer()

	running := ipn.Running
	s.updateStateFromNotify(&ipn.Notify{State: &running})
	require.Nil(t, s.state.Snapshot().Self, "precondition: no self state before any netmap/self notify")

	hi := &tailcfg.Hostinfo{Hostname: "udm-se"}
	s.updateStateFromNotify(&ipn.Notify{SelfChange: &tailcfg.Node{
		Name:      "udm-se.tail1234.ts.net.",
		Addresses: []netip.Prefix{netip.MustParsePrefix("100.64.0.5/32")},
		AllowedIPs: []netip.Prefix{
			netip.MustParsePrefix("100.64.0.5/32"),
			netip.MustParsePrefix("192.168.1.0/24"),
		},
		Hostinfo: hi.View(),
	}})

	snap := s.state.Snapshot()
	require.NotNil(t, snap.Self, "SelfChange must populate self state")
	assert.Equal(t, "udm-se", snap.Self.HostName)
	assert.Equal(t, "udm-se.tail1234.ts.net.", snap.Self.DNSName)
	assert.True(t, snap.Self.Online, "self is online while backend state is Running")
	assert.Equal(t, []string{"100.64.0.5"}, snap.TailscaleIPs)
	assert.Len(t, s.state.AllowedIPs(), 2, "AllowedIPs feed recomputeRoutes; a stale empty set marks every advertised route unapproved")
}

// BUG-TS1102: the DERP latency table is built from the self node's NetInfo plus
// the DERP region catalogue. The catalogue used to ride along on the netmap
// notify; when self state now arrives via SelfChange the region names must be
// fetched from the daemon, otherwise the DERP panel stays empty after a
// login that happened after the manager started.
func TestUpdateStateFromNotify_SelfChangePopulatesDERPFromDaemonMap(t *testing.T) {
	derpCalls := 0
	ts := &mockTailscaleControl{
		currentDERPMapFn: func(ctx context.Context) (*tailcfg.DERPMap, error) {
			derpCalls++
			return &tailcfg.DERPMap{Regions: map[int]*tailcfg.DERPRegion{
				10: {RegionID: 10, RegionCode: "fra", RegionName: "Frankfurt"},
			}}, nil
		},
	}
	s := newTestServer(func(s *Server) { s.ts = ts })

	hi := &tailcfg.Hostinfo{
		Hostname: "udm-se",
		NetInfo:  &tailcfg.NetInfo{PreferredDERP: 10, DERPLatency: map[string]float64{"10-v4": 0.0231}},
	}
	s.processNotify(context.Background(), &ipn.Notify{SelfChange: &tailcfg.Node{
		Name:      "udm-se.tail1234.ts.net.",
		Addresses: []netip.Prefix{netip.MustParsePrefix("100.64.0.5/32")},
		Hostinfo:  hi.View(),
	}})

	derp := s.state.Snapshot().DERP
	require.Len(t, derp, 1, "DERP table must be built from the daemon's DERP map")
	assert.Equal(t, "fra", derp[0].RegionCode)
	assert.Equal(t, 10, derp[0].RegionID)
	assert.True(t, derp[0].Preferred)

	s.processNotify(context.Background(), &ipn.Notify{SelfChange: &tailcfg.Node{
		Name:      "udm-se.tail1234.ts.net.",
		Addresses: []netip.Prefix{netip.MustParsePrefix("100.64.0.5/32")},
		Hostinfo:  hi.View(),
	}})
	assert.Equal(t, 1, derpCalls, "DERP map must be cached, not refetched on every SelfChange")
}

// BUG-TS1102: nm.Domain was the only source of the tailnet name. With the
// netmap no longer arriving on Linux, the name must come from the status
// refresh that already runs every few seconds.
func TestApplyEnrichment_TakesTailnetNameFromStatus(t *testing.T) {
	ts := &mockTailscaleControl{
		statusFn: func(ctx context.Context) (*ipnstate.Status, error) {
			return &ipnstate.Status{
				BackendState:   "Running",
				CurrentTailnet: &ipnstate.TailnetStatus{Name: "tail1234.ts.net"},
			}, nil
		},
	}
	s := newTestServer(func(s *Server) { s.ts = ts })

	e := s.fetchStatusEnrichment(context.Background())
	require.NotNil(t, e)
	s.state.Update(func(d *stateData) { s.applyEnrichment(d, e) })

	assert.Equal(t, "tail1234.ts.net", s.state.Snapshot().TailnetName)
}

// BUG-DERP-EMPTY: LocalClient.CurrentDERPMap never returns nil — when the
// daemon has no netmap yet it encodes `null`, which unmarshals into an empty
// DERPMap. Caching that empty value would pin derpRegions() to nil for the
// process lifetime, so the DERP panel would stay empty forever after a logout
// / re-login even though the daemon later has a perfectly good catalogue.
func TestEnsureDERPMap_DoesNotCacheEmptyCatalogue(t *testing.T) {
	var calls int
	ts := &mockTailscaleControl{
		currentDERPMapFn: func(ctx context.Context) (*tailcfg.DERPMap, error) {
			calls++
			if calls == 1 {
				return &tailcfg.DERPMap{}, nil // daemon has no netmap yet
			}
			return &tailcfg.DERPMap{Regions: map[int]*tailcfg.DERPRegion{
				10: {RegionID: 10, RegionCode: "fra", RegionName: "Frankfurt"},
			}}, nil
		},
	}
	s := newTestServer(func(s *Server) { s.ts = ts })

	notify := func() *ipn.Notify {
		hi := &tailcfg.Hostinfo{
			Hostname: "udm-se",
			NetInfo:  &tailcfg.NetInfo{PreferredDERP: 10, DERPLatency: map[string]float64{"10-v4": 0.0231}},
		}
		return &ipn.Notify{SelfChange: &tailcfg.Node{
			Name:      "udm-se.tail1234.ts.net.",
			Addresses: []netip.Prefix{netip.MustParsePrefix("100.64.0.5/32")},
			Hostinfo:  hi.View(),
		}}
	}

	s.processNotify(context.Background(), notify())
	require.Empty(t, s.state.Snapshot().DERP, "precondition: empty catalogue yields no DERP rows")

	s.processNotify(context.Background(), notify())
	assert.Equal(t, 2, calls, "an empty catalogue must not be cached; the next SelfChange has to retry")
	require.Len(t, s.state.Snapshot().DERP, 1, "DERP must populate once the daemon returns a catalogue")
}

// BUG-DERP-STALE: the catalogue used to be refreshed by every netmap notify.
// Now that it is fetched once and cached, a region the node has since been
// moved to would be missing from the cache, and buildDERPInfo silently drops
// rows whose region is unknown — the node's own preferred relay would vanish
// from the panel until a manager restart.
func TestEnsureDERPMap_RefetchesWhenPreferredRegionIsUnknown(t *testing.T) {
	var calls int
	ts := &mockTailscaleControl{
		currentDERPMapFn: func(ctx context.Context) (*tailcfg.DERPMap, error) {
			calls++
			regions := map[int]*tailcfg.DERPRegion{10: {RegionID: 10, RegionCode: "fra", RegionName: "Frankfurt"}}
			if calls > 1 {
				regions[20] = &tailcfg.DERPRegion{RegionID: 20, RegionCode: "waw", RegionName: "Warsaw"}
			}
			return &tailcfg.DERPMap{Regions: regions}, nil
		},
	}
	s := newTestServer(func(s *Server) { s.ts = ts })

	selfIn := func(region int) *ipn.Notify {
		hi := &tailcfg.Hostinfo{
			Hostname: "udm-se",
			NetInfo: &tailcfg.NetInfo{
				PreferredDERP: region,
				DERPLatency:   map[string]float64{fmt.Sprintf("%d-v4", region): 0.0231},
			},
		}
		return &ipn.Notify{SelfChange: &tailcfg.Node{
			Name:      "udm-se.tail1234.ts.net.",
			Addresses: []netip.Prefix{netip.MustParsePrefix("100.64.0.5/32")},
			Hostinfo:  hi.View(),
		}}
	}

	s.processNotify(context.Background(), selfIn(10))
	require.Len(t, s.state.Snapshot().DERP, 1)

	s.processNotify(context.Background(), selfIn(20))
	assert.Equal(t, 2, calls, "a preferred region missing from the cache must trigger a refetch")
	derp := s.state.Snapshot().DERP
	require.Len(t, derp, 1, "the node's new preferred region must not be dropped")
	assert.Equal(t, "waw", derp[0].RegionCode)
}

// The manager subscribed to the IPN bus already receives the full
// health.UnhealthyState for every unhealthy warnable, but used to keep only
// the map key. During the 2026-08-10 incident that discarded value was the
// only thing that could have told the user the node was waiting on the
// coordination server rather than failing to start.
func TestUpdateStateFromNotify_KeepsHealthWarningDetail(t *testing.T) {
	s := newTestServer()

	s.updateStateFromNotify(&ipn.Notify{Health: &health.State{
		Warnings: map[health.WarnableCode]health.UnhealthyState{
			"not-in-map-poll": {
				WarnableCode:        "not-in-map-poll",
				Severity:            health.SeverityMedium,
				Title:               "Out of sync",
				Text:                "Unable to connect to the Tailscale coordination server to synchronize the state of your tailnet.",
				ImpactsConnectivity: true,
			},
		},
	}})

	got := s.state.Snapshot().Health
	require.Len(t, got, 1)
	require.Equal(t, domain.HealthWarning{
		Code:                "not-in-map-poll",
		Title:               "Out of sync",
		Text:                "Unable to connect to the Tailscale coordination server to synchronize the state of your tailnet.",
		Severity:            "medium",
		ImpactsConnectivity: true,
	}, got[0])
}

// Go map iteration order is randomised per run. Emitting the warnings in that
// order would make two identical health states serialise differently and push
// meaningless diffs at every SSE broadcast, and would make any ordering
// assertion downstream flaky.
func TestUpdateStateFromNotify_HealthWarningsSortedByCode(t *testing.T) {
	s := newTestServer()

	s.updateStateFromNotify(&ipn.Notify{Health: &health.State{
		Warnings: map[health.WarnableCode]health.UnhealthyState{
			"warming-up":      {Severity: health.SeverityLow, Title: "Tailscale is starting"},
			"no-derp-home":    {Severity: health.SeverityHigh, Title: "No home relay server"},
			"not-in-map-poll": {Severity: health.SeverityMedium, Title: "Out of sync"},
		},
	}})

	got := s.state.Snapshot().Health
	require.Len(t, got, 3)
	require.Equal(t, []string{"no-derp-home", "not-in-map-poll", "warming-up"},
		[]string{got[0].Code, got[1].Code, got[2].Code})
}
