package service

import (
	"context"
	"fmt"
	"net/netip"
	"strings"
	"time"

	"tailscale.com/ipn"
	"tailscale.com/ipn/ipnstate"

	"unifi-tailscale/manager/config"
	"unifi-tailscale/manager/domain"
)

type RoutingTailscale interface {
	GetPrefs(ctx context.Context) (*ipn.Prefs, error)
	EditPrefs(ctx context.Context, mp *ipn.MaskedPrefs) (*ipn.Prefs, error)
	Status(ctx context.Context) (*ipnstate.Status, error)
	Start(ctx context.Context, opts ipn.Options) error
}

type RoutingFirewall interface {
	IntegrationReady() bool
	CheckTailscaleRulesPresent(ctx context.Context) (forward, input, output, ipset bool)
}

type RoutingIntegration interface {
	HasAPIKey() bool
}

type RoutingManifest interface {
	GetTailscaleChainPrefix() string
}

type RouteStatus = domain.RouteStatus

type RoutesResponse struct {
	Routes   []RouteStatus `json:"routes"`
	ExitNode bool          `json:"exitNode"`
}

type SetRoutesRequest struct {
	Routes   []string `json:"routes"`
	ExitNode bool     `json:"exitNode"`
	Confirm  bool     `json:"confirm"`
}

type SetRoutesResult struct {
	OK              bool   `json:"ok"`
	Message         string `json:"message"`
	AdminURL        string `json:"adminURL"`
	Warning         string `json:"warning,omitempty"`
	ConfirmRequired bool   `json:"confirmRequired,omitempty"`
}

type SubnetEntry struct {
	CIDR string `json:"cidr"`
	Name string `json:"name"`
	Type string `json:"type"`
}

type FirewallState struct {
	WatcherRunning bool
	LastRestore    *time.Time
	UDAPIReachable bool
}

type FirewallStatusResponse struct {
	IntegrationAPI bool            `json:"integrationAPI"`
	ChainPrefix    string          `json:"chainPrefix"`
	WatcherRunning bool            `json:"watcherRunning"`
	LastRestore    *time.Time      `json:"lastRestore"`
	RulesPresent   map[string]bool `json:"rulesPresent"`
	UDAPIReachable bool            `json:"udapiReachable"`
}

type SubnetProvider func() []SubnetEntry

type RoutingService struct {
	ts       RoutingTailscale
	fw       RoutingFirewall
	ic       RoutingIntegration
	manifest RoutingManifest
	subnets  SubnetProvider
}

func NewRoutingService(
	ts RoutingTailscale,
	fw RoutingFirewall,
	ic RoutingIntegration,
	manifest RoutingManifest,
	subnets SubnetProvider,
) *RoutingService {
	return &RoutingService{ts: ts, fw: fw, ic: ic, manifest: manifest, subnets: subnets}
}

func (svc *RoutingService) GetRoutes(ctx context.Context) (*RoutesResponse, error) {
	prefs, err := svc.ts.GetPrefs(ctx)
	if err != nil {
		return nil, upstreamError(humanizeLocalAPIError(err), err)
	}

	allowed := make(map[string]bool)
	st, err := svc.ts.Status(ctx)
	if err == nil && st.Self != nil && st.Self.AllowedIPs != nil {
		for i := range st.Self.AllowedIPs.Len() {
			allowed[st.Self.AllowedIPs.At(i).String()] = true
		}
	}

	routes, isExit := BuildRouteStatuses(prefs.AdvertiseRoutes, allowed)
	return &RoutesResponse{Routes: routes, ExitNode: isExit}, nil
}

