package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"tailscale.com/ipn"
	"tailscale.com/ipn/ipnstate"
	"tailscale.com/tailcfg"
	"tailscale.com/types/key"

	"unifi-tailscale/manager/domain"
)

// --- Mocks ---

type mockRemoteExitManifest struct {
	remoteExitNode    *domain.RemoteExitNode
	advertiseEnabled  bool
	setRemoteErr      error
}

func (m *mockRemoteExitManifest) GetRemoteExitNode() *domain.RemoteExitNode {
	if m.remoteExitNode == nil {
		return nil
	}
	cp := *m.remoteExitNode
	return &cp
}

func (m *mockRemoteExitManifest) SetRemoteExitNode(r *domain.RemoteExitNode) error {
	if m.setRemoteErr != nil {
		return m.setRemoteErr
	}
	if r != nil {
		cp := *r
		m.remoteExitNode = &cp
	} else {
		m.remoteExitNode = nil
	}
	return nil
}

func (m *mockRemoteExitManifest) GetAdvertiseExitNodeEnabled() bool {
	return m.advertiseEnabled
}

// --- Helpers ---

func testPeerStatus(id string, hostName string, online bool, exitNodeOption bool, exitNode bool) *ipnstate.PeerStatus {
	return &ipnstate.PeerStatus{
		ID:             tailcfg.StableNodeID(id),
		HostName:       hostName,
		DNSName:        hostName + ".ts.net.",
		Online:         online,
		OS:             "linux",
		ExitNodeOption: exitNodeOption,
		ExitNode:       exitNode,
	}
}

func testStatusWithPeers(peers ...*ipnstate.PeerStatus) *ipnstate.Status {
	st := &ipnstate.Status{
		Peer: make(map[key.NodePublic]*ipnstate.PeerStatus, len(peers)),
	}
	for _, p := range peers {
		k := key.NewNode().Public()
		st.Peer[k] = p
	}
	return st
}

func newTestRemoteExitService(ts RoutingTailscale, manifest *mockRemoteExitManifest) *RemoteExitService {
	state := newFakeIPRuleState()
	exitSvc := NewExitNodeService(&mockExitManifest{}, state.runner())
	return NewRemoteExitService(ts, exitSvc, manifest)
}

// --- GetAvailable tests ---

func TestGetAvailable_FiltersByExitNodeOption(t *testing.T) {
	ts := &mockRoutingTailscale{
		statusFn: func(ctx context.Context) (*ipnstate.Status, error) {
			return testStatusWithPeers(
				testPeerStatus("stable-1", "exit-server", true, true, false),
				testPeerStatus("stable-2", "regular-node", true, false, false),
				testPeerStatus("stable-3", "another-exit", false, true, false),
			), nil
		},
	}
	manifest := &mockRemoteExitManifest{}
	svc := newTestRemoteExitService(ts, manifest)

	resp, err := svc.GetAvailable(context.Background())
	require.NoError(t, err)
	require.Len(t, resp.Peers, 2)

	ids := map[string]bool{}
	for _, p := range resp.Peers {
		ids[p.ID] = true
	}
	assert.True(t, ids["stable-1"])
	assert.True(t, ids["stable-3"])
	assert.Nil(t, resp.Current)
}

func TestGetAvailable_ShowsCurrentExitNode(t *testing.T) {
	ts := &mockRoutingTailscale{
		statusFn: func(ctx context.Context) (*ipnstate.Status, error) {
			st := testStatusWithPeers(
				testPeerStatus("stable-1", "exit-server", true, true, true),
			)
			st.ExitNodeStatus = &ipnstate.ExitNodeStatus{
				ID:     "stable-1",
				Online: true,
			}
			return st, nil
		},
	}
	manifest := &mockRemoteExitManifest{
		remoteExitNode: &domain.RemoteExitNode{
			PeerID: "stable-1",
			Mode:   domain.ExitNodeAll,
		},
	}
	svc := newTestRemoteExitService(ts, manifest)

	resp, err := svc.GetAvailable(context.Background())
	require.NoError(t, err)
	require.Len(t, resp.Peers, 1)
	assert.True(t, resp.Peers[0].Active)
	require.NotNil(t, resp.Current)
	assert.Equal(t, "stable-1", resp.Current.PeerID)
	assert.Equal(t, "exit-server", resp.Current.HostName)
	assert.True(t, resp.Current.Online)
	assert.Equal(t, "all", resp.Current.Mode)
}

