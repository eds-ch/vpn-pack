package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"sync/atomic"

	"tailscale.com/ipn"
	"tailscale.com/ipn/ipnstate"
	"tailscale.com/tailcfg"

	"unifi-tailscale/manager/domain"
	"unifi-tailscale/manager/ops"
)

// ErrPartialDisable indicates EditPrefs succeeded but a subsequent step
// (local rules cleanup or manifest clear) failed. Callers must surface
// this to the operator — exit-node is off, but local state may be stale.
var ErrPartialDisable = errors.New("exit-node disabled with cleanup errors")

type RemoteExitManifest interface {
	GetRemoteExitNode() *domain.RemoteExitNode
	SetRemoteExitNode(r *domain.RemoteExitNode) error
	SetAdvertiseExitNode(enabled bool) error
}

type RemoteExitService struct {
	ts       RoutingTailscale
	exitSvc  *ExitNodeService
	manifest RemoteExitManifest
	applying atomic.Bool
}

func NewRemoteExitService(ts RoutingTailscale, exitSvc *ExitNodeService, manifest RemoteExitManifest) *RemoteExitService {
	return &RemoteExitService{ts: ts, exitSvc: exitSvc, manifest: manifest}
}

type ExitNodePeer struct {
	ID       string `json:"id"`
	HostName string `json:"hostName"`
	DNSName  string `json:"dnsName"`
	Online   bool   `json:"online"`
	OS       string `json:"os"`
	Active   bool   `json:"active"`
}

type RemoteExitResponse struct {
	Peers   []ExitNodePeer              `json:"peers"`
	Current *domain.RemoteExitNodeStatus `json:"current,omitempty"`
}

type EnableRemoteExitRequest struct {
	PeerID  string               `json:"peerId"`
	Mode    domain.ExitNodeMode  `json:"mode"`
	Clients []domain.ExitNodeClient `json:"clients,omitempty"`
	Confirm bool                 `json:"confirm"`
}

type EnableRemoteExitResult struct {
	OK              bool   `json:"ok"`
	Message         string `json:"message"`
	ConfirmRequired bool   `json:"confirmRequired,omitempty"`
}

func (svc *RemoteExitService) GetAvailable(ctx context.Context) (*RemoteExitResponse, error) {
	st, err := svc.ts.Status(ctx)
	if err != nil {
		return nil, upstreamError(humanizeLocalAPIError(err), err)
	}

	peers := filterExitNodePeers(st)
	resp := &RemoteExitResponse{Peers: peers}

	if current := svc.currentExitNodeStatus(st); current != nil {
		resp.Current = current
	}

	return resp, nil
}

