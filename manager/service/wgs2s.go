package service

import (
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"sync"

	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"

	"unifi-tailscale/manager/config"
	"unifi-tailscale/manager/domain"
	"unifi-tailscale/manager/internal/wgs2s"
)

const wgMaxPort = 65535

// --- Interfaces ---

type WgS2sWireGuard interface {
	CreateTunnel(cfg wgs2s.TunnelConfig, privateKey string) (*wgs2s.TunnelConfig, error)
	DeleteTunnel(id string) error
	EnableTunnel(id string) error
	DisableTunnel(id string) error
	UpdateTunnel(id string, updates wgs2s.TunnelConfig) (*wgs2s.TunnelConfig, error)
	GetTunnels() []wgs2s.TunnelConfig
	GetStatuses() []wgs2s.WgS2sStatus
	GetPublicKey(id string) (string, error)
}

type WgS2sFirewall interface {
	SetupZone(ctx context.Context, tunnelID, zoneID, zoneName string) *ZoneSetupResult
	SetupFirewall(ctx context.Context, tunnelID, iface string, allowedIPs []string) error
	RemoveFirewall(ctx context.Context, tunnelID, iface string, allowedIPs []string)
	RemoveIPSetEntries(ctx context.Context, tunnelID string, cidrs []string)
	TeardownZone(ctx context.Context, tunnelID string)
	OpenWanPort(ctx context.Context, port int, iface string)
	CloseWanPort(ctx context.Context, port int, iface string)
	CheckRulesPresent(ctx context.Context, ifaces []string) map[string]bool
	IntegrationReady() bool
}

type WgS2sManifest interface {
	GetZone(tunnelID string) (ZoneInfo, bool)
	GetZones() []WgS2sZoneEntry
}

type WgS2sLogger interface {
	LogWarn(msg string)
}

// --- Provider function types ---

type SubnetValidator func(allowedIPs []string, excludeIfaces ...string) (warnings, blocks []SubnetConflict)
type WanIPProvider func() string
type LocalSubnetsProvider func() []SubnetEntry

// --- Types ---

type ZoneInfo struct {
	ZoneID   string
	ZoneName string
}

type ZoneSetupResult struct {
	ZoneCreated   bool
	PoliciesReady bool
	UDAPIApplied  bool
	Errors        []string
}

func (r *ZoneSetupResult) hasErrors() bool {
	return r != nil && len(r.Errors) > 0
}

type WgS2sZoneEntry struct {
	ZoneID      string `json:"zoneId"`
	ZoneName    string `json:"zoneName"`
	TunnelCount int    `json:"tunnelCount"`
}

type SubnetConflict = domain.SubnetConflict

type SubnetConflictError struct {
	Msg       string
	Conflicts []SubnetConflict
}

func (e *SubnetConflictError) Error() string { return e.Msg }

type FirewallStatus struct {
	ZoneCreated   bool     `json:"zoneCreated"`
	PoliciesReady bool     `json:"policiesReady"`
	UDAPIApplied  bool     `json:"udapiApplied"`
	Errors        []string `json:"errors,omitempty"`
}

type TunnelInfo struct {
	wgs2s.TunnelConfig
	PublicKey string             `json:"publicKey,omitempty"`
	Status    *wgs2s.WgS2sStatus `json:"status,omitempty"`
	ZoneID    string             `json:"zoneId,omitempty"`
	ZoneName  string             `json:"zoneName,omitempty"`
	Warnings  []SubnetConflict   `json:"warnings,omitempty"`
}

type TunnelCreateResponse struct {
	TunnelInfo
	SetupStatus string          `json:"setupStatus,omitempty"`
	Firewall    *FirewallStatus `json:"firewall,omitempty"`
}

type TunnelUpdateResponse struct {
	TunnelInfo
	SetupStatus string          `json:"setupStatus,omitempty"`
	Firewall    *FirewallStatus `json:"firewall,omitempty"`
}

type EnableTunnelResponse struct {
	OK          bool            `json:"ok"`
	SetupStatus string          `json:"setupStatus,omitempty"`
	Firewall    *FirewallStatus `json:"firewall,omitempty"`
}

type WgS2sCreateRequest struct {
	wgs2s.TunnelConfig
	PrivateKey string `json:"privateKey,omitempty"`
	ZoneID     string `json:"zoneId,omitempty"`
	ZoneName   string `json:"zoneName,omitempty"`
	CreateZone bool   `json:"createZone,omitempty"`
}

type Keypair struct {
	PublicKey  string `json:"publicKey"`
	PrivateKey string `json:"privateKey"`
}