func TestGetAvailable_NoPeers(t *testing.T) {
	ts := &mockRoutingTailscale{
		statusFn: func(ctx context.Context) (*ipnstate.Status, error) {
			return &ipnstate.Status{}, nil
		},
	}
	manifest := &mockRemoteExitManifest{}
	svc := newTestRemoteExitService(ts, manifest)

	resp, err := svc.GetAvailable(context.Background())
	require.NoError(t, err)
	assert.Empty(t, resp.Peers)
	assert.Nil(t, resp.Current)
}

// --- Enable tests ---

func TestEnable_All(t *testing.T) {
	var editedPrefs ipn.MaskedPrefs
	ts := &mockRoutingTailscale{
		statusFn: func(ctx context.Context) (*ipnstate.Status, error) {
			return testStatusWithPeers(
				testPeerStatus("stable-1", "exit-server", true, true, false),
			), nil
		},
		editPrefsFn: func(ctx context.Context, mp *ipn.MaskedPrefs) (*ipn.Prefs, error) {
			editedPrefs = *mp
			return &ipn.Prefs{}, nil
		},
	}
	manifest := &mockRemoteExitManifest{}
	svc := newTestRemoteExitService(ts, manifest)

	result, err := svc.Enable(context.Background(), &EnableRemoteExitRequest{
		PeerID:  "stable-1",
		Mode:    domain.ExitNodeAll,
		Confirm: true,
	})
	require.NoError(t, err)
	assert.True(t, result.OK)
	assert.Contains(t, result.Message, "exit-server")

	assert.True(t, editedPrefs.ExitNodeIDSet)
	assert.Equal(t, tailcfg.StableNodeID("stable-1"), editedPrefs.ExitNodeID)
	assert.True(t, editedPrefs.ExitNodeAllowLANAccessSet)
	assert.True(t, editedPrefs.ExitNodeAllowLANAccess)

	require.NotNil(t, manifest.remoteExitNode)
	assert.Equal(t, "stable-1", manifest.remoteExitNode.PeerID)
	assert.Equal(t, domain.ExitNodeAll, manifest.remoteExitNode.Mode)
}

func TestEnable_Selective(t *testing.T) {
	ts := &mockRoutingTailscale{
		statusFn: func(ctx context.Context) (*ipnstate.Status, error) {
			return testStatusWithPeers(
				testPeerStatus("stable-1", "exit-server", true, true, false),
			), nil
		},
		editPrefsFn: func(ctx context.Context, mp *ipn.MaskedPrefs) (*ipn.Prefs, error) {
			return &ipn.Prefs{}, nil
		},
	}
	manifest := &mockRemoteExitManifest{}
	svc := newTestRemoteExitService(ts, manifest)

	clients := []domain.ExitNodeClient{{IP: "192.168.1.100", Label: "PC"}}
	result, err := svc.Enable(context.Background(), &EnableRemoteExitRequest{
		PeerID:  "stable-1",
		Mode:    domain.ExitNodeSelective,
		Clients: clients,
	})
	require.NoError(t, err)
	assert.True(t, result.OK)
	assert.False(t, result.ConfirmRequired)

	require.NotNil(t, manifest.remoteExitNode)
	assert.Equal(t, domain.ExitNodeSelective, manifest.remoteExitNode.Mode)
	require.Len(t, manifest.remoteExitNode.Clients, 1)
}