func (svc *RoutingService) SetRoutes(ctx context.Context, req *SetRoutesRequest, activeVPNClients []string) (*SetRoutesResult, error) {
	prefixes := make([]netip.Prefix, 0, len(req.Routes))
	for _, cidr := range req.Routes {
		p, err := netip.ParsePrefix(cidr)
		if err != nil {
			return nil, validationError(fmt.Sprintf("invalid CIDR: %s", cidr))
		}
		prefixes = append(prefixes, p.Masked())
	}

	if req.ExitNode && !req.Confirm {
		return &SetRoutesResult{
			ConfirmRequired: true,
			Message: "Exit node will redirect ALL internet traffic from ALL clients " +
				"behind this router (all VLANs, all devices) through Tailscale. " +
				"Direct internet access will be lost.",
		}, nil
	}

	var warning string
	if req.ExitNode {
		if len(activeVPNClients) > 0 {
			ifaces := strings.Join(activeVPNClients, ", ")
			warning = fmt.Sprintf(
				"Advertising is safe, but don't route this device's own traffic through a remote exit node — "+
					"Tailscale ip rules have higher priority and would override %s routing.", ifaces)
		}
		prefixes = append(prefixes,
			netip.MustParsePrefix("0.0.0.0/0"),
			netip.MustParsePrefix("::/0"))
	}

	_, err := svc.ts.EditPrefs(ctx, &ipn.MaskedPrefs{
		Prefs:              ipn.Prefs{AdvertiseRoutes: prefixes},
		AdvertiseRoutesSet: true,
	})
	if err != nil {
		return nil, upstreamError(humanizeLocalAPIError(err), err)
	}

	return &SetRoutesResult{
		OK:       true,
		Message:  "Routes applied locally. Approve in Tailscale admin console.",
		AdminURL: "https://login.tailscale.com/admin/machines",
		Warning:  warning,
	}, nil
}

func (svc *RoutingService) ActivateWithKey(ctx context.Context, authKey string) error {
	if svc.fw == nil || !svc.fw.IntegrationReady() {
		return preconditionError(ErrMsgIntegrationKeyRequired)
	}

	if authKey == "" {
		return validationError("auth key is required")
	}
	if !strings.HasPrefix(authKey, "tskey-") {
		return validationError("auth key must start with 'tskey-' prefix")
	}

	_, err := svc.ts.EditPrefs(ctx, &ipn.MaskedPrefs{
		Prefs:      ipn.Prefs{CorpDNS: false},
		CorpDNSSet: true,
	})
	if err != nil {
		return upstreamError(humanizeLocalAPIError(err), err)
	}

	if err := svc.ts.Start(ctx, ipn.Options{AuthKey: authKey}); err != nil {
		return upstreamError(humanizeLocalAPIError(err), err)
	}

	return nil
}

func (svc *RoutingService) GetSubnets() []SubnetEntry {
	if svc.subnets != nil {
		return svc.subnets()
	}
	return []SubnetEntry{}
}

func (svc *RoutingService) GetFirewallStatus(ctx context.Context, state FirewallState) *FirewallStatusResponse {
	forward, input, output, ipset := false, false, false, false
	if svc.fw != nil {
		forward, input, output, ipset = svc.fw.CheckTailscaleRulesPresent(ctx)
	}

	chainPrefix := config.DefaultChainPrefix
	if svc.manifest != nil {
		chainPrefix = svc.manifest.GetTailscaleChainPrefix()
	}

	return &FirewallStatusResponse{
		IntegrationAPI: svc.ic != nil && svc.ic.HasAPIKey(),
		ChainPrefix:    chainPrefix,
		WatcherRunning: state.WatcherRunning,
		LastRestore:    state.LastRestore,
		RulesPresent: map[string]bool{
			"forward": forward,
			"input":   input,
			"output":  output,
			"ipset":   ipset,
		},
		UDAPIReachable: state.UDAPIReachable,
	}
}

func BuildRouteStatuses(routes []netip.Prefix, allowed map[string]bool) ([]RouteStatus, bool) {
	var result []RouteStatus
	isExit := false
	for _, p := range routes {
		str := p.String()
		if str == "0.0.0.0/0" || str == "::/0" {
			isExit = true
			continue
		}
		result = append(result, RouteStatus{
			CIDR:     str,
			Approved: allowed[str],
		})
	}
	if result == nil {
		result = []RouteStatus{}
	}
	return result, isExit
}