// --- Service ---

type WgS2sService struct {
	wgMu            sync.RWMutex
	wg              WgS2sWireGuard
	fw              WgS2sFirewall
	manifest        WgS2sManifest
	logger          WgS2sLogger
	validateSubnets SubnetValidator
	wanIP           WanIPProvider
	localSubnets    LocalSubnetsProvider
}

type WgS2sConfig struct {
	WG              WgS2sWireGuard
	Firewall        WgS2sFirewall
	Manifest        WgS2sManifest
	Logger          WgS2sLogger
	ValidateSubnets SubnetValidator
	WanIP           WanIPProvider
	LocalSubnets    LocalSubnetsProvider
}

func NewWgS2sService(cfg WgS2sConfig) *WgS2sService {
	return &WgS2sService{
		wg:              cfg.WG,
		fw:              cfg.Firewall,
		manifest:        cfg.Manifest,
		logger:          cfg.Logger,
		validateSubnets: cfg.ValidateSubnets,
		wanIP:           cfg.WanIP,
		localSubnets:    cfg.LocalSubnets,
	}
}

func (svc *WgS2sService) SetWireGuard(wg WgS2sWireGuard) {
	svc.wgMu.Lock()
	svc.wg = wg
	svc.wgMu.Unlock()
}

func (svc *WgS2sService) loadWG() WgS2sWireGuard {
	svc.wgMu.RLock()
	defer svc.wgMu.RUnlock()
	return svc.wg
}

func (svc *WgS2sService) Available() bool {
	return svc.loadWG() != nil
}

func (svc *WgS2sService) ListTunnels(ctx context.Context) []TunnelInfo {
	wg := svc.loadWG()
	tunnels := wg.GetTunnels()
	statuses := wg.GetStatuses()
	svc.EnrichForwardINOk(ctx, statuses)

	statusMap := make(map[string]*wgs2s.WgS2sStatus, len(statuses))
	for i := range statuses {
		statusMap[statuses[i].ID] = &statuses[i]
	}

	result := make([]TunnelInfo, 0, len(tunnels))
	for _, t := range tunnels {
		info := TunnelInfo{TunnelConfig: t}
		if st, ok := statusMap[t.ID]; ok {
			info.Status = st
		}
		svc.enrichTunnelInfo(&info, t.ID)
		result = append(result, info)
	}
	return result
}

func (svc *WgS2sService) CreateTunnel(ctx context.Context, req *WgS2sCreateRequest) (*TunnelCreateResponse, error) {
	if err := validateCreateRequest(req); err != nil {
		return nil, validationError(err.Error())
	}

	var warnings []SubnetConflict
	if svc.validateSubnets != nil {
		var blocks []SubnetConflict
		warnings, blocks = svc.validateSubnets(req.AllowedIPs)
		if len(blocks) > 0 {
			return nil, &SubnetConflictError{
				Msg:       fmt.Sprintf("Subnet conflict: %s", blocks[0].Message),
				Conflicts: blocks,
			}
		}
	}

	wg := svc.loadWG()
	tunnel, err := wg.CreateTunnel(req.TunnelConfig, req.PrivateKey)
	if err != nil {
		return nil, upstreamError(humanizeWgS2sError(err), err)
	}

	zoneResult := svc.setupTunnelZone(ctx, tunnel.ID, req.CreateZone, req.ZoneID, req.ZoneName)

	var fwErr error
	if svc.fw != nil {
		fwErr = svc.fw.SetupFirewall(ctx, tunnel.ID, tunnel.InterfaceName, tunnel.AllowedIPs)
		if fwErr != nil {
			svc.logFirewallError(tunnel.InterfaceName, fwErr)
		}
	}
	if svc.fw != nil {
		svc.fw.OpenWanPort(ctx, tunnel.ListenPort, tunnel.InterfaceName)
	}

	info := TunnelInfo{TunnelConfig: *tunnel, Warnings: warnings}
	svc.enrichTunnelInfo(&info, tunnel.ID)

	resp := &TunnelCreateResponse{TunnelInfo: info}
	resp.SetupStatus = firewallResultStatus(zoneResult, fwErr)
	if resp.SetupStatus == "partial" {
		resp.Firewall = buildFirewallStatus(zoneResult, fwErr)
	}
	return resp, nil
}

