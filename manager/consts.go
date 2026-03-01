package main

import "time"

const (
	persistentBase         = "/persistent/vpn-pack"
	manifestPath           = persistentBase + "/config/manifest.json"
	apiKeyPath             = persistentBase + "/config/api-key"
	nginxConfigSrc         = persistentBase + "/config/nginx-vpnpack.conf"
	wgS2sConfigDir         = persistentBase + "/config/wg-s2s"
	tailscaledDefaultsPath = persistentBase + "/tailscaled.defaults"
	versionFilePath        = persistentBase + "/VERSION"
)

const (
	udapiSocketPath = "/run/ubnt-udapi-server.sock"
	udapiConfigPath = "/data/udapi-config/udapi-net-cfg.json"
	nginxConfigDir  = "/data/unifi-core/config/http"
	nginxConfigDest = nginxConfigDir + "/shared-runnable-vpnpack.conf"
	nginxConfigFile = "shared-runnable-vpnpack.conf"
)

const (
	readHeaderTimeout   = 10 * time.Second
	readTimeout         = 30 * time.Second
	idleTimeout         = 120 * time.Second
	maxHeaderBytes      = 1 << 20
	maxRequestBodyBytes = 64 << 10 // 64 KB
	shutdownTimeout     = 5 * time.Second
)

const (
	backoffInitial = 500 * time.Millisecond
	backoffMax     = 30 * time.Second
)

const (
	debounceDuration = 500 * time.Millisecond
	pollInterval     = 5 * time.Second
	reconnectDelay   = 2 * time.Second
	statusRefresh    = 5 * time.Second
	maxSSEClients    = 64
	sseChannelBuffer = 16
	firewallChBuffer = 8
	logBufferSize    = 1000
)

const (
	updateCheckPeriod  = 24 * time.Hour
	updateInitialDelay = 30 * time.Second
	githubAPITimeout   = 15 * time.Second
)

const (
	integrationBaseURL     = "https://127.0.0.1/proxy/network/integration"
	integrationHTTPTimeout = 10 * time.Second
	paginationLimit        = 200
)

const (
	netcheckCacheTTL = 60 * time.Second
	netcheckTimeout  = 10 * time.Second
)

const logReconnectDelay = 2 * time.Second

const tailscaleInterface = "tailscale0"

const (
	tailscaleCGNAT       = "100.64.0.0/10"
	firewallMarker       = "vpn-pack-manager"
	defaultChainPrefix   = "VPN"
	defaultTailscalePort = 41641
)

const (
	chainForwardInUser  = "UBIOS_FORWARD_IN_USER"
	chainInputUserHook  = "UBIOS_INPUT_USER_HOOK"
	chainOutputUserHook = "UBIOS_OUTPUT_USER_HOOK"
)

const (
	wanMarkerTailscaleWG = "tailscale-wg"
	wanMarkerRelay       = "relay-server"
	wanMarkerWgS2sPrefix = "wg-s2s:"
)

const (
	deviceInfoCmd   = "ubnt-device-info"
	vpnClientPrefix = "wgclt"
)

type FirewallAction string

const (
	FirewallActionCheckAndRestore FirewallAction = "check-and-restore"
	FirewallActionApplyWgS2s      FirewallAction = "apply-wg-s2s"
)

const (
	wgKeyBase64Len = 44
	wgKeyBytes     = 32
)

const (
	dirPerm    = 0755
	secretPerm = 0600
	configPerm = 0644
)
