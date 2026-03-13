package main

import (
	"context"
	"sync/atomic"
	"testing"

	"unifi-tailscale/manager/domain"
	"unifi-tailscale/manager/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"tailscale.com/ipn"
	"tailscale.com/ipn/ipnstate"
	"tailscale.com/tailcfg"
	"tailscale.com/types/key"
)

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

func TestRestoreExitNodeRules_NilRemoteExitNode_CleansUp(t *testing.T) {
	var cleanupCalled atomic.Bool

	runner := func(_ context.Context, name string, args ...string) ([]byte, error) {
		cleanupCalled.Store(true)
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
	assert.True(t, cleanupCalled.Load(), "should call Cleanup when no remote exit node configured")
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