func (svc *WgS2sService) UpdateTunnel(ctx context.Context, id string, updates wgs2s.TunnelConfig) (*TunnelUpdateResponse, error) {
	if err := validateUpdateRequest(&updates); err != nil {
		return nil, validationError(err.Error())
	}

	existing := svc.findTunnelByID(id)

	var warnings []SubnetConflict
	if updates.AllowedIPs != nil && existing != nil && svc.validateSubnets != nil {
		var blocks []SubnetConflict
		warnings, blocks = svc.validateSubnets(updates.AllowedIPs, existing.InterfaceName)
		if len(blocks) > 0 {
			return nil, &SubnetConflictError{
				Msg:       fmt.Sprintf("Subnet conflict: %s", blocks[0].Message),
				Conflicts: blocks,
			}
		}
	}

	wg := svc.loadWG()
	tunnel, err := wg.UpdateTunnel(id, updates)
	if err != nil {
		return nil, upstreamError(humanizeWgS2sError(err), err)
	}

	var fwErr error
	if updates.AllowedIPs != nil && tunnel.Enabled && existing != nil && svc.fw != nil {
		svc.fw.RemoveIPSetEntries(ctx, id, existing.AllowedIPs)
		fwErr = svc.fw.SetupFirewall(ctx, tunnel.ID, tunnel.InterfaceName, tunnel.AllowedIPs)
		if fwErr != nil {
			svc.logFirewallError(tunnel.InterfaceName, fwErr)
		}
	}

	info := TunnelInfo{TunnelConfig: *tunnel, Warnings: warnings}
	svc.enrichTunnelInfo(&info, tunnel.ID)

	resp := &TunnelUpdateResponse{TunnelInfo: info}
	resp.SetupStatus = firewallResultStatus(nil, fwErr)
	if resp.SetupStatus == "partial" {
		resp.Firewall = &FirewallStatus{Errors: []string{fwErr.Error()}}
	}
	return resp, nil
}

func (svc *WgS2sService) DeleteTunnel(ctx context.Context, id string) error {
	t := svc.findTunnelByID(id)
	if t == nil {
		return notFoundError("tunnel not found")
	}

	if err := svc.loadWG().DeleteTunnel(id); err != nil {
		return upstreamError(humanizeWgS2sError(err), err)
	}

	svc.teardownTunnelFirewall(ctx, t)
	if svc.fw != nil {
		svc.fw.TeardownZone(ctx, id)
	}
	return nil
}

func (svc *WgS2sService) EnableTunnel(ctx context.Context, id string) (*EnableTunnelResponse, error) {
	if svc.findTunnelByID(id) == nil {
		return nil, notFoundError("tunnel not found")
	}
	if err := svc.loadWG().EnableTunnel(id); err != nil {
		return nil, upstreamError(humanizeWgS2sError(err), err)
	}

	resp := &EnableTunnelResponse{OK: true}
	if t := svc.findTunnelByID(id); t != nil && svc.fw != nil {
		fwErr := svc.fw.SetupFirewall(ctx, t.ID, t.InterfaceName, t.AllowedIPs)
		if fwErr != nil {
			svc.logFirewallError(t.InterfaceName, fwErr)
		}
		svc.fw.OpenWanPort(ctx, t.ListenPort, t.InterfaceName)
		if fwErr != nil {
			resp.SetupStatus = "partial"
			resp.Firewall = &FirewallStatus{Errors: []string{fwErr.Error()}}
		}
	}
	return resp, nil
}

func (svc *WgS2sService) DisableTunnel(ctx context.Context, id string) error {
	t := svc.findTunnelByID(id)
	if t == nil {
		return notFoundError("tunnel not found")
	}

	if err := svc.loadWG().DisableTunnel(id); err != nil {
		return upstreamError(humanizeWgS2sError(err), err)
	}

	svc.teardownTunnelFirewall(ctx, t)
	return nil
}

func (svc *WgS2sService) GenerateKeypair() (*Keypair, error) {
	privKey, err := wgtypes.GeneratePrivateKey()
	if err != nil {
		return nil, internalError("failed to generate keypair")
	}
	return &Keypair{
		PublicKey:  privKey.PublicKey().String(),
		PrivateKey: privKey.String(),
	}, nil
}