func TestEnable_Confirmation(t *testing.T) {
	ts := &mockRoutingTailscale{
		statusFn: func(ctx context.Context) (*ipnstate.Status, error) {
			return testStatusWithPeers(
				testPeerStatus("stable-1", "exit-server", true, true, false),
			), nil
		},
	}
	manifest := &mockRemoteExitManifest{}
	svc := newTestRemoteExitService(ts, manifest)

	result, err := svc.Enable(context.Background(), &EnableRemoteExitRequest{
		PeerID:  "stable-1",
		Mode:    domain.ExitNodeAll,
		Confirm: false,
	})
	require.NoError(t, err)
	assert.True(t, result.ConfirmRequired)
	assert.Contains(t, result.Message, "ALL")
	assert.Nil(t, manifest.remoteExitNode)
}

func TestEnable_InvalidPeer_NotFound(t *testing.T) {
	ts := &mockRoutingTailscale{
		statusFn: func(ctx context.Context) (*ipnstate.Status, error) {
			return testStatusWithPeers(
				testPeerStatus("stable-1", "exit-server", true, true, false),
			), nil
		},
	}
	manifest := &mockRemoteExitManifest{}
	svc := newTestRemoteExitService(ts, manifest)

	_, err := svc.Enable(context.Background(), &EnableRemoteExitRequest{
		PeerID:  "nonexistent",
		Mode:    domain.ExitNodeAll,
		Confirm: true,
	})
	require.Error(t, err)
	var se *Error
	require.ErrorAs(t, err, &se)
	assert.Equal(t, ErrNotFound, se.Kind)
}

func TestEnable_InvalidPeer_NotExitNodeOption(t *testing.T) {
	ts := &mockRoutingTailscale{
		statusFn: func(ctx context.Context) (*ipnstate.Status, error) {
			return testStatusWithPeers(
				testPeerStatus("stable-1", "regular-node", true, false, false),
			), nil
		},
	}
	manifest := &mockRemoteExitManifest{}
	svc := newTestRemoteExitService(ts, manifest)

	_, err := svc.Enable(context.Background(), &EnableRemoteExitRequest{
		PeerID:  "stable-1",
		Mode:    domain.ExitNodeAll,
		Confirm: true,
	})
	require.Error(t, err)
	var se *Error
	require.ErrorAs(t, err, &se)
	assert.Equal(t, ErrValidation, se.Kind)
}

func TestEnable_SetsExitNodeAllowLANAccess(t *testing.T) {
	var editedPrefs ipn.MaskedPrefs
	ts := &mockRoutingTailscale{
		statusFn: func(ctx context.Context) (*ipnstate.Status, error) {
			return testStatusWithPeers(
				testPeerStatus("stable-1", "exit-server", true, true, false),
			), nil
		},
		editPrefsFn: func(ctx context.Context, mp *ipn.MaskedPrefs) (*ipn.Prefs, error) {
			editedPrefs = *mp
			return &ipn.Prefs{}, nil
		},
	}
	manifest := &mockRemoteExitManifest{}
	svc := newTestRemoteExitService(ts, manifest)

	_, err := svc.Enable(context.Background(), &EnableRemoteExitRequest{
		PeerID:  "stable-1",
		Mode:    domain.ExitNodeAll,
		Confirm: true,
	})
	require.NoError(t, err)

	assert.True(t, editedPrefs.ExitNodeAllowLANAccessSet,
		"ExitNodeAllowLANAccessSet must be true")
	assert.True(t, editedPrefs.ExitNodeAllowLANAccess,
		"ExitNodeAllowLANAccess must be true — without it the router loses LAN connectivity")
}

func TestEnable_EmptyPeerID(t *testing.T) {
	ts := &mockRoutingTailscale{}
	manifest := &mockRemoteExitManifest{}
	svc := newTestRemoteExitService(ts, manifest)

	_, err := svc.Enable(context.Background(), &EnableRemoteExitRequest{
		PeerID: "",
	})
	require.Error(t, err)
	var se *Error
	require.ErrorAs(t, err, &se)
	assert.Equal(t, ErrValidation, se.Kind)
}

