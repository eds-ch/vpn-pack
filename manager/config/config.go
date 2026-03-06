package config

import "time"

const (
	PersistentBase         = "/persistent/vpn-pack"
	ManifestPath           = PersistentBase + "/config/manifest.json"
	APIKeyPath             = PersistentBase + "/config/api-key"
	NginxConfigSrc         = PersistentBase + "/config/nginx-vpnpack.conf"
	WgS2sConfigDir         = PersistentBase + "/config/wg-s2s"
	TailscaledDefaultsPath = PersistentBase + "/tailscaled.defaults"
	VersionFilePath        = PersistentBase + "/VERSION"
)

const (
	UDAPISocketPath = "/run/ubnt-udapi-server.sock"
	UDAPIConfigPath = "/data/udapi-config/udapi-net-cfg.json"
	NginxConfigDir  = "/data/unifi-core/config/http"
	NginxConfigDest = NginxConfigDir + "/shared-runnable-vpnpack.conf"
	NginxConfigFile = "shared-runnable-vpnpack.conf"
)

const (
	ReadHeaderTimeout   = 10 * time.Second
	ReadTimeout         = 30 * time.Second
	IdleTimeout         = 120 * time.Second
	MaxHeaderBytes      = 1 << 20
	MaxRequestBodyBytes = 64 << 10 // 64 KB
	ShutdownTimeout     = 5 * time.Second
)

const (
	BackoffInitial = 500 * time.Millisecond
	BackoffMax     = 30 * time.Second
)

const (
	DebounceDuration = 500 * time.Millisecond
	PollInterval     = 5 * time.Second
	ReconnectDelay   = 2 * time.Second
	StatusRefresh    = 5 * time.Second
	MaxSSEClients    = 64
	SSEChannelBuffer = 16
	LogBufferSize    = 1000
)

const (
	UpdateCheckPeriod  = 24 * time.Hour
	UpdateInitialDelay = 30 * time.Second
	GithubAPITimeout   = 15 * time.Second
)

const (
	IntegrationBaseURL     = "https://127.0.0.1/proxy/network/integration"
	IntegrationHTTPTimeout = 10 * time.Second
	PaginationLimit        = 200
)

const IntegrationCacheTTL = 30 * time.Second

const LogReconnectDelay = 2 * time.Second

const TailscaleInterface = "tailscale0"

const MongoPort = "27117"

const (
	TailscaleCGNAT       = "100.64.0.0/10"
	FirewallMarker       = "vpn-pack-manager"
	DefaultChainPrefix   = "VPN"
	DefaultTailscalePort = 41641
)

const (
	ChainForwardInUser  = "UBIOS_FORWARD_IN_USER"
	ChainInputUserHook  = "UBIOS_INPUT_USER_HOOK"
	ChainOutputUserHook = "UBIOS_OUTPUT_USER_HOOK"
)

const (
	WanMarkerTailscaleWG = "tailscale-wg"
	WanMarkerRelay       = "relay-server"
	WanMarkerWgS2sPrefix = "wg-s2s:"
)

const (
	DNSMarkerTailscale     = "tailscale-dns"
	TailscaleDNSResolverIP = "100.100.100.100"
)

const (
	DeviceInfoCmd   = "ubnt-device-info"
	VPNClientPrefix = "wgclt"
)

const MaxPort = 65535

const (
	WGKeyBase64Len = 44
	WGKeyBytes     = 32
)

const (
	DirPerm    = 0755
	SecretPerm = 0600
	ConfigPerm = 0644
)

// Set via -ldflags at build time.
var (
	Version          = "dev"
	TailscaleVersion = "unknown"
	GitCommit        = "unknown"
	BuildDate        = "unknown"
	GithubRepo       = "eds-ch/vpn-pack"
)