func (svc *WgS2sService) GetConfig(_ context.Context, id string) (string, error) {
	tunnel := svc.findTunnelByID(id)
	if tunnel == nil {
		return "", notFoundError("tunnel not found")
	}

	pubKey, err := svc.loadWG().GetPublicKey(id)
	if err != nil {
		return "", internalError("failed to read public key")
	}

	wanIP := ""
	if svc.wanIP != nil {
		wanIP = svc.wanIP()
	}

	var allowedIPs []string
	if len(tunnel.LocalSubnets) > 0 {
		allowedIPs = append(allowedIPs, tunnel.LocalSubnets...)
	} else if svc.localSubnets != nil {
		for _, sub := range svc.localSubnets() {
			allowedIPs = append(allowedIPs, sub.CIDR)
		}
	}
	if tunnel.TunnelAddress != "" {
		ip, _, err := net.ParseCIDR(tunnel.TunnelAddress)
		if err == nil {
			allowedIPs = append(allowedIPs, ip.String()+"/32")
		}
	}

	var b strings.Builder
	b.WriteString("[Interface]\n")
	b.WriteString("PrivateKey = <PRIVATE_KEY>\n")
	b.WriteString("Address = <TUNNEL_ADDRESS>\n")
	fmt.Fprintf(&b, "ListenPort = %d\n", tunnel.ListenPort)
	b.WriteString("\n[Peer]\n")
	fmt.Fprintf(&b, "PublicKey = %s\n", pubKey)
	if wanIP != "" {
		fmt.Fprintf(&b, "Endpoint = %s:%d\n", wanIP, tunnel.ListenPort)
	}
	if len(allowedIPs) > 0 {
		fmt.Fprintf(&b, "AllowedIPs = %s\n", strings.Join(allowedIPs, ", "))
	}
	if tunnel.PersistentKeepalive > 0 {
		fmt.Fprintf(&b, "PersistentKeepalive = %d\n", tunnel.PersistentKeepalive)
	}

	return b.String(), nil
}

func (svc *WgS2sService) GetWanIP() string {
	if svc.wanIP != nil {
		return svc.wanIP()
	}
	return ""
}

func (svc *WgS2sService) GetLocalSubnets() []SubnetEntry {
	if svc.localSubnets != nil {
		return svc.localSubnets()
	}
	return []SubnetEntry{}
}

func (svc *WgS2sService) ListZones() []WgS2sZoneEntry {
	zones := svc.manifest.GetZones()
	if zones == nil {
		return []WgS2sZoneEntry{}
	}
	return zones
}

// --- Private helpers ---

func (svc *WgS2sService) logFirewallError(iface string, err error) {
	slog.Warn("wg-s2s firewall rules failed", "iface", iface, "err", err)
	if svc.logger != nil {
		svc.logger.LogWarn(fmt.Sprintf("firewall rules failed iface=%s err=%v", iface, err))
	}
}

func (svc *WgS2sService) findTunnelByID(id string) *wgs2s.TunnelConfig {
	wg := svc.loadWG()
	if wg == nil {
		return nil
	}
	tunnels := wg.GetTunnels()
	for i := range tunnels {
		if tunnels[i].ID == id {
			return &tunnels[i]
		}
	}
	return nil
}

func (svc *WgS2sService) EnrichForwardINOk(ctx context.Context, statuses []wgs2s.WgS2sStatus) {
	if svc.fw == nil {
		return
	}
	var ifaces []string
	for _, st := range statuses {
		if st.Enabled {
			ifaces = append(ifaces, st.InterfaceName)
		}
	}
	fwPresent := svc.fw.CheckRulesPresent(ctx, ifaces)
	for i := range statuses {
		statuses[i].ForwardINOk = fwPresent[statuses[i].InterfaceName]
	}
}

func (svc *WgS2sService) teardownTunnelFirewall(ctx context.Context, t *wgs2s.TunnelConfig) {
	if svc.fw == nil || t == nil {
		return
	}
	svc.fw.RemoveFirewall(ctx, t.ID, t.InterfaceName, t.AllowedIPs)
	svc.fw.CloseWanPort(ctx, t.ListenPort, t.InterfaceName)
}

func (svc *WgS2sService) setupTunnelZone(ctx context.Context, tunnelID string, createZone bool, zoneID, zoneName string) *ZoneSetupResult {
	if svc.fw == nil || !svc.fw.IntegrationReady() {
		return nil
	}
	var result *ZoneSetupResult
	switch {
	case createZone:
		result = svc.fw.SetupZone(ctx, tunnelID, "", zoneName)
	case zoneID != "":
		result = svc.fw.SetupZone(ctx, tunnelID, zoneID, "")
	case len(svc.manifest.GetZones()) == 0:
		result = svc.fw.SetupZone(ctx, tunnelID, "", "WireGuard S2S")
	}
	if result != nil && result.hasErrors() {
		slog.Warn("wg-s2s zone setup failed", "errors", result.Errors)
	}
	return result
}

func firewallResultStatus(zoneResult *ZoneSetupResult, fwErr error) string {
	if !zoneResult.hasErrors() && fwErr == nil {
		return "ok"
	}
	return "partial"
}