func TestEnable_DefaultModeIsAll(t *testing.T) {
	ts := &mockRoutingTailscale{
		statusFn: func(ctx context.Context) (*ipnstate.Status, error) {
			return testStatusWithPeers(
				testPeerStatus("stable-1", "exit-server", true, true, false),
			), nil
		},
	}
	manifest := &mockRemoteExitManifest{}
	svc := newTestRemoteExitService(ts, manifest)

	// No mode specified, no confirm → should require confirmation (mode defaults to "all")
	result, err := svc.Enable(context.Background(), &EnableRemoteExitRequest{
		PeerID: "stable-1",
	})
	require.NoError(t, err)
	assert.True(t, result.ConfirmRequired)
}

func TestEnable_SelectiveInvalidClient(t *testing.T) {
	ts := &mockRoutingTailscale{
		statusFn: func(ctx context.Context) (*ipnstate.Status, error) {
			return testStatusWithPeers(
				testPeerStatus("stable-1", "exit-server", true, true, false),
			), nil
		},
	}
	manifest := &mockRemoteExitManifest{}
	svc := newTestRemoteExitService(ts, manifest)

	_, err := svc.Enable(context.Background(), &EnableRemoteExitRequest{
		PeerID:  "stable-1",
		Mode:    domain.ExitNodeSelective,
		Clients: []domain.ExitNodeClient{{IP: "not-valid"}},
	})
	require.Error(t, err)
	var se *Error
	require.ErrorAs(t, err, &se)
	assert.Equal(t, ErrValidation, se.Kind)
}

// --- Disable tests ---

func TestDisable(t *testing.T) {
	var editedPrefs ipn.MaskedPrefs
	ts := &mockRoutingTailscale{
		editPrefsFn: func(ctx context.Context, mp *ipn.MaskedPrefs) (*ipn.Prefs, error) {
			editedPrefs = *mp
			return &ipn.Prefs{}, nil
		},
	}
	manifest := &mockRemoteExitManifest{
		remoteExitNode: &domain.RemoteExitNode{
			PeerID: "stable-1",
			Mode:   domain.ExitNodeAll,
		},
	}
	svc := newTestRemoteExitService(ts, manifest)

	err := svc.Disable(context.Background())
	require.NoError(t, err)

	assert.True(t, editedPrefs.ExitNodeIDSet)
	assert.Equal(t, tailcfg.StableNodeID(""), editedPrefs.ExitNodeID)
	assert.Nil(t, manifest.remoteExitNode)
}

func TestDisable_ClearsExitNodeAllowLANAccess(t *testing.T) {
	var editedPrefs ipn.MaskedPrefs
	ts := &mockRoutingTailscale{
		editPrefsFn: func(ctx context.Context, mp *ipn.MaskedPrefs) (*ipn.Prefs, error) {
			editedPrefs = *mp
			return &ipn.Prefs{}, nil
		},
	}
	manifest := &mockRemoteExitManifest{
		remoteExitNode: &domain.RemoteExitNode{PeerID: "stable-1", Mode: domain.ExitNodeAll},
	}
	svc := newTestRemoteExitService(ts, manifest)

	err := svc.Disable(context.Background())
	require.NoError(t, err)

	assert.True(t, editedPrefs.ExitNodeAllowLANAccessSet)
	assert.False(t, editedPrefs.ExitNodeAllowLANAccess,
		"ExitNodeAllowLANAccess must be false after disable")
}

// --- SyncExitNodeID tests ---

func TestSyncExitNodeID_InSync(t *testing.T) {
	editCalled := false
	ts := &mockRoutingTailscale{
		getPrefsFn: func(ctx context.Context) (*ipn.Prefs, error) {
			return &ipn.Prefs{
				ExitNodeID: tailcfg.StableNodeID("stable-1"),
			}, nil
		},
		editPrefsFn: func(ctx context.Context, mp *ipn.MaskedPrefs) (*ipn.Prefs, error) {
			editCalled = true
			return &ipn.Prefs{}, nil
		},
	}
	manifest := &mockRemoteExitManifest{
		remoteExitNode: &domain.RemoteExitNode{PeerID: "stable-1", Mode: domain.ExitNodeAll},
	}
	svc := newTestRemoteExitService(ts, manifest)

	err := svc.SyncExitNodeID(context.Background())
	require.NoError(t, err)
	assert.False(t, editCalled, "EditPrefs should not be called when in sync")
}

