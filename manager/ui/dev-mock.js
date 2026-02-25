// Vite plugin: mock API server for UI development without Go backend.
// Usage: MOCK=1 npm run dev -- --host 0.0.0.0

const CSRF_TOKEN = 'mock-csrf-token-dev';

const mockStatus = {
    backendState: 'Running',
    tailscaleIPs: ['100.65.93.69', 'fd7a:115c:a1e0::4145'],
    tailnetName: 'tail1234.ts.net',
    authURL: '',
    version: '1.95.0-unifi',
    controlURL: 'https://controlplane.tailscale.com',
    self: {
        id: 'n1234567890abcdef',
        publicKey: 'mock+ABC123publickey000000000000000000000=',
        hostName: 'udm-se-test-us-office',
        dnsName: 'udm-se-test-us-office.tail1234.ts.net.',
        os: 'linux',
        online: true,
        tailscaleIPs: ['100.65.93.69', 'fd7a:115c:a1e0::4145'],
        relay: 'nyc',
        curAddr: '203.0.113.42:41641',
        rxBytes: 1482000000,
        txBytes: 389000000,
    },
    health: [],
    exitNode: false,
    derp: [
        { regionID: 1, regionCode: 'nyc', regionName: 'New York', latencyMs: 4.2, preferred: true },
        { regionID: 12, regionCode: 'ord', regionName: 'Chicago', latencyMs: 8.1, preferred: false },
        { regionID: 2, regionCode: 'sfo', regionName: 'San Francisco', latencyMs: 22.7, preferred: false },
        { regionID: 13, regionCode: 'dfw', regionName: 'Dallas', latencyMs: 28.5, preferred: false },
        { regionID: 10, regionCode: 'fra', regionName: 'Frankfurt', latencyMs: 85.1, preferred: false },
        { regionID: 4, regionCode: 'ams', regionName: 'Amsterdam', latencyMs: 92.4, preferred: false },
        { regionID: 11, regionCode: 'waw', regionName: 'Warsaw', latencyMs: 98.9, preferred: false },
        { regionID: 3, regionCode: 'sin', regionName: 'Singapore', latencyMs: 168.3, preferred: false },
        { regionID: 7, regionCode: 'tok', regionName: 'Tokyo', latencyMs: 205.6, preferred: false },
    ],
    routes: [
        { cidr: '192.168.1.0/24', approved: true },
        { cidr: '10.10.0.0/16', approved: true },
        { cidr: '172.16.50.0/24', approved: false },
    ],
    peers: [
        {
            id: 'peer-1',
            hostName: 'macbook-john',
            dnsName: 'macbook-john.tail1234.ts.net.',
            os: 'macOS',
            online: true,
            tailscaleIP: '100.100.12.34',
            tailscaleIPs: ['100.100.12.34', 'fd7a:115c:a1e0::c22'],
            lastSeen: new Date().toISOString(),
            curAddr: '192.168.1.42:41641',
            relay: '',
            peerRelay: '',
            rxBytes: 52400000,
            txBytes: 21300000,
        },
        {
            id: 'peer-2',
            hostName: 'iphone-john',
            dnsName: 'iphone-john.tail1234.ts.net.',
            os: 'iOS',
            online: true,
            tailscaleIP: '100.100.56.78',
            tailscaleIPs: ['100.100.56.78'],
            lastSeen: new Date().toISOString(),
            curAddr: '',
            relay: '',
            peerRelay: '100.65.93.69:40000:vni:12345',
            rxBytes: 8200000,
            txBytes: 3100000,
        },
        {
            id: 'peer-3',
            hostName: 'nas-synology',
            dnsName: 'nas-synology.tail1234.ts.net.',
            os: 'linux',
            online: false,
            tailscaleIP: '100.100.99.10',
            tailscaleIPs: ['100.100.99.10'],
            lastSeen: new Date(Date.now() - 3600000 * 48).toISOString(),
            curAddr: '',
            relay: '',
            rxBytes: 120000000,
            txBytes: 95000000,
        },
        {
            id: 'peer-4',
            hostName: 'office-server-nyc',
            dnsName: 'office-server-nyc.tail1234.ts.net.',
            os: 'linux',
            online: true,
            tailscaleIP: '100.100.22.44',
            tailscaleIPs: ['100.100.22.44', 'fd7a:115c:a1e0::2c'],
            lastSeen: new Date().toISOString(),
            curAddr: '',
            relay: 'ord',
            rxBytes: 340000000,
            txBytes: 280000000,
        },
        {
            id: 'peer-5',
            hostName: 'dev-workstation',
            dnsName: 'dev-workstation.tail1234.ts.net.',
            os: 'linux',
            online: true,
            tailscaleIP: '100.100.88.2',
            tailscaleIPs: ['100.100.88.2'],
            lastSeen: new Date().toISOString(),
            curAddr: '10.153.1.123:41641',
            relay: '',
            rxBytes: 89000000,
            txBytes: 67000000,
        },
        {
            id: 'peer-6',
            hostName: 'windows-pc-alex',
            dnsName: 'windows-pc-alex.tail1234.ts.net.',
            os: 'windows',
            online: true,
            tailscaleIP: '100.100.33.17',
            tailscaleIPs: ['100.100.33.17'],
            lastSeen: new Date().toISOString(),
            curAddr: '192.168.1.55:41641',
            relay: '',
            rxBytes: 210000000,
            txBytes: 145000000,
        },
        {
            id: 'peer-7',
            hostName: 'rpi-monitoring',
            dnsName: 'rpi-monitoring.tail1234.ts.net.',
            os: 'linux',
            online: true,
            tailscaleIP: '100.100.44.88',
            tailscaleIPs: ['100.100.44.88'],
            lastSeen: new Date().toISOString(),
            curAddr: '',
            relay: 'fra',
            rxBytes: 5600000,
            txBytes: 2300000,
        },
        {
            id: 'peer-8',
            hostName: 'ipad-work',
            dnsName: 'ipad-work.tail1234.ts.net.',
            os: 'iOS',
            online: false,
            tailscaleIP: '100.100.55.12',
            tailscaleIPs: ['100.100.55.12'],
            lastSeen: new Date(Date.now() - 3600000 * 5).toISOString(),
            curAddr: '',
            relay: '',
            rxBytes: 32000000,
            txBytes: 18000000,
        },
        {
            id: 'peer-9',
            hostName: 'docker-host-prod',
            dnsName: 'docker-host-prod.tail1234.ts.net.',
            os: 'linux',
            online: true,
            tailscaleIP: '100.100.66.3',
            tailscaleIPs: ['100.100.66.3', 'fd7a:115c:a1e0::6603'],
            lastSeen: new Date().toISOString(),
            curAddr: '10.20.0.5:41641',
            relay: '',
            rxBytes: 780000000,
            txBytes: 520000000,
        },
        {
            id: 'peer-10',
            hostName: 'android-john',
            dnsName: 'android-john.tail1234.ts.net.',
            os: 'android',
            online: true,
            tailscaleIP: '100.100.77.21',
            tailscaleIPs: ['100.100.77.21'],
            lastSeen: new Date().toISOString(),
            curAddr: '',
            relay: 'nyc',
            rxBytes: 15000000,
            txBytes: 9800000,
        },
        {
            id: 'peer-11',
            hostName: 'backup-server-hel',
            dnsName: 'backup-server-hel.tail1234.ts.net.',
            os: 'linux',
            online: false,
            tailscaleIP: '100.100.88.99',
            tailscaleIPs: ['100.100.88.99'],
            lastSeen: new Date(Date.now() - 3600000 * 72).toISOString(),
            curAddr: '',
            relay: '',
            rxBytes: 450000000,
            txBytes: 380000000,
        },
        {
            id: 'peer-12',
            hostName: 'gitlab-runner',
            dnsName: 'gitlab-runner.tail1234.ts.net.',
            os: 'linux',
            online: true,
            tailscaleIP: '100.100.91.5',
            tailscaleIPs: ['100.100.91.5'],
            lastSeen: new Date().toISOString(),
            curAddr: '10.20.0.12:41641',
            relay: '',
            rxBytes: 124000000,
            txBytes: 98000000,
        },
    ],
    integrationStatus: {
        configured: true,
        valid: true,
        siteId: '88f7af54-98f8-306a-a1c7-c9349722b1f6',
        appVersion: '10.1.85',
    },
    firewallHealth: {
        zoneActive: true,
        watcherRunning: true,
        udapiReachable: true,
    },
    wgS2sTunnels: [
        {
            id: 'tun-nyc-office',
            name: 'NYC Office',
            enabled: true,
            connected: true,
            lastHandshake: new Date(Date.now() - 45000).toISOString(),
            transferRx: 1250000000,
            transferTx: 890000000,
            endpoint: '85.12.34.56:51820',
            forwardINRule: true,
        },
        {
            id: 'tun-home-lab',
            name: 'Home Lab',
            enabled: false,
            connected: false,
            lastHandshake: new Date(Date.now() - 3600000 * 6).toISOString(),
            transferRx: 45000000,
            transferTx: 12000000,
            endpoint: '78.90.12.34:51821',
            forwardINRule: true,
        },
        {
            id: 'tun-helsinki-dc',
            name: 'Helsinki DC',
            enabled: true,
            connected: true,
            lastHandshake: new Date(Date.now() - 12000).toISOString(),
            transferRx: 3200000000,
            transferTx: 1800000000,
            endpoint: '185.60.22.10:51822',
            forwardINRule: true,
        },
        {
            id: 'tun-berlin-office',
            name: 'Berlin Office',
            enabled: true,
            connected: true,
            lastHandshake: new Date(Date.now() - 28000).toISOString(),
            transferRx: 670000000,
            transferTx: 410000000,
            endpoint: '91.44.78.15:51823',
            forwardINRule: true,
        },
        {
            id: 'tun-aws-eu-west',
            name: 'AWS eu-west-1',
            enabled: true,
            connected: false,
            lastHandshake: new Date(Date.now() - 3600000 * 2).toISOString(),
            transferRx: 89000000,
            transferTx: 56000000,
            endpoint: '52.18.44.100:51824',
            forwardINRule: true,
        },
        {
            id: 'tun-gcp-us-central',
            name: 'GCP us-central1',
            enabled: false,
            connected: false,
            lastHandshake: new Date(Date.now() - 3600000 * 24).toISOString(),
            transferRx: 12000000,
            transferTx: 8000000,
            endpoint: '35.192.0.50:51825',
            forwardINRule: false,
        },
    ],
    connected: true,
};