func (svc *RemoteExitService) Enable(ctx context.Context, req *EnableRemoteExitRequest) (*EnableRemoteExitResult, error) {
	if req.PeerID == "" {
		return nil, validationError("peerId is required")
	}

	st, err := svc.ts.Status(ctx)
	if err != nil {
		return nil, upstreamError(humanizeLocalAPIError(err), err)
	}

	peer := findPeerByID(st, req.PeerID)
	if peer == nil {
		return nil, notFoundError(fmt.Sprintf("peer %s not found", req.PeerID))
	}
	if !peer.ExitNodeOption {
		return nil, validationError(fmt.Sprintf("peer %s is not an exit node option", peer.HostName))
	}

	mode := req.Mode
	if mode == "" {
		mode = domain.ExitNodeAll
	}

	prefs, err := svc.ts.GetPrefs(ctx)
	if err != nil {
		return nil, upstreamError(humanizeLocalAPIError(err), err)
	}
	wasAdvertising := prefs.AdvertisesExitNode()

	if wasAdvertising && !req.Confirm {
		msg := "Advertise as Exit Node will be disabled. "
		if mode == domain.ExitNodeAll {
			msg += fmt.Sprintf(
				"All internet traffic from ALL clients behind this router will be routed through %s. "+
					"Direct internet access will be lost.", peer.HostName)
		} else {
			msg += fmt.Sprintf("Selected clients will be routed through %s.", peer.HostName)
		}
		return &EnableRemoteExitResult{ConfirmRequired: true, Message: msg}, nil
	}

	if !wasAdvertising && mode == domain.ExitNodeAll && !req.Confirm {
		return &EnableRemoteExitResult{
			ConfirmRequired: true,
			Message: fmt.Sprintf(
				"All internet traffic from ALL clients behind this router will be routed through %s. "+
					"Direct internet access will be lost.", peer.HostName),
		}, nil
	}

	policy := domain.ExitNodePolicy{Mode: mode, Clients: req.Clients}
	if mode == domain.ExitNodeSelective {
		if err := ValidateExitNodePolicy(policy); err != nil {
			return nil, err
		}
	}

	svc.applying.Store(true)
	defer svc.applying.Store(false)

	prevRemote := svc.manifest.GetRemoteExitNode()
	newRemote := &domain.RemoteExitNode{
		PeerID:  req.PeerID,
		Mode:    mode,
		Clients: req.Clients,
	}

	mp := &ipn.MaskedPrefs{
		Prefs: ipn.Prefs{
			ExitNodeID:             tailcfg.StableNodeID(req.PeerID),
			ExitNodeAllowLANAccess: true,
		},
		ExitNodeIDSet:             true,
		ExitNodeAllowLANAccessSet: true,
	}
	if wasAdvertising {
		mp.AdvertiseRoutes = filterNonExitRoutes(prefs.AdvertiseRoutes)
		mp.AdvertiseRoutesSet = true
	}

	// Restore-prefs for EditPrefs undo. Captures the pre-Enable state so a
	// later failure can revert in a single masked-prefs round-trip.
	restorePrefs := &ipn.MaskedPrefs{
		Prefs: ipn.Prefs{
			ExitNodeID:             prefs.ExitNodeID,
			ExitNodeAllowLANAccess: prefs.ExitNodeAllowLANAccess,
		},
		ExitNodeIDSet:             true,
		ExitNodeAllowLANAccessSet: true,
	}
	if wasAdvertising {
		restorePrefs.AdvertiseRoutes = prefs.AdvertiseRoutes
		restorePrefs.AdvertiseRoutesSet = true
	}

	steps := []ops.Op{
		{
			Name: "persist manifest with new peer",
			Do:   func(_ context.Context) error { return svc.manifest.SetRemoteExitNode(newRemote) },
			Undo: func(_ context.Context) error { return svc.manifest.SetRemoteExitNode(prevRemote) },
		},
	}
	if wasAdvertising {
		steps = append(steps, ops.Op{
			Name: "clear advertise-exit-node in manifest",
			Do:   func(_ context.Context) error { return svc.manifest.SetAdvertiseExitNode(false) },
			Undo: func(_ context.Context) error { return svc.manifest.SetAdvertiseExitNode(true) },
		})
	}
	if svc.exitSvc != nil {
		steps = append(steps, ops.Op{
			Name: "apply local exit-node rules",
			Do:   func(ctx context.Context) error { return svc.exitSvc.Apply(ctx, policy) },
			Undo: func(ctx context.Context) error { return svc.exitSvc.Cleanup(ctx) },
		})
	}
	steps = append(steps, ops.Op{
		Name: "set Tailscale prefs",
		Do: func(ctx context.Context) error {
			_, err := svc.ts.EditPrefs(ctx, mp)
			return err
		},
		Undo: func(ctx context.Context) error {
			_, err := svc.ts.EditPrefs(ctx, restorePrefs)
			return err
		},
	})

	if err := ops.Run(ctx, steps); err != nil {
		return nil, upstreamError(humanizeLocalAPIError(err), err)
	}

	return &EnableRemoteExitResult{
		OK:      true,
		Message: fmt.Sprintf("Traffic routed through %s.", peer.HostName),
	}, nil
}

func (svc *RemoteExitService) Disable(ctx context.Context) error {
	_, err := svc.ts.EditPrefs(ctx, &ipn.MaskedPrefs{
		Prefs: ipn.Prefs{
			ExitNodeID:             "",
			ExitNodeAllowLANAccess: false,
		},
		ExitNodeIDSet:             true,
		ExitNodeAllowLANAccessSet: true,
	})
	if err != nil {
		return upstreamError(humanizeLocalAPIError(err), err)
	}

	var partial []error
	if svc.exitSvc != nil {
		if err := svc.exitSvc.Cleanup(ctx); err != nil {
			slog.Warn("exit node rules cleanup failed", "err", err)
			partial = append(partial, fmt.Errorf("cleanup local rules: %w", err))
		}
	}
	if err := svc.manifest.SetRemoteExitNode(nil); err != nil {
		slog.Warn("failed to clear remote exit node from manifest", "err", err)
		partial = append(partial, fmt.Errorf("clear manifest: %w", err))
	}

	if len(partial) > 0 {
		return errors.Join(append([]error{ErrPartialDisable}, partial...)...)
	}
	return nil
}

