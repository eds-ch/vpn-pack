package service

import (
	"context"
	"fmt"
	"log/slog"
	"net/netip"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"tailscale.com/ipn"
	"tailscale.com/ipn/ipnstate"

	"unifi-tailscale/manager/config"
	"unifi-tailscale/manager/domain"
)

// Dependency interfaces — minimal subsets, satisfied by concrete types via duck typing.

type TailscalePrefs interface {
	GetPrefs(ctx context.Context) (*ipn.Prefs, error)
	EditPrefs(ctx context.Context, mp *ipn.MaskedPrefs) (*ipn.Prefs, error)
	Status(ctx context.Context) (*ipnstate.Status, error)
}

type SettingsFirewall interface {
	IntegrationReady() bool
	EnsureDNSForwarding(ctx context.Context, magicDNSSuffix string) error
	RemoveDNSForwarding(ctx context.Context) error
	OpenWanPort(ctx context.Context, port int, marker string) error
	CloseWanPort(ctx context.Context, port int, marker string) error
	RestoreRulesWithRetry(ctx context.Context, retries int, delay time.Duration)
}

type SettingsIntegration interface {
	HasAPIKey() bool
}

type SettingsManifest interface {
	HasDNSPolicy(marker string) bool
	WanPort(marker string) (port int, ok bool)
}

type SettingsNotifier interface {
	OnRestartRequired()
	OnDNSChanged(enabled bool)
}

// Types — exported for use in HTTP handlers and SSE state.

type SettingsFields = domain.SettingsFields

type SettingsResponse struct {
	SettingsFields
	ControlURL string `json:"controlURL"`
}

type SettingsRequest struct {
	Hostname             *string   `json:"hostname,omitempty"`
	AcceptDNS            *bool     `json:"acceptDNS,omitempty"`
	AcceptRoutes         *bool     `json:"acceptRoutes,omitempty"`
	ShieldsUp            *bool     `json:"shieldsUp,omitempty"`
	RunSSH               *bool     `json:"runSSH,omitempty"`
	ControlURL           *string   `json:"controlURL,omitempty"`
	NoSNAT               *bool     `json:"noSNAT,omitempty"`
	UDPPort              *int      `json:"udpPort,omitempty"`
	RelayServerPort      *int      `json:"relayServerPort"`
	RelayServerEndpoints *string   `json:"relayServerEndpoints,omitempty"`
	AdvertiseTags        *[]string `json:"advertiseTags,omitempty"`
}

// Error types — protocol-agnostic, no net/http.

type ErrorKind int

const (
	ErrValidation ErrorKind = iota
	ErrUpstream
	ErrInternal
	ErrPrecondition
	ErrNotFound
	ErrUnavailable
)

type Error struct {
	Kind    ErrorKind
	Message string
	Cause   error
}

func (e *Error) Error() string { return e.Message }

func (e *Error) Unwrap() error { return e.Cause }

func validationError(msg string) *Error {
	return &Error{Kind: ErrValidation, Message: msg}
}

func upstreamError(msg string, cause error) *Error {
	return &Error{Kind: ErrUpstream, Message: msg, Cause: cause}
}

func internalError(msg string) *Error {
	return &Error{Kind: ErrInternal, Message: msg}
}

// SetResult carries the response and side-effect flags for the HTTP handler.
type SetResult struct {
	Response         SettingsResponse
	DNSChanged       bool
	AcceptDNSEnabled bool
	NeedsRestart     bool
}

// SettingsService encapsulates settings business logic.
type SettingsService struct {
	ts       TailscalePrefs
	fw       SettingsFirewall
	ic       SettingsIntegration
	manifest SettingsManifest
	notify   SettingsNotifier
	hasUDAPI bool
}

func NewSettingsService(
	ts TailscalePrefs,
	fw SettingsFirewall,
	ic SettingsIntegration,
	manifest SettingsManifest,
	hasUDAPI bool,
	notify SettingsNotifier,
) *SettingsService {
	return &SettingsService{
		ts:       ts,
		fw:       fw,
		ic:       ic,
		manifest: manifest,
		hasUDAPI: hasUDAPI,
		notify:   notify,
	}
}