const mockDevice = {
    hostname: 'UDM-SE-TEST-US-OFFICE',
    model: 'UDM-SE',
    firmware: '4.1.13',
    unifiVersion: '9.0.114',
    mac: 'f4:e2:c6:ab:cd:ef',
    uptime: 864000,
    architecture: 'aarch64',
    kernelVersion: '4.19.152-ui-alpine',
    hasTUN: true,
    hasUDAPISocket: true,
    persistentFree: '1.4 GB free',
    activeVPNClients: [],
};

const mockWgS2sZones = [
    { zoneId: 'zone-wg-s2s-001', zoneName: 'WireGuard S2S', tunnelCount: 2 },
];

const mockTunnels = [
    {
        id: 'tun-nyc-office',
        name: 'NYC Office',
        interfaceName: 'wgs2s0',
        listenPort: 51820,
        tunnelAddress: '10.255.0.1/30',
        peerPublicKey: 'dGhpcyBpcyBhIGZha2UgcHVibGljIGtleSBiYXNlNjQ=',
        peerEndpoint: '85.12.34.56:51820',
        allowedIPs: ['192.168.10.0/24', '192.168.20.0/24'],
        persistentKeepalive: 25,
        mtu: 1420,
        enabled: true,
        zoneId: 'zone-wg-s2s-001',
        zoneName: 'WireGuard S2S',
    },
    {
        id: 'tun-home-lab',
        name: 'Home Lab',
        interfaceName: 'wgs2s1',
        listenPort: 51821,
        tunnelAddress: '10.255.1.1/30',
        peerPublicKey: 'YW5vdGhlciBmYWtlIHB1YmxpYyBrZXkgYmFzZTY0MQ==',
        peerEndpoint: '78.90.12.34:51821',
        allowedIPs: ['10.0.0.0/24'],
        persistentKeepalive: 25,
        mtu: 1420,
        enabled: false,
        zoneId: 'zone-wg-s2s-001',
        zoneName: 'WireGuard S2S',
    },
];