func TestSyncExitNodeID_Diverged(t *testing.T) {
	var editedPrefs ipn.MaskedPrefs
	ts := &mockRoutingTailscale{
		getPrefsFn: func(ctx context.Context) (*ipn.Prefs, error) {
			return &ipn.Prefs{
				ExitNodeID: tailcfg.StableNodeID("wrong-id"),
			}, nil
		},
		editPrefsFn: func(ctx context.Context, mp *ipn.MaskedPrefs) (*ipn.Prefs, error) {
			editedPrefs = *mp
			return &ipn.Prefs{}, nil
		},
	}
	manifest := &mockRemoteExitManifest{
		remoteExitNode: &domain.RemoteExitNode{PeerID: "stable-1", Mode: domain.ExitNodeAll},
	}
	svc := newTestRemoteExitService(ts, manifest)

	err := svc.SyncExitNodeID(context.Background())
	require.NoError(t, err)

	assert.True(t, editedPrefs.ExitNodeIDSet)
	assert.Equal(t, tailcfg.StableNodeID("stable-1"), editedPrefs.ExitNodeID)
	assert.True(t, editedPrefs.ExitNodeAllowLANAccess)
}

func TestSyncExitNodeID_NoManifest(t *testing.T) {
	editCalled := false
	ts := &mockRoutingTailscale{
		editPrefsFn: func(ctx context.Context, mp *ipn.MaskedPrefs) (*ipn.Prefs, error) {
			editCalled = true
			return &ipn.Prefs{}, nil
		},
	}
	manifest := &mockRemoteExitManifest{}
	svc := newTestRemoteExitService(ts, manifest)

	err := svc.SyncExitNodeID(context.Background())
	require.NoError(t, err)
	assert.False(t, editCalled, "should no-op when manifest has no remote exit node")
}

// --- Warning tests ---

func TestAdvertiseAndRemote_Warning(t *testing.T) {
	ts := &mockRoutingTailscale{
		statusFn: func(ctx context.Context) (*ipnstate.Status, error) {
			return testStatusWithPeers(
				testPeerStatus("stable-1", "exit-server", true, true, false),
			), nil
		},
		editPrefsFn: func(ctx context.Context, mp *ipn.MaskedPrefs) (*ipn.Prefs, error) {
			return &ipn.Prefs{}, nil
		},
	}
	manifest := &mockRemoteExitManifest{advertiseEnabled: true}
	svc := newTestRemoteExitService(ts, manifest)

	result, err := svc.Enable(context.Background(), &EnableRemoteExitRequest{
		PeerID:  "stable-1",
		Mode:    domain.ExitNodeAll,
		Confirm: true,
	})
	require.NoError(t, err)
	assert.True(t, result.OK)
	assert.Contains(t, result.Warning, "routing loop")
}

func TestAdvertiseAndRemote_NoWarning(t *testing.T) {
	ts := &mockRoutingTailscale{
		statusFn: func(ctx context.Context) (*ipnstate.Status, error) {
			return testStatusWithPeers(
				testPeerStatus("stable-1", "exit-server", true, true, false),
			), nil
		},
		editPrefsFn: func(ctx context.Context, mp *ipn.MaskedPrefs) (*ipn.Prefs, error) {
			return &ipn.Prefs{}, nil
		},
	}
	manifest := &mockRemoteExitManifest{advertiseEnabled: false}
	svc := newTestRemoteExitService(ts, manifest)

	result, err := svc.Enable(context.Background(), &EnableRemoteExitRequest{
		PeerID:  "stable-1",
		Mode:    domain.ExitNodeAll,
		Confirm: true,
	})
	require.NoError(t, err)
	assert.Empty(t, result.Warning)
}