func (svc *SettingsService) GetSettings(ctx context.Context) (*SettingsResponse, error) {
	prefs, err := svc.ts.GetPrefs(ctx)
	if err != nil {
		return nil, upstreamError(humanizeLocalAPIError(err), err)
	}
	resp := ToSettingsResponse(prefs)
	resp.AcceptDNS = svc.manifest != nil && svc.manifest.HasDNSPolicy(config.DNSMarkerTailscale)
	return &resp, nil
}

func (svc *SettingsService) SetSettings(ctx context.Context, req *SettingsRequest) (*SetResult, error) {
	dnsForwardingTouched := req.AcceptDNS != nil

	relayEndpoints, err := svc.validate(ctx, req)
	if err != nil {
		return nil, err
	}

	if req.AcceptDNS != nil {
		if err := svc.applyDNSForwarding(ctx, *req.AcceptDNS); err != nil {
			return nil, err
		}
		req.AcceptDNS = nil
	}

	old, err := svc.fetchPreEditPrefs(ctx, req)
	if err != nil {
		return nil, err
	}
	mp := BuildMaskedPrefs(req, relayEndpoints)

	updated, err := svc.ts.EditPrefs(ctx, mp)
	if err != nil {
		return nil, upstreamError(humanizeLocalAPIError(err), err)
	}

	var needsRestart bool
	if req.ControlURL != nil && *req.ControlURL != old.controlURL {
		needsRestart = true
	}

	portRestart, err := svc.applyUDPPortChange(req.UDPPort)
	if err != nil {
		return nil, err
	}
	if portRestart {
		needsRestart = true
	}

	svc.updateRelayPortRules(ctx, req.RelayServerPort, old.relayPort)
	svc.updateTailscaleWgPortRules(ctx, req.UDPPort)

	acceptDNSEnabled := svc.manifest != nil && svc.manifest.HasDNSPolicy(config.DNSMarkerTailscale)

	resp := ToSettingsResponse(updated)
	resp.AcceptDNS = acceptDNSEnabled

	if svc.notify != nil {
		if needsRestart {
			svc.notify.OnRestartRequired()
		}
		if dnsForwardingTouched {
			svc.notify.OnDNSChanged(acceptDNSEnabled)
		}
	}

	return &SetResult{
		Response:         resp,
		DNSChanged:       dnsForwardingTouched,
		AcceptDNSEnabled: acceptDNSEnabled,
		NeedsRestart:     needsRestart,
	}, nil
}

// --- Private helpers ---

func (svc *SettingsService) validate(ctx context.Context, req *SettingsRequest) ([]netip.AddrPort, error) {
	if req.AcceptDNS != nil && *req.AcceptDNS {
		if svc.fw == nil || !svc.fw.IntegrationReady() {
			return nil, validationError("DNS forwarding requires Integration API. Configure in Settings > Integration.")
		}
	}

	if req.ControlURL != nil {
		if err := ValidateControlURL(*req.ControlURL); err != nil {
			return nil, validationError(err.Error())
		}
		prefs, err := svc.ts.GetPrefs(ctx)
		if err != nil {
			return nil, upstreamError(humanizeLocalAPIError(err), err)
		}
		if *req.ControlURL != prefs.ControlURL {
			st, err := svc.ts.Status(ctx)
			if err != nil {
				return nil, upstreamError(humanizeLocalAPIError(err), err)
			}
			if st.BackendState == "Running" {
				return nil, validationError("Must log out before changing control server URL")
			}
		}
	}

	if req.UDPPort != nil {
		port := *req.UDPPort
		if port < 1 || port > config.MaxPort {
			return nil, validationError("UDP port must be between 1 and 65535")
		}
	}

	if req.RelayServerPort != nil {
		port := *req.RelayServerPort
		if port < -1 || port > config.MaxPort {
			return nil, validationError("Relay server port must be between 0 and 65535, or -1 to disable")
		}
	}

	if req.AdvertiseTags != nil {
		for _, tag := range *req.AdvertiseTags {
			if err := ValidateTag(tag); err != nil {
				return nil, validationError(fmt.Sprintf("Invalid tag %q: %v", tag, err))
			}
		}
	}

	var relayEndpoints []netip.AddrPort
	if req.RelayServerEndpoints != nil && *req.RelayServerEndpoints != "" {
		var err error
		relayEndpoints, err = ParseAddrPorts(*req.RelayServerEndpoints)
		if err != nil {
			return nil, validationError(fmt.Sprintf("Invalid relay server endpoints: %v", err))
		}
	}

	return relayEndpoints, nil
}