func (svc *RemoteExitService) SyncManifestFromTailscale(ctx context.Context) error {
	if svc.applying.Load() {
		return nil
	}

	prefs, err := svc.ts.GetPrefs(ctx)
	if err != nil {
		return fmt.Errorf("get prefs: %w", err)
	}

	tsExitNode := string(prefs.ExitNodeID)
	rem := svc.manifest.GetRemoteExitNode()

	if tsExitNode == "" {
		if rem != nil {
			slog.Info("exit node disabled externally, clearing manifest",
				"previousPeer", rem.PeerID)
			if svc.exitSvc != nil {
				if err := svc.exitSvc.Cleanup(ctx); err != nil {
					slog.Warn("exit node rules cleanup after external disable", "err", err)
				}
			}
			return svc.manifest.SetRemoteExitNode(nil)
		}
		return nil
	}

	if rem != nil && rem.PeerID == tsExitNode {
		return nil
	}

	if rem == nil {
		slog.Info("exit node active without manifest, creating entry",
			"peerId", tsExitNode)
		return svc.manifest.SetRemoteExitNode(&domain.RemoteExitNode{
			PeerID: tsExitNode,
			Mode:   domain.ExitNodeAll,
		})
	}

	slog.Info("exit node peer changed externally, updating manifest",
		"from", rem.PeerID, "to", tsExitNode)
	return svc.manifest.SetRemoteExitNode(&domain.RemoteExitNode{
		PeerID:  tsExitNode,
		Mode:    rem.Mode,
		Clients: rem.Clients,
	})
}

func filterExitNodePeers(st *ipnstate.Status) []ExitNodePeer {
	if st == nil || st.Peer == nil {
		return []ExitNodePeer{}
	}
	var peers []ExitNodePeer
	for _, p := range st.Peer {
		if !p.ExitNodeOption {
			continue
		}
		peers = append(peers, ExitNodePeer{
			ID:       string(p.ID),
			HostName: p.HostName,
			DNSName:  p.DNSName,
			Online:   p.Online,
			OS:       p.OS,
			Active:   p.ExitNode,
		})
	}
	if peers == nil {
		peers = []ExitNodePeer{}
	}
	return peers
}

func findPeerByID(st *ipnstate.Status, peerID string) *ipnstate.PeerStatus {
	if st == nil || st.Peer == nil {
		return nil
	}
	target := tailcfg.StableNodeID(peerID)
	for _, p := range st.Peer {
		if p.ID == target {
			return p
		}
	}
	return nil
}

func (svc *RemoteExitService) currentExitNodeStatus(st *ipnstate.Status) *domain.RemoteExitNodeStatus {
	return BuildRemoteExitNodeStatus(st, svc.manifest.GetRemoteExitNode())
}

func filterNonExitRoutes(routes []netip.Prefix) []netip.Prefix {
	out := make([]netip.Prefix, 0, len(routes))
	for _, r := range routes {
		if r.Bits() != 0 {
			out = append(out, r)
		}
	}
	return out
}

func BuildRemoteExitNodeStatus(st *ipnstate.Status, rem *domain.RemoteExitNode) *domain.RemoteExitNodeStatus {
	if st == nil || st.ExitNodeStatus == nil || rem == nil {
		return nil
	}

	exitID := string(st.ExitNodeStatus.ID)
	if exitID == "" {
		return nil
	}

	hostName := exitID
	online := st.ExitNodeStatus.Online
	if peer := findPeerByID(st, exitID); peer != nil {
		hostName = peer.HostName
		online = peer.Online
	}

	mode := string(rem.Mode)
	if mode == "" {
		mode = string(domain.ExitNodeAll)
	}

	return &domain.RemoteExitNodeStatus{
		PeerID:   exitID,
		HostName: hostName,
		Online:   online,
		Mode:     mode,
		Clients:  rem.Clients,
	}
}