const mockSubnets = {
    subnets: [
        { cidr: '192.168.1.0/24', name: 'Default (br0)' },
        { cidr: '10.10.0.0/16', name: 'IoT VLAN (br10)' },
        { cidr: '172.16.50.0/24', name: 'Guest (br50)' },
    ],
};

const mockSettings = {
    hostname: 'udm-se-test-us-office',
    acceptRoutes: false,
    shieldsUp: false,
    runSSH: true,
    udpPort: 41641,
    controlURL: '',
    noSNAT: false,
    relayServerPort: null,
    relayServerEndpoints: '',
    advertiseTags: [],
};

const mockFirewall = {
    integrationAPI: true,
    chainPrefix: 'VPN',
    watcherRunning: true,
    udapiReachable: true,
    lastRestore: new Date(Date.now() - 7200000).toISOString(),
    rulesPresent: {
        forward: true,
        input: true,
        output: true,
        ipset: true,
    },
};

const mockDiagnostics = {
    ipForwarding: 'enabled',
    fwmarkPatched: true,
    preferredDERP: 1,
    derpRegions: [
        { regionID: 1, regionCode: 'nyc', regionName: 'New York', latencyMs: 4.2 },
        { regionID: 12, regionCode: 'ord', regionName: 'Chicago', latencyMs: 8.1 },
        { regionID: 2, regionCode: 'sfo', regionName: 'San Francisco', latencyMs: 22.7 },
        { regionID: 13, regionCode: 'dfw', regionName: 'Dallas', latencyMs: 28.5 },
        { regionID: 10, regionCode: 'fra', regionName: 'Frankfurt', latencyMs: 85.1 },
        { regionID: 4, regionCode: 'ams', regionName: 'Amsterdam', latencyMs: 92.4 },
        { regionID: 3, regionCode: 'sin', regionName: 'Singapore', latencyMs: 168.3 },
        { regionID: 7, regionCode: 'tok', regionName: 'Tokyo', latencyMs: 205.6 },
    ],
    wgS2s: {
        wireguardModule: true,
        tunnels: [
            { id: 'tun-nyc-office', name: 'NYC Office', interfaceName: 'wg-s2s0',
              interfaceUp: true, routesOk: true, forwardINOk: true, connected: true,
              endpoint: '85.12.34.56:51820' },
            { id: 'tun-helsinki-dc', name: 'Helsinki DC', interfaceName: 'wg-s2s2',
              interfaceUp: true, routesOk: true, forwardINOk: true, connected: true,
              endpoint: '185.60.22.10:51822' },
            { id: 'tun-aws-eu-west', name: 'AWS eu-west-1', interfaceName: 'wg-s2s4',
              interfaceUp: true, routesOk: false, forwardINOk: true, connected: false,
              endpoint: '52.18.44.100:51824' },
        ],
    },
};

