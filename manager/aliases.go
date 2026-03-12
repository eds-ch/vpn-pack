package main

import (
	"unifi-tailscale/manager/client"
	"unifi-tailscale/manager/domain"
	"unifi-tailscale/manager/service"
	"unifi-tailscale/manager/state"
	"unifi-tailscale/manager/udapi"
)

// Type aliases bridge sub-packages back to the root package.
// Lowercase aliases (sseMessage, stateData) were unexported pre-migration.

// Interfaces
type SSEHub = domain.SSEHub
type ManifestStore = domain.ManifestStore
type IntegrationAPI = domain.IntegrationAPI
type IPNWatcher = domain.IPNWatcher
type TailscaleControl = domain.TailscaleControl
type FirewallService = domain.FirewallService
type WgS2sControl = domain.WgS2sControl

// manifest.go types
type ZoneManifest = domain.ZoneManifest
type WanPortEntry = domain.WanPortEntry
type DNSPolicyEntry = domain.DNSPolicyEntry
type WgS2sZoneInfo = domain.WgS2sZoneInfo

// integration_api.go types
type Zone = domain.Zone
type Policy = domain.Policy
type AppInfo = domain.AppInfo
type DNSPolicy = domain.DNSPolicy
type PolicyAction = domain.PolicyAction
type PolicyEndpoint = domain.PolicyEndpoint
type TrafficFilter = domain.TrafficFilter
type PortFilter = domain.PortFilter
type PortFilterItem = domain.PortFilterItem
type IPProtocolScope = domain.IPProtocolScope
type ProtocolFilter = domain.ProtocolFilter
type ProtocolName = domain.ProtocolName
type SiteInfo = domain.SiteInfo

// sse.go
type sseMessage = domain.SSEMessage

// health.go
type WatcherStatus = domain.WatcherStatus
type WatcherHealth = domain.WatcherHealth
type HealthSnapshot = domain.HealthSnapshot

// watcher.go
type stateData = domain.StateData
type TailscaleState = domain.TailscaleState
type SelfNode = domain.SelfNode
type PeerInfo = domain.PeerInfo
type DERPInfo = domain.DERPInfo
type FirewallHealth = domain.FirewallHealth
type RoutingHealth = domain.RoutingHealth
type RemoteExitNodeStatus = domain.RemoteExitNodeStatus

type OperationResponse = domain.OperationResponse

// detect.go
type DeviceInfo = domain.DeviceInfo

// updater.go
type UpdateInfo = domain.UpdateInfo

// state/ types
type Manifest = state.Manifest
type LogBuffer = state.LogBuffer
type logEntry = state.LogEntry

var (
	LoadManifest = state.LoadManifest
	NewLogBuffer = state.NewLogBuffer
	newLogEntry  = state.NewLogEntry
)

// Constants
const (
	StatusHealthy   = domain.StatusHealthy
	StatusDegraded  = domain.StatusDegraded
	StatusUnhealthy = domain.StatusUnhealthy
)

// Error variables
var (
	ErrUnauthorized   = domain.ErrUnauthorized
	ErrNotFound       = domain.ErrNotFound
	ErrIntegrationAPI = domain.ErrIntegrationAPI
)

// client/ types
type IntegrationClient = client.IntegrationClient

var (
	NewIntegrationClient = client.NewIntegrationClient
	NewTailscaleControl  = client.NewTailscaleControl
	connectWithBackoff   = client.ConnectWithBackoff
	wanPortPolicyName    = client.WanPortPolicyName
)

// udapi/ functions
var (
	getWanIP          = udapi.GetWanIP
	parseLocalSubnets = udapi.ParseLocalSubnets
)

// service/ types
type InterfaceSubnet = service.InterfaceSubnet
type RouteSubnet = service.RouteSubnet
type SystemSubnets = service.SystemSubnets
type SubnetConflict = domain.SubnetConflict
type ValidationResult = service.ValidationResult

var (
	CollectSystemSubnets = service.CollectSystemSubnets
	ValidateAllowedIPs   = service.ValidateAllowedIPs
)