func buildFirewallStatus(zoneResult *ZoneSetupResult, fwErr error) *FirewallStatus {
	fs := &FirewallStatus{}
	if zoneResult != nil {
		fs.ZoneCreated = zoneResult.ZoneCreated
		fs.PoliciesReady = zoneResult.PoliciesReady
		fs.UDAPIApplied = zoneResult.UDAPIApplied
		fs.Errors = append(fs.Errors, zoneResult.Errors...)
	}
	if fwErr != nil {
		fs.Errors = append(fs.Errors, fwErr.Error())
	}
	return fs
}

func (svc *WgS2sService) enrichTunnelInfo(info *TunnelInfo, tunnelID string) {
	wg := svc.loadWG()
	if pubKey, err := wg.GetPublicKey(tunnelID); err == nil {
		info.PublicKey = pubKey
	}
	if zm, ok := svc.manifest.GetZone(tunnelID); ok {
		info.ZoneID = zm.ZoneID
		info.ZoneName = zm.ZoneName
	}
}

// --- Validation ---

func validateCreateRequest(req *WgS2sCreateRequest) error {
	cfg := req.TunnelConfig
	if cfg.Name == "" {
		return fmt.Errorf("name is required")
	}
	if cfg.ListenPort < 1 || cfg.ListenPort > wgMaxPort {
		return fmt.Errorf("listenPort must be between 1 and 65535")
	}
	if err := validateCIDR(cfg.TunnelAddress); err != nil {
		return fmt.Errorf("invalid tunnelAddress: %s", err)
	}
	if err := validateBase64Key(cfg.PeerPublicKey); err != nil {
		return fmt.Errorf("invalid peerPublicKey: %s", err)
	}
	if err := validateCIDRList(cfg.AllowedIPs, "allowedIP"); err != nil {
		return err
	}
	if err := validateCIDRList(cfg.LocalSubnets, "localSubnet"); err != nil {
		return err
	}
	return validateRouteMetric(cfg.RouteMetric)
}

func validateUpdateRequest(updates *wgs2s.TunnelConfig) error {
	if updates.ListenPort < 0 || updates.ListenPort > wgMaxPort {
		return fmt.Errorf("listenPort must be between 0 and 65535")
	}
	if updates.TunnelAddress != "" {
		if err := validateCIDR(updates.TunnelAddress); err != nil {
			return fmt.Errorf("invalid tunnelAddress: %s", err)
		}
	}
	if updates.PeerPublicKey != "" {
		if err := validateBase64Key(updates.PeerPublicKey); err != nil {
			return fmt.Errorf("invalid peerPublicKey: %s", err)
		}
	}
	if err := validateCIDRList(updates.AllowedIPs, "allowedIP"); err != nil {
		return err
	}
	if err := validateCIDRList(updates.LocalSubnets, "localSubnet"); err != nil {
		return err
	}
	return validateRouteMetric(updates.RouteMetric)
}

func validateCIDRList(cidrs []string, fieldName string) error {
	for _, cidr := range cidrs {
		if err := validateCIDR(cidr); err != nil {
			return fmt.Errorf("invalid %s %q: %s", fieldName, cidr, err)
		}
	}
	return nil
}

const maxRouteMetric = 9999

func validateRouteMetric(metric int) error {
	if metric < 0 || metric > maxRouteMetric {
		return fmt.Errorf("routeMetric must be between 0 and %d (0 = default 100)", maxRouteMetric)
	}
	return nil
}

func validateCIDR(s string) error {
	_, _, err := net.ParseCIDR(s)
	if err != nil {
		return fmt.Errorf("not a valid CIDR notation")
	}
	return nil
}

func validateBase64Key(s string) error {
	if len(s) != config.WGKeyBase64Len {
		return fmt.Errorf("must be 44 characters (base64-encoded 32 bytes)")
	}
	decoded, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return fmt.Errorf("invalid base64 encoding")
	}
	if len(decoded) != config.WGKeyBytes {
		return fmt.Errorf("decoded key must be 32 bytes")
	}
	return nil
}

func humanizeWgS2sError(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	lower := strings.ToLower(msg)

	switch {
	case strings.Contains(lower, "tunnel") && strings.Contains(lower, "not found"):
		return "Tunnel not found"
	case strings.Contains(lower, "port in use") || strings.Contains(lower, "address already in use"):
		return "Port is already in use. Choose a different port."
	case strings.Contains(lower, "permission denied") || strings.Contains(lower, "operation not permitted"):
		return "Insufficient permissions for network operations. Ensure the manager runs as root."
	default:
		return fmt.Sprintf("WG S2S error: %s", msg)
	}
}