function makeTimestamp(offsetMs) {
    return new Date(Date.now() - offsetMs).toISOString();
}

const mockLogs = {
    lines: [
        { level: 'info', message: 'tailscaled started, version 1.95.0-unifi, package unifi-tailscale', timestamp: makeTimestamp(86400000) },
        { level: 'info', message: 'wgengine: Using kernel TUN device tailscale0', timestamp: makeTimestamp(86399000) },
        { level: 'info', message: 'magicsock: disco key is d:abcdef1234567890', timestamp: makeTimestamp(86398000) },
        { level: 'info', message: 'netcheck: UDP=true IPv4=true IPv6=false MappingVariesByDestIP=false HairPinning=false', timestamp: makeTimestamp(86397000) },
        { level: 'info', message: 'netcheck: preferred DERP region 1 (New York), latency 4.2ms', timestamp: makeTimestamp(86396000) },
        { level: 'info', message: 'DERP home is nyc (New York)', timestamp: makeTimestamp(86395000) },
        { level: 'info', message: 'control: NetInfo: derp=1 portmap= v4=true v6=false udp=true', timestamp: makeTimestamp(86394000) },
        { level: 'info', message: 'Firewall zone active: tailscale0 bound to VPN_IN via UDAPI socket', timestamp: makeTimestamp(86100000) },
        { level: 'info', message: 'Firewall watcher started, monitoring ubios-udapi-server config pushes', timestamp: makeTimestamp(86000000) },
        { level: 'info', message: 'accept-routes=false, exit-node=false, shields-up=false, ssh=true', timestamp: makeTimestamp(85900000) },
        { level: 'info', message: 'Advertising routes: 192.168.1.0/24, 10.10.0.0/16, 172.16.50.0/24', timestamp: makeTimestamp(72000000) },
        { level: 'warn', message: 'Route 172.16.50.0/24 not yet approved in admin console', timestamp: makeTimestamp(71900000) },
        { level: 'info', message: 'wgs2s0: WireGuard S2S tunnel "NYC Office" started on port 51820', timestamp: makeTimestamp(43200000) },
        { level: 'info', message: 'wgs2s0: handshake completed with peer 85.12.34.56:51820', timestamp: makeTimestamp(43100000) },
        { level: 'info', message: 'wgs2s0: FORWARD_IN rule applied — VPN_IN zone, both IP versions', timestamp: makeTimestamp(43000000) },
        { level: 'info', message: 'Peer macbook-john (100.100.12.34) connected via DERP(nyc)', timestamp: makeTimestamp(7200000) },
        { level: 'info', message: 'Peer macbook-john direct connection established: 192.168.1.42:41641', timestamp: makeTimestamp(7190000) },
        { level: 'info', message: 'Peer iphone-john (100.100.56.78) connected via DERP(nyc)', timestamp: makeTimestamp(3600000) },
        { level: 'info', message: 'Peer office-server-nyc (100.100.22.44) connected via DERP(ord)', timestamp: makeTimestamp(1800000) },
        { level: 'warn', message: 'Peer nas-synology (100.100.99.10) offline for >24h', timestamp: makeTimestamp(900000) },
        { level: 'info', message: 'wgs2s0: latest handshake 45s ago, rx=1.16 GB, tx=829 MB', timestamp: makeTimestamp(120000) },
        { level: 'info', message: 'Firewall watcher: rules intact after config push event', timestamp: makeTimestamp(60000) },
        { level: 'error', message: 'wgs2s1: handshake timeout with peer 78.90.12.34:51821 (tunnel disabled)', timestamp: makeTimestamp(30000) },
        { level: 'info', message: 'Health check: zone=ok watcher=ok udapi=ok', timestamp: makeTimestamp(10000) },
    ],
};

