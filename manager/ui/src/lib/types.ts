export interface SelfNode {
    hostName: string;
    dnsName: string;
    online: boolean;
    txBytes: number;
    rxBytes: number;
}

export interface PeerInfo {
    hostName: string;
    dnsName: string;
    tailscaleIP: string;
    os: string;
    online: boolean;
    lastSeen: string;
    curAddr: string;
    relay: string;
    peerRelay: string;
    rxBytes: number;
    txBytes: number;
    active: boolean;
}

export interface DERPInfo {
    regionID: number;
    regionCode: string;
    regionName: string;
    latencyMs: number;
    preferred: boolean;
}

export interface RouteStatus {
    cidr: string;
    approved: boolean;
}

export interface ExitNodeClient {
    ip: string;
    label?: string;
}

export interface SubnetEntry {
    cidr: string;
    name: string;
    type: string;
}

export interface FirewallHealth {
    zoneActive: boolean;
    watcherRunning: boolean;
    udapiReachable: boolean;
    chainPrefix: string;
    zoneName?: string;
}

export interface IntegrationStatus {
    configured: boolean;
    valid: boolean;
    siteId?: string;
    appVersion?: string;
    error?: string;
    reason?: string;
    zbfEnabled?: boolean;
}

export interface WgS2sStatus {
    id: string;
    name: string;
    interfaceName: string;
    enabled: boolean;
    connected: boolean;
    lastHandshake: string;
    transferRx: number;
    transferTx: number;
    endpoint: string;
    listenPort: number;
    localAddress: string;
    remoteSubnets: string[];
    forwardINOk: boolean;
}

export interface TunnelConfig {
    id: string;
    name: string;
    interfaceName: string;
    listenPort: number;
    tunnelAddress: string;
    peerPublicKey: string;
    peerEndpoint: string;
    allowedIPs: string[];
    localSubnets?: string[];
    persistentKeepalive: number;
    mtu: number;
    enabled: boolean;
    createdAt: string;
}

export interface SettingsFields {
    hostname: string;
    acceptDNS: boolean;
    acceptRoutes: boolean;
    shieldsUp: boolean;
    runSSH: boolean;
    noSNAT: boolean;
    udpPort: number;
    relayServerPort: number | null;
    relayServerEndpoints: string;
    advertiseTags: string[];
}

export interface WatcherHealth {
    status: string;
    lastSuccess?: string;
    reconnects: number;
    error?: string;
    degradedReason?: string;
}

export interface HealthSnapshot {
    status: string;
    watchers: Record<string, WatcherHealth>;
}

export interface Status extends SettingsFields {
    backendState: string;
    tailscaleIPs: string[];
    tailnetName: string;
    authURL: string;
    controlURL: string;
    version: string;
    self: SelfNode | null;
    health: string[];
    exitNode: boolean;
    exitNodeMode?: string;
    exitNodeClients?: ExitNodeClient[];
    routes: RouteStatus[];
    peers: PeerInfo[];
    derp: DERPInfo[];
    firewallHealth: FirewallHealth | null;
    dpiFingerprinting: boolean | null;
    integrationStatus: IntegrationStatus | null;
    wgS2sTunnels: WgS2sStatus[];
    connected: boolean;
    watcherHealth: HealthSnapshot | null;
}

export interface DeviceInfo {
    hostname: string;
    model: string;
    modelShort: string;
    firmware: string;
    unifiVersion: string;
    packageVersion: string;
    tailscaleVersion: string;
    hasTUN: boolean;
    hasUDAPISocket: boolean;
    persistentFree: number;
    activeVPNClients: string[];
    uptime: number;
}

export interface OperationResponse {
    ok: boolean;
}

export interface SetRoutesResult {
    ok: boolean;
    message: string;
    adminURL: string;
    warning?: string;
    confirmRequired?: boolean;
}

export interface FirewallStatusResponse {
    integrationAPI: boolean;
    chainPrefix: string;
    watcherRunning: boolean;
    lastRestore: string | null;
    rulesPresent: Record<string, boolean>;
    udapiReachable: boolean;
}

export interface SettingsResponse extends SettingsFields {
    controlURL: string;
}

export interface SettingsRequest {
    hostname?: string;
    acceptDNS?: boolean;
    acceptRoutes?: boolean;
    shieldsUp?: boolean;
    runSSH?: boolean;
    controlURL?: string;
    noSNAT?: boolean;
    udpPort?: number;
    relayServerPort?: number | null;
    relayServerEndpoints?: string;
    advertiseTags?: string[];
}

export interface DERPRegionInfo {
    regionID: number;
    regionCode: string;
    regionName: string;
    latencyMs: number;
    preferred: boolean;
}

export interface WgS2sTunnelDiag {
    id: string;
    name: string;
    interfaceName: string;
    interfaceUp: boolean;
    routesOk: boolean;
    forwardINOk: boolean;
    connected: boolean;
    endpoint?: string;
}

export interface WgS2sDiagnostics {
    wireguardModule: boolean;
    tunnels: WgS2sTunnelDiag[];
}

export interface DiagnosticsResponse {
    ipForwarding: string;
    fwmarkPatched: boolean;
    fwmarkValue: string;
    preferredDERP: number;
    derpRegions: DERPRegionInfo[];
    wgS2s?: WgS2sDiagnostics;
}

export interface SubnetConflict {
    cidr: string;
    conflictsWith: string;
    interface?: string;
    severity: string;
    message: string;
}

export interface FirewallStatus {
    zoneCreated: boolean;
    policiesReady: boolean;
    udapiApplied: boolean;
    errors?: string[];
}

export interface TunnelInfo extends TunnelConfig {
    publicKey?: string;
    status?: WgS2sStatus;
    zoneId?: string;
    zoneName?: string;
    warnings?: SubnetConflict[];
}

export interface TunnelCreateResponse extends TunnelInfo {
    setupStatus?: string;
    firewall?: FirewallStatus;
}

export interface TunnelUpdateResponse extends TunnelInfo {
    setupStatus?: string;
    firewall?: FirewallStatus;
}

export interface EnableTunnelResponse {
    ok: boolean;
    setupStatus?: string;
    firewall?: FirewallStatus;
}

export interface Keypair {
    publicKey: string;
    privateKey: string;
}

export interface WgS2sCreateRequest extends TunnelConfig {
    privateKey?: string;
    zoneId?: string;
    zoneName?: string;
    createZone?: boolean;
}

export interface WgS2sZoneEntry {
    zoneId: string;
    zoneName: string;
    tunnelCount: number;
}

export interface LogEntry {
    timestamp: string;
    level: string;
    message: string;
    source?: string;
}

export interface UpdateInfo {
    available: boolean;
    version: string;
    currentVersion: string;
    changelogURL: string;
    dismissed: boolean;
}

export interface StoreError {
    id: number;
    message: string;
    type: string;
    timestamp: string;
}