func (svc *SettingsService) applyDNSForwarding(ctx context.Context, enable bool) error {
	if enable {
		st, err := svc.ts.Status(ctx)
		if err != nil {
			return upstreamError(humanizeLocalAPIError(err), err)
		}
		var suffix string
		if st.CurrentTailnet != nil {
			suffix = st.CurrentTailnet.MagicDNSSuffix
		}
		if suffix == "" {
			return validationError("Cannot determine tailnet DNS suffix. Ensure Tailscale is connected.")
		}
		if err := svc.fw.EnsureDNSForwarding(ctx, suffix); err != nil {
			return internalError("Failed to create DNS forwarding: " + err.Error())
		}
		return nil
	}
	if err := svc.fw.RemoveDNSForwarding(ctx); err != nil {
		return internalError("Failed to remove DNS forwarding: " + err.Error())
	}
	return nil
}

type preEditPrefs struct {
	controlURL string
	relayPort  *uint16
}

func (svc *SettingsService) fetchPreEditPrefs(ctx context.Context, req *SettingsRequest) (preEditPrefs, error) {
	needControlURL := req.ControlURL != nil
	needRelayPort := req.RelayServerPort != nil && svc.hasUDAPI
	if !needControlURL && !needRelayPort {
		return preEditPrefs{}, nil
	}
	prefs, err := svc.ts.GetPrefs(ctx)
	if err != nil {
		return preEditPrefs{}, upstreamError(humanizeLocalAPIError(err), err)
	}
	var p preEditPrefs
	if needControlURL {
		p.controlURL = prefs.ControlURL
	}
	if needRelayPort {
		p.relayPort = prefs.RelayServerPort
	}
	return p, nil
}

func (svc *SettingsService) applyUDPPortChange(newPort *int) (bool, error) {
	if newPort == nil {
		return false, nil
	}
	currentPort := ReadTailscaledPort()
	if *newPort == currentPort {
		return false, nil
	}
	if err := WriteTailscaledPort(*newPort); err != nil {
		slog.Warn("failed to write tailscaled port", "err", err)
		return false, internalError("Failed to update UDP port configuration")
	}
	return true, nil
}

func (svc *SettingsService) swapWanPort(ctx context.Context, oldPort, newPort int, marker string) {
	var changed bool
	if oldPort > 0 {
		if err := svc.fw.CloseWanPort(ctx, oldPort, marker); err != nil {
			slog.Warn("WAN port close failed", "port", oldPort, "marker", marker, "err", err)
		} else {
			changed = true
		}
	}
	if newPort > 0 {
		if err := svc.fw.OpenWanPort(ctx, newPort, marker); err != nil {
			slog.Warn("WAN port open failed", "port", newPort, "marker", marker, "err", err)
		} else {
			changed = true
		}
	}
	if changed {
		go svc.fw.RestoreRulesWithRetry(context.WithoutCancel(ctx), 3, 2*time.Second)
	}
}

func (svc *SettingsService) updateRelayPortRules(ctx context.Context, newRelayPort *int, oldRelayPort *uint16) {
	if newRelayPort == nil || svc.ic == nil || !svc.ic.HasAPIKey() {
		return
	}
	oldPort := 0
	if oldRelayPort != nil {
		oldPort = int(*oldRelayPort)
	}
	svc.swapWanPort(ctx, oldPort, *newRelayPort, config.WanMarkerRelay)
}