function json(res, data, statusCode = 200) {
    res.writeHead(statusCode, {
        'Content-Type': 'application/json',
        'X-Csrf-Token': CSRF_TOKEN,
    });
    res.end(JSON.stringify(data));
}

export default function mockApiPlugin() {
    if (!process.env.MOCK) return { name: 'mock-api-noop' };

    console.log('\n  \x1b[33m⚡ Mock API enabled\x1b[0m\n');

    let sseClients = [];

    setInterval(() => {
        mockStatus.self.rxBytes += Math.floor(Math.random() * 50000);
        mockStatus.self.txBytes += Math.floor(Math.random() * 20000);
        if (mockStatus.wgS2sTunnels[0]) {
            mockStatus.wgS2sTunnels[0].transferRx += Math.floor(Math.random() * 30000);
            mockStatus.wgS2sTunnels[0].transferTx += Math.floor(Math.random() * 15000);
            mockStatus.wgS2sTunnels[0].lastHandshake = new Date(Date.now() - Math.random() * 90000).toISOString();
        }
        for (const p of mockStatus.peers) {
            if (p.online) p.lastSeen = new Date().toISOString();
        }

        const msg = `data: ${JSON.stringify(mockStatus)}\n\n`;
        sseClients = sseClients.filter(res => {
            try { res.write(msg); return true; } catch { return false; }
        });
    }, 3000);

    return {
        name: 'mock-api',
        configureServer(server) {
            server.middlewares.use((req, res, next) => {
                const url = req.url?.replace(/\?.*$/, '') ?? '';

                if (!url.startsWith('/vpn-pack/api')) return next();

                const path = url.replace('/vpn-pack/api', '');

                // SSE
                if (path === '/events') {
                    res.writeHead(200, {
                        'Content-Type': 'text/event-stream',
                        'Cache-Control': 'no-cache',
                        'Connection': 'keep-alive',
                        'X-Csrf-Token': CSRF_TOKEN,
                    });
                    res.write(`data: ${JSON.stringify(mockStatus)}\n\n`);
                    sseClients.push(res);
                    req.on('close', () => {
                        sseClients = sseClients.filter(c => c !== res);
                    });
                    return;
                }

                // GET endpoints
                if (path === '/status') return json(res, mockStatus);
                if (path === '/device') return json(res, mockDevice);
                if (path === '/subnets') return json(res, mockSubnets);
                if (path === '/firewall') return json(res, mockFirewall);
                if (path === '/settings' && req.method === 'GET') return json(res, mockSettings);
                if (path === '/diagnostics') return json(res, mockDiagnostics);
                if (path === '/logs') return json(res, mockLogs);
                if (path === '/routes' && req.method === 'GET') return json(res, mockStatus.routes);

                // WG S2S reads
                if (path === '/wg-s2s/zones') return json(res, mockWgS2sZones);
                if (path === '/wg-s2s/tunnels' && req.method === 'GET') return json(res, mockTunnels);
                if (path === '/wg-s2s/wan-ip') return json(res, { ip: '203.0.113.42' });
                if (path === '/wg-s2s/local-subnets') return json(res, [
                    { cidr: '192.168.1.0/24', name: 'Default (br0)' },
                    { cidr: '10.10.0.0/16', name: 'IoT VLAN (br10)' },
                ]);
                if (path === '/wg-s2s/generate-keypair') {
                    return json(res, {
                        publicKey: 'bW9jay1wdWJsaWMta2V5LWZvci1kZXYtdGVzdGluZw==',
                        privateKey: '(stored server-side)',
                    });
                }
                if (path.match(/^\/wg-s2s\/tunnels\/[\w-]+\/config$/)) {
                    const id = path.split('/')[3];
                    const tun = mockTunnels.find(t => t.id === id);
                    if (!tun) return json(res, { error: 'not found' }, 404);
                    return json(res, {
                        config: `[Interface]\nListenPort = ${tun.listenPort}\nAddress = ${tun.tunnelAddress}\n\n[Peer]\nPublicKey = ${tun.peerPublicKey}\nEndpoint = ${tun.peerEndpoint}\nAllowedIPs = ${tun.allowedIPs.join(', ')}\nPersistentKeepalive = ${tun.persistentKeepalive}\n`,
                    });
                }

                // Integration API
                if (path === '/integration/status') return json(res, mockStatus.integrationStatus);
                if (path === '/integration/api-key' && req.method === 'POST') {
                    let body = '';
                    req.on('data', c => body += c);
                    req.on('end', () => {
                        const { apiKey } = JSON.parse(body);
                        if (!apiKey?.trim()) return json(res, { error: 'API key is required' }, 400);
                        mockStatus.integrationStatus = {
                            configured: true,
                            valid: true,
                            siteId: '88f7af54-98f8-306a-a1c7-c9349722b1f6',
                            appVersion: '10.1.85',
                        };
                        mockFirewall.integrationAPI = true;
                        json(res, mockStatus.integrationStatus);
                    });
                    return;
                }
                if (path === '/integration/api-key' && req.method === 'DELETE') {
                    mockStatus.integrationStatus = { configured: false };
                    mockFirewall.integrationAPI = false;
                    return json(res, { ok: true });
                }
                if (path === '/integration/test') {
                    if (!mockStatus.integrationStatus.configured) {
                        return json(res, { ok: false, error: 'no API key configured' });
                    }
                    return json(res, {
                        ok: true,
                        siteId: '88f7af54-98f8-306a-a1c7-c9349722b1f6',
                        appVersion: '10.1.85',
                    });
                }

                // Mutations
                if (path === '/tailscale/up') {
                    mockStatus.backendState = 'Running';
                    mockStatus.authURL = '';
                    return json(res, { ok: true });
                }
                if (path === '/tailscale/down') {
                    mockStatus.backendState = 'Stopped';
                    return json(res, { ok: true });
                }
                if (path === '/tailscale/login') {
                    mockStatus.backendState = 'NeedsLogin';
                    mockStatus.authURL = 'https://login.tailscale.com/a/mock123456';
                    return json(res, { authURL: mockStatus.authURL });
                }
                if (path === '/tailscale/logout') {
                    mockStatus.backendState = 'NeedsLogin';
                    mockStatus.authURL = '';
                    mockStatus.tailscaleIPs = [];
                    mockStatus.self = null;
                    mockStatus.peers = [];
                    return json(res, { ok: true });
                }
                if (path === '/tailscale/auth-key') return json(res, { ok: true });
                if (path === '/routes' && req.method === 'POST') return json(res, { ok: true });
                if (path === '/settings' && req.method === 'POST') {
                    let body = '';
                    req.on('data', c => body += c);
                    req.on('end', () => {
                        const updates = JSON.parse(body);
                        Object.assign(mockSettings, updates);
                        json(res, mockSettings);
                    });
                    return;
                }

                // WG S2S mutations
                if (path === '/wg-s2s/tunnels' && req.method === 'POST') {
                    let body = '';
                    req.on('data', c => body += c);
                    req.on('end', () => {
                        const data = JSON.parse(body);
                        const zoneId = data.zoneId === 'new' ? 'zone-wg-' + Date.now() : (data.zoneId || mockWgS2sZones[0]?.zoneId || '');
                        const zoneName = data.zoneId === 'new' ? (data.zoneName || 'WireGuard S2S') : (mockWgS2sZones.find(z => z.zoneId === zoneId)?.zoneName || 'WireGuard S2S');
                        const newTunnel = { id: 'tun-' + Date.now(), interfaceName: 'wgs2s' + mockTunnels.length, ...data, enabled: true, zoneId, zoneName };
                        delete newTunnel.zoneId; delete newTunnel.zoneName;
                        newTunnel.zoneId = zoneId;
                        newTunnel.zoneName = zoneName;
                        mockTunnels.push(newTunnel);
                        json(res, newTunnel);
                    });
                    return;
                }
                if (path.match(/^\/wg-s2s\/tunnels\/[\w-]+$/) && req.method === 'DELETE') {
                    const id = path.split('/')[3];
                    const idx = mockTunnels.findIndex(t => t.id === id);
                    if (idx !== -1) mockTunnels.splice(idx, 1);
                    return json(res, { ok: true });
                }
                if (path.match(/^\/wg-s2s\/tunnels\/[\w-]+\/enable$/)) {
                    const id = path.split('/')[3];
                    const tun = mockTunnels.find(t => t.id === id);
                    if (tun) tun.enabled = true;
                    return json(res, { ok: true });
                }
                if (path.match(/^\/wg-s2s\/tunnels\/[\w-]+\/disable$/)) {
                    const id = path.split('/')[3];
                    const tun = mockTunnels.find(t => t.id === id);
                    if (tun) tun.enabled = false;
                    return json(res, { ok: true });
                }
                if (path.match(/^\/wg-s2s\/tunnels\/[\w-]+$/) && req.method === 'PATCH') {
                    const id = path.split('/')[3];
                    let body = '';
                    req.on('data', c => body += c);
                    req.on('end', () => {
                        const updates = JSON.parse(body);
                        const tun = mockTunnels.find(t => t.id === id);
                        if (tun) Object.assign(tun, updates);
                        json(res, tun || { error: 'not found' });
                    });
                    return;
                }

                if (path === '/bugreport') return json(res, { reportId: 'BUG-20260222-mock-' + Date.now() });

                json(res, { error: 'not found' }, 404);
            });
        },
    };
}