func (svc *SettingsService) updateTailscaleWgPortRules(ctx context.Context, newPort *int) {
	if newPort == nil || svc.ic == nil || !svc.ic.HasAPIKey() {
		return
	}
	currentPort, _ := svc.manifest.WanPort(config.WanMarkerTailscaleWG)
	if currentPort == *newPort {
		return
	}
	svc.swapWanPort(ctx, currentPort, *newPort, config.WanMarkerTailscaleWG)
}

// --- Exported pure functions ---

func ToSettingsResponse(prefs *ipn.Prefs) SettingsResponse {
	return SettingsResponse{
		SettingsFields: SettingsFields{
			Hostname:             prefs.Hostname,
			AcceptDNS:            prefs.CorpDNS,
			AcceptRoutes:         prefs.RouteAll,
			ShieldsUp:            prefs.ShieldsUp,
			RunSSH:               prefs.RunSSH,
			NoSNAT:               prefs.NoSNAT,
			UDPPort:              ReadTailscaledPort(),
			RelayServerPort:      prefs.RelayServerPort,
			RelayServerEndpoints: FormatAddrPorts(prefs.RelayServerStaticEndpoints),
			AdvertiseTags:        prefs.AdvertiseTags,
		},
		ControlURL: prefs.ControlURL,
	}
}

func BuildMaskedPrefs(req *SettingsRequest, relayEndpoints []netip.AddrPort) *ipn.MaskedPrefs {
	mp := &ipn.MaskedPrefs{}
	if req.Hostname != nil {
		mp.Hostname = *req.Hostname
		mp.HostnameSet = true
	}
	if req.AcceptRoutes != nil {
		mp.RouteAll = *req.AcceptRoutes
		mp.RouteAllSet = true
	}
	if req.ShieldsUp != nil {
		mp.ShieldsUp = *req.ShieldsUp
		mp.ShieldsUpSet = true
	}
	if req.RunSSH != nil {
		mp.RunSSH = *req.RunSSH
		mp.RunSSHSet = true
	}
	if req.ControlURL != nil {
		mp.ControlURL = *req.ControlURL
		mp.ControlURLSet = true
	}
	if req.NoSNAT != nil {
		mp.NoSNAT = *req.NoSNAT
		mp.NoSNATSet = true
	}
	if req.RelayServerPort != nil {
		port := *req.RelayServerPort
		if port < 0 {
			mp.RelayServerPort = nil
		} else {
			p := uint16(port)
			mp.RelayServerPort = &p
		}
		mp.RelayServerPortSet = true
	}
	if req.RelayServerEndpoints != nil {
		mp.RelayServerStaticEndpoints = relayEndpoints
		mp.RelayServerStaticEndpointsSet = true
	}
	if req.AdvertiseTags != nil {
		mp.AdvertiseTags = *req.AdvertiseTags
		mp.AdvertiseTagsSet = true
	}
	return mp
}

func ValidateControlURL(raw string) error {
	if raw == "" {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	if u.Scheme != "https" {
		return fmt.Errorf("control server URL must use HTTPS scheme")
	}
	if u.Host == "" {
		return fmt.Errorf("control server URL must have a host")
	}
	return nil
}

func ValidateTag(tag string) error {
	name, ok := strings.CutPrefix(tag, "tag:")
	if !ok {
		return fmt.Errorf("must start with 'tag:'")
	}
	if name == "" {
		return fmt.Errorf("name must not be empty")
	}
	if !isAlpha(name[0]) {
		return fmt.Errorf("name must start with a letter")
	}
	for _, b := range []byte(name) {
		if !isAlpha(b) && !isNum(b) && b != '-' {
			return fmt.Errorf("name can only contain letters, numbers, or dashes")
		}
	}
	return nil
}

func isAlpha(b byte) bool { return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') }
func isNum(b byte) bool   { return b >= '0' && b <= '9' }

var portRe = regexp.MustCompile(`(?m)^PORT="(\d+)"`)

var cachedTailscaledPort atomic.Pointer[int]

func ReadTailscaledPort() int {
	if p := cachedTailscaledPort.Load(); p != nil {
		return *p
	}
	port := readPortFromFile()
	cachedTailscaledPort.Store(&port)
	return port
}

func readPortFromFile() int {
	data, err := os.ReadFile(config.TailscaledDefaultsPath)
	if err != nil {
		return config.DefaultTailscalePort
	}
	m := portRe.FindSubmatch(data)
	if m == nil {
		return config.DefaultTailscalePort
	}
	port, err := strconv.Atoi(string(m[1]))
	if err != nil || port < 1 || port > config.MaxPort {
		return config.DefaultTailscalePort
	}
	return port
}

func WriteTailscaledPort(port int) error {
	data, err := os.ReadFile(config.TailscaledDefaultsPath)
	if err != nil {
		data = []byte(fmt.Sprintf("PORT=\"%d\"\nFLAGS=\"\"\n", port))
		if err := os.WriteFile(config.TailscaledDefaultsPath, data, config.ConfigPerm); err != nil {
			return err
		}
		cachedTailscaledPort.Store(&port)
		return nil
	}
	content := string(data)
	newLine := fmt.Sprintf("PORT=\"%d\"", port)
	if portRe.MatchString(content) {
		content = portRe.ReplaceAllString(content, newLine)
	} else {
		content = newLine + "\n" + strings.TrimRight(content, "\n") + "\n"
	}
	if err := os.WriteFile(config.TailscaledDefaultsPath, []byte(content), config.ConfigPerm); err != nil {
		return err
	}
	cachedTailscaledPort.Store(&port)
	return nil
}

func FormatAddrPorts(addrs []netip.AddrPort) string {
	if len(addrs) == 0 {
		return ""
	}
	parts := make([]string, len(addrs))
	for i, ap := range addrs {
		parts[i] = ap.String()
	}
	return strings.Join(parts, ", ")
}

func ParseAddrPorts(s string) ([]netip.AddrPort, error) {
	if s == "" {
		return nil, nil
	}
	parts := strings.Split(s, ",")
	result := make([]netip.AddrPort, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		ap, err := netip.ParseAddrPort(p)
		if err != nil {
			return nil, fmt.Errorf("invalid endpoint %q: %w", p, err)
		}
		result = append(result, ap)
	}
	return result, nil
}

// humanizeLocalAPIError converts raw Tailscale local API errors to user-friendly messages.
func humanizeLocalAPIError(err error) string {
	if err == nil {
		return ""
	}

	msg := err.Error()
	lower := strings.ToLower(msg)

	switch {
	case strings.Contains(lower, "not logged in"):
		return "Tailscale is not connected. Use the Status tab to log in first."
	case strings.Contains(lower, "not running"):
		return "Tailscale service is stopped. Start it from the Status tab."
	case strings.Contains(lower, "backend state: needslogin"):
		return "Authentication required. Complete login in the Status tab."
	case strings.Contains(lower, "already running"):
		return "Tailscale is already connected."
	case strings.Contains(lower, "timeout") || strings.Contains(lower, "deadline exceeded"):
		return "Tailscale service is not responding. Check if tailscaled is running: systemctl status tailscaled"
	case strings.Contains(lower, "connection refused"):
		return "Cannot reach tailscaled. Verify the service is running: systemctl status tailscaled"
	case strings.Contains(lower, "auth key") && strings.Contains(lower, "expired"):
		return "The auth key has expired. Generate a new one in the Tailscale admin console."
	case strings.Contains(lower, "auth key") && strings.Contains(lower, "invalid"):
		return "The auth key is not recognized. Verify it was copied correctly from the Tailscale admin console."
	case strings.Contains(lower, "node already registered"):
		return "This device is already registered to a tailnet. Log out first if you want to re-register."
	default:
		return fmt.Sprintf("Tailscale error: %s. If this persists, check logs: journalctl -u tailscaled -n 50", msg)
	}
}
