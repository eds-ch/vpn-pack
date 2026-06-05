package wgs2s

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/jsimonetti/rtnetlink"
	"golang.org/x/sys/unix"
	"golang.zx2c4.com/wireguard/wgctrl"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"

	"unifi-tailscale/manager/ops"
)

const (
	errFmtTunnelNotFound = "tunnel %s not found"
	configFileName       = "tunnels.json"
)

func validTunnelID(id string) bool {
	if id == "" || len(id) > 64 {
		return false
	}
	for _, r := range id {
		isAlnum := (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
		if !isAlnum && r != '-' && r != '_' {
			return false
		}
	}
	return true
}

type TunnelManager struct {
	mu          sync.Mutex
	config      *TunnelsConfig
	configDir   string
	wgClient    *wgctrl.Client
	rtConn      *rtnetlink.Conn
	routes      routeOps
	routeRefs   *routeRefCounter
	lookupIface func(name string) (uint32, bool)
	listIfaces  func(prefix string) []ifaceEntry
	deleteLink  func(idx uint32) error
	log         *slog.Logger

	// bringUpForTest, when set, replaces the production bringUp pipeline so
	// tests can drive RestoreAll without touching real netlink/wireguard.
	bringUpForTest func(cfg TunnelConfig) error

	// saveOverride, when set, replaces the disk persistence step so tests can
	// observe save invocations (state snapshots, call ordering) or inject
	// failures without touching a real filesystem.
	saveOverride func() error
}

type ifaceEntry struct {
	name string
	idx  uint32
}

func NewTunnelManager(configDir string, log *slog.Logger) (*TunnelManager, error) {
	if err := os.MkdirAll(configDir, 0700); err != nil {
		return nil, fmt.Errorf("create config dir: %w", err)
	}

	wgClient, err := wgctrl.New()
	if err != nil {
		return nil, fmt.Errorf("create wgctrl client: %w", err)
	}

	rtConn, err := rtnetlink.Dial(nil)
	if err != nil {
		_ = wgClient.Close()
		return nil, fmt.Errorf("create rtnetlink conn: %w", err)
	}

	cfgPath := filepath.Join(configDir, configFileName)
	cfg, err := loadConfig(cfgPath)
	if err != nil {
		_ = rtConn.Close()
		_ = wgClient.Close()
		return nil, fmt.Errorf("load config: %w", err)
	}

	return &TunnelManager{
		config:      cfg,
		configDir:   configDir,
		wgClient:    wgClient,
		rtConn:      rtConn,
		routes:      &rtnetlinkRoutes{conn: rtConn},
		routeRefs:   newRouteRefCounter(),
		lookupIface: getInterfaceIndex,
		listIfaces:  listInterfacesByPrefix,
		deleteLink:  func(idx uint32) error { return deleteInterface(rtConn, idx) },
		log:         log,
	}, nil
}

func (m *TunnelManager) Close() {
	if m.wgClient != nil {
		_ = m.wgClient.Close()
	}
	if m.rtConn != nil {
		_ = m.rtConn.Close()
	}
}

func (m *TunnelManager) CreateTunnel(cfg TunnelConfig, privateKey string) (*TunnelConfig, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if cfg.Name == "" {
		return nil, fmt.Errorf("tunnel name is required")
	}
	if cfg.ListenPort == 0 {
		return nil, fmt.Errorf("listen port is required")
	}
	if cfg.TunnelAddress == "" {
		return nil, fmt.Errorf("tunnel address is required")
	}

	if err := checkPortAvailable(cfg.ListenPort); err != nil {
		return nil, fmt.Errorf("port %d: %w", cfg.ListenPort, err)
	}

	var err error
	cfg.ID, err = generateID()
	if err != nil {
		return nil, err
	}
	cfg.InterfaceName = nextInterfaceName(m.config.Tunnels)
	cfg.CreatedAt = time.Now()
	cfg.Enabled = true

	if cfg.PersistentKeepalive == 0 {
		cfg.PersistentKeepalive = defaultPersistentKeepalive
	}
	if cfg.MTU == 0 {
		cfg.MTU = defaultMTU
	}
	if cfg.RouteMetric == 0 {
		cfg.RouteMetric = defaultRouteMetric
	}

	var privKey wgtypes.Key
	if privateKey != "" {
		privKey, err = saveExistingKeypair(m.configDir, cfg.ID, privateKey)
	} else {
		privKey, err = generateKeypair(m.configDir, cfg.ID)
	}
	if err != nil {
		return nil, err
	}

	if err := m.bringUp(cfg, privKey); err != nil {
		deleteKeyFiles(m.configDir, cfg.ID)
		return nil, err
	}

	m.config.Tunnels = append(m.config.Tunnels, cfg)
	if err := m.save(); err != nil {
		if tdErr := m.tearDown(cfg); tdErr != nil {
			m.log.Warn("CreateTunnel: rollback tearDown failed", "id", cfg.ID, "err", tdErr)
		}
		deleteKeyFiles(m.configDir, cfg.ID)
		return nil, err
	}

	m.log.Info("tunnel created", "id", cfg.ID, "name", cfg.Name, "iface", cfg.InterfaceName)
	return &cfg, nil
}

func (m *TunnelManager) DeleteTunnel(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	idx := m.findTunnel(id)
	if idx < 0 {
		return fmt.Errorf(errFmtTunnelNotFound, id)
	}

	cfg := m.config.Tunnels[idx]

	err := ops.Run(context.Background(), []ops.Op{
		{
			Name: "tear down kernel interface",
			Do: func(_ context.Context) error {
				if !cfg.Enabled {
					return nil
				}
				return m.tearDown(cfg)
			},
			Undo: func(_ context.Context) error {
				if !cfg.Enabled {
					return nil
				}
				return m.bringUpTunnel(cfg)
			},
		},
		{
			Name: "remove tunnel from disk config",
			Do: func(_ context.Context) error {
				m.config.Tunnels = append(m.config.Tunnels[:idx], m.config.Tunnels[idx+1:]...)
				return m.save()
			},
			Undo: func(_ context.Context) error {
				m.config.Tunnels = slices.Insert(m.config.Tunnels, idx, cfg)
				_ = m.save()
				return nil
			},
		},
		ops.Noop("delete key files", func(_ context.Context) error {
			deleteKeyFiles(m.configDir, cfg.ID)
			return nil
		}),
	})
	if err != nil {
		return err
	}

	m.log.Info("tunnel deleted", "id", id, "name", cfg.Name)
	return nil
}

// bringUpTunnel runs the production bring-up unless a test seam is installed.
// The test seam is used by RestoreAll and the Enable/Disable sagas so unit
// tests don't have to touch real netlink / wireguard.
func (m *TunnelManager) bringUpTunnel(cfg TunnelConfig) error {
	if m.bringUpForTest != nil {
		return m.bringUpForTest(cfg)
	}
	privKey, err := loadPrivateKey(m.configDir, cfg.ID)
	if err != nil {
		return fmt.Errorf("load key: %w", err)
	}
	return m.bringUp(cfg, privKey)
}

func (m *TunnelManager) EnableTunnel(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	idx := m.findTunnel(id)
	if idx < 0 {
		return fmt.Errorf(errFmtTunnelNotFound, id)
	}
	if m.config.Tunnels[idx].Enabled {
		return nil
	}
	cfgVal := m.config.Tunnels[idx]

	return ops.Run(context.Background(), []ops.Op{
		{
			Name: "bring up kernel interface",
			Do:   func(_ context.Context) error { return m.bringUpTunnel(cfgVal) },
			Undo: func(_ context.Context) error {
				ifIdx, ok := m.lookupIface(cfgVal.InterfaceName)
				if !ok {
					return nil
				}
				m.releaseRoutes(cfgVal.ID, ifIdx, cfgVal.AllowedIPs, effectiveMetric(cfgVal.RouteMetric))
				return m.deleteLink(ifIdx)
			},
		},
		ops.Noop("persist enabled=true to disk", func(_ context.Context) error {
			m.config.Tunnels[idx].Enabled = true
			return m.save()
		}),
	})
}

func (m *TunnelManager) DisableTunnel(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	idx := m.findTunnel(id)
	if idx < 0 {
		return fmt.Errorf(errFmtTunnelNotFound, id)
	}
	if !m.config.Tunnels[idx].Enabled {
		return nil
	}
	cfgVal := m.config.Tunnels[idx]

	return ops.Run(context.Background(), []ops.Op{
		{
			Name: "tear down kernel interface",
			Do:   func(_ context.Context) error { return m.tearDown(cfgVal) },
			Undo: func(_ context.Context) error { return m.bringUpTunnel(cfgVal) },
		},
		ops.Noop("persist enabled=false to disk", func(_ context.Context) error {
			m.config.Tunnels[idx].Enabled = false
			return m.save()
		}),
	})
}

func applyUpdates(base, updates TunnelConfig) TunnelConfig {
	merged := base
	if updates.Name != "" {
		merged.Name = updates.Name
	}
	if updates.ListenPort != 0 {
		merged.ListenPort = updates.ListenPort
	}
	if updates.TunnelAddress != "" {
		merged.TunnelAddress = updates.TunnelAddress
	}
	if updates.PeerPublicKey != "" {
		merged.PeerPublicKey = updates.PeerPublicKey
	}
	if updates.PeerEndpoint != "" {
		merged.PeerEndpoint = updates.PeerEndpoint
	}
	if updates.AllowedIPs != nil {
		merged.AllowedIPs = updates.AllowedIPs
	}
	if updates.LocalSubnets != nil {
		merged.LocalSubnets = updates.LocalSubnets
	}
	if updates.PersistentKeepalive != 0 {
		merged.PersistentKeepalive = updates.PersistentKeepalive
	}
	if updates.MTU != 0 {
		merged.MTU = updates.MTU
	}
	if updates.RouteMetric != 0 {
		merged.RouteMetric = updates.RouteMetric
	}
	return merged
}

func needsRecreate(old, merged TunnelConfig) bool {
	return old.ListenPort != merged.ListenPort ||
		old.TunnelAddress != merged.TunnelAddress ||
		old.MTU != merged.MTU ||
		old.PeerPublicKey != merged.PeerPublicKey
}

func (m *TunnelManager) UpdateTunnel(id string, updates TunnelConfig) (*TunnelConfig, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	idx := m.findTunnel(id)
	if idx < 0 {
		return nil, fmt.Errorf(errFmtTunnelNotFound, id)
	}

	cfg := &m.config.Tunnels[idx]
	wasEnabled := cfg.Enabled

	merged := applyUpdates(*cfg, updates)

	if merged.TunnelAddress != "" {
		if _, _, err := net.ParseCIDR(merged.TunnelAddress); err != nil {
			return nil, fmt.Errorf("invalid tunnel address %q: %w", merged.TunnelAddress, err)
		}
	}

	if wasEnabled {
		if merged.PeerEndpoint != "" && merged.PeerEndpoint != cfg.PeerEndpoint {
			if _, err := net.ResolveUDPAddr("udp", merged.PeerEndpoint); err != nil {
				return nil, fmt.Errorf("preflight: cannot resolve peer endpoint %s: %w", merged.PeerEndpoint, err)
			}
		}
		if merged.ListenPort != cfg.ListenPort && merged.ListenPort != 0 {
			if err := checkPortAvailable(merged.ListenPort); err != nil {
				return nil, fmt.Errorf("preflight: port %d: %w", merged.ListenPort, err)
			}
		}
	}

	recreate := !wasEnabled || needsRecreate(*cfg, merged)
	oldAllowedIPs := cfg.AllowedIPs
	oldMetric := effectiveMetric(cfg.RouteMetric)
	oldCfg := *cfg

	// Kernel-first: drive the kernel ops against a local copy so m.config
	// stays at the old value. Commit `merged` to m.config and save only after
	// the kernel side succeeds. If the kernel ops fail, both disk and the
	// in-memory config keep the prior state (BUG-M11 contract extended).
	local := merged
	if wasEnabled && recreate {
		if err := m.recreateTunnel(&local, true); err != nil {
			return nil, err
		}
	} else if wasEnabled && !recreate {
		if err := m.hotUpdate(local, oldAllowedIPs, oldMetric); err != nil {
			m.log.Warn("hot update failed, falling back to recreate", "id", local.ID, "err", err)
			if err := m.recreateTunnel(&local, true); err != nil {
				return nil, err
			}
		}
	}

	m.config.Tunnels[idx] = local
	if err := m.save(); err != nil {
		m.config.Tunnels[idx] = oldCfg
		return nil, err
	}

	result := m.config.Tunnels[idx]
	return &result, nil
}

func (m *TunnelManager) hotUpdate(cfg TunnelConfig, oldAllowedIPs []string, oldMetric int) error {
	peer := &peerConfig{
		PublicKey:           cfg.PeerPublicKey,
		Endpoint:            cfg.PeerEndpoint,
		AllowedIPs:          cfg.AllowedIPs,
		PersistentKeepalive: cfg.PersistentKeepalive,
	}
	if err := updatePeer(m.wgClient, cfg.InterfaceName, peer); err != nil {
		return err
	}

	ifIndex, ok := m.lookupIface(cfg.InterfaceName)
	if !ok {
		return fmt.Errorf("interface %s not found", cfg.InterfaceName)
	}

	newMetric := effectiveMetric(cfg.RouteMetric)
	if !slices.Equal(oldAllowedIPs, cfg.AllowedIPs) || oldMetric != newMetric {
		m.releaseRoutes(cfg.ID, ifIndex, oldAllowedIPs, oldMetric)
		if err := m.claimRoutes(cfg.ID, ifIndex, cfg.AllowedIPs, newMetric); err != nil {
			return fmt.Errorf("hot update: claimRoutes: %w", err)
		}
	}

	m.log.Info("tunnel hot-updated", "id", cfg.ID, "name", cfg.Name)
	return nil
}

func (m *TunnelManager) RestoreAll() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.removeOrphanInterfaces()

	var errs []error
	for _, cfg := range m.config.Tunnels {
		if !cfg.Enabled {
			continue
		}

		if m.bringUpForTest != nil {
			if err := m.bringUpForTest(cfg); err != nil {
				m.log.Warn("restore: failed to bring up tunnel", "id", cfg.ID, "err", err)
				errs = append(errs, err)
				continue
			}
			m.log.Info("tunnel restored", "id", cfg.ID, "name", cfg.Name, "iface", cfg.InterfaceName)
			continue
		}

		privKey, err := loadPrivateKey(m.configDir, cfg.ID)
		if err != nil {
			m.log.Warn("restore: failed to load key", "id", cfg.ID, "err", err)
			errs = append(errs, err)
			continue
		}

		if err := m.bringUp(cfg, privKey); err != nil {
			m.log.Warn("restore: failed to bring up tunnel", "id", cfg.ID, "err", err)
			errs = append(errs, err)
			continue
		}

		m.log.Info("tunnel restored", "id", cfg.ID, "name", cfg.Name, "iface", cfg.InterfaceName)
	}
	return errors.Join(errs...)
}

// removeOrphanInterfaces sweeps the kernel for wg-s2s* interfaces that are not
// referenced by the current config and deletes them. This protects the next
// bring-up from inheriting stale state left behind by a crash, a config
// rewrite, or a tunnel that was removed while the manager was offline (BUG-L12).
func (m *TunnelManager) removeOrphanInterfaces() {
	if m.listIfaces == nil {
		return
	}
	configured := make(map[string]bool, len(m.config.Tunnels))
	for _, cfg := range m.config.Tunnels {
		configured[cfg.InterfaceName] = true
	}
	for _, ifc := range m.listIfaces("wg-s2s") {
		if configured[ifc.name] {
			continue
		}
		if err := m.deleteLink(ifc.idx); err != nil {
			m.log.Warn("RestoreAll: failed to remove orphan interface",
				"iface", ifc.name, "err", err)
			continue
		}
		m.log.Info("RestoreAll: removed orphan interface", "iface", ifc.name)
	}
}

func (m *TunnelManager) GetTunnels() []TunnelConfig {
	m.mu.Lock()
	defer m.mu.Unlock()

	result := make([]TunnelConfig, len(m.config.Tunnels))
	copy(result, m.config.Tunnels)
	return result
}

func (m *TunnelManager) GetStatuses() []WgS2sStatus {
	m.mu.Lock()
	tunnels := make([]TunnelConfig, len(m.config.Tunnels))
	copy(tunnels, m.config.Tunnels)
	wgClient := m.wgClient
	log := m.log
	m.mu.Unlock()

	return getAllStatuses(wgClient, tunnels, log)
}

func (m *TunnelManager) GetPublicKey(id string) (string, error) {
	pubPath := filepath.Join(m.configDir, id+".pub")
	data, err := os.ReadFile(pubPath)
	if err != nil {
		return "", fmt.Errorf("read public key: %w", err)
	}
	return strings.TrimSpace(string(data)), nil
}

func (m *TunnelManager) cleanupExistingInterface(cfg TunnelConfig) error {
	idx, ok := m.lookupIface(cfg.InterfaceName)
	if !ok {
		return nil
	}
	m.releaseRoutes(cfg.ID, idx, cfg.AllowedIPs, effectiveMetric(cfg.RouteMetric))
	if err := m.deleteLink(idx); err != nil {
		m.log.Warn("delete existing interface failed, reconnecting rtnetlink",
			"iface", cfg.InterfaceName, "err", err)
		if err := m.reconnectRtnetlink(); err != nil {
			return fmt.Errorf("reconnect rtnetlink: %w", err)
		}
		if idx2, ok2 := m.lookupIface(cfg.InterfaceName); ok2 {
			if err := m.deleteLink(idx2); err != nil {
				return fmt.Errorf("delete interface %s after reconnect: %w", cfg.InterfaceName, err)
			}
		}
	}
	return nil
}

func (m *TunnelManager) bringUp(cfg TunnelConfig, privKey wgtypes.Key) error {
	if err := m.cleanupExistingInterface(cfg); err != nil {
		return err
	}

	ifIndex, err := createInterface(m.rtConn, cfg.InterfaceName)
	if err != nil {
		return err
	}

	cleanup := func() {
		if delErr := m.deleteLink(ifIndex); delErr != nil {
			m.log.Warn("cleanup: deleteInterface failed", "iface", cfg.InterfaceName, "err", delErr)
		}
	}

	if cfg.MTU != 0 {
		if err := setMTU(m.rtConn, ifIndex, uint32(cfg.MTU)); err != nil {
			cleanup()
			return fmt.Errorf("set MTU: %w", err)
		}
	}

	peer := &peerConfig{
		PublicKey:           cfg.PeerPublicKey,
		Endpoint:            cfg.PeerEndpoint,
		AllowedIPs:          cfg.AllowedIPs,
		PersistentKeepalive: cfg.PersistentKeepalive,
	}
	if err := configureDevice(m.wgClient, cfg.InterfaceName, privKey, cfg.ListenPort, peer); err != nil {
		cleanup()
		return err
	}

	if err := addAddress(m.rtConn, ifIndex, cfg.TunnelAddress); err != nil {
		cleanup()
		return fmt.Errorf("add address: %w", err)
	}

	if err := setInterfaceUp(m.rtConn, ifIndex); err != nil {
		cleanup()
		return fmt.Errorf("set interface up: %w", err)
	}

	if len(cfg.AllowedIPs) > 0 {
		if err := m.claimRoutes(cfg.ID, ifIndex, cfg.AllowedIPs, effectiveMetric(cfg.RouteMetric)); err != nil {
			cleanup()
			return err
		}
	}

	return nil
}

func (m *TunnelManager) reconnectRtnetlink() error {
	newConn, err := rtnetlink.Dial(nil)
	if err != nil {
		return fmt.Errorf("dial rtnetlink: %w", err)
	}
	_ = m.rtConn.Close()
	m.rtConn = newConn
	m.routes = &rtnetlinkRoutes{conn: newConn}
	m.deleteLink = func(idx uint32) error { return deleteInterface(newConn, idx) }
	return nil
}

func (m *TunnelManager) claimRoutes(tunnelID string, ifIndex uint32, cidrs []string, metric int) error {
	var registered []string
	for _, cidr := range cidrs {
		firstOwner := m.routeRefs.add(cidr, tunnelID, ifIndex, metric)
		registered = append(registered, cidr)

		msg, err := buildRouteMessage(cidr, ifIndex, metric)
		if err != nil {
			m.unregisterRoutes(tunnelID, registered, metric)
			return err
		}

		if !firstOwner {
			// Defensive: kernel route may have been removed by a stale cleanup
			// or an external actor (see BUG-H4). Re-assert; ignore EEXIST.
			if err := m.routes.Add(msg); err != nil && !errors.Is(err, unix.EEXIST) {
				m.log.Warn("route re-assert on shared owner failed",
					"cidr", cidr, "tunnel", tunnelID, "err", err)
			}
			continue
		}

		if err := m.routes.Add(msg); err != nil {
			if errors.Is(err, unix.EEXIST) {
				m.log.Debug("route already exists in kernel, skipping", "cidr", cidr)
				continue
			}
			m.unregisterRoutes(tunnelID, registered, metric)
			return fmt.Errorf("add route %s: %w", cidr, err)
		}
	}
	return nil
}

func (m *TunnelManager) releaseRoutes(tunnelID string, ifIndex uint32, cidrs []string, metric int) {
	for _, cidr := range cidrs {
		remaining := m.routeRefs.remove(cidr, tunnelID, metric)
		if len(remaining) == 0 {
			msg, err := buildRouteMessage(cidr, ifIndex, metric)
			if err != nil {
				m.log.Warn("releaseRoutes: invalid CIDR", "cidr", cidr, "err", err)
				continue
			}
			if err := m.routes.Delete(msg); err != nil && !errors.Is(err, unix.ESRCH) {
				m.log.Warn("releaseRoutes: delete failed", "cidr", cidr, "err", err)
			}
		} else {
			msg, err := buildRouteMessage(cidr, remaining[0].ifIndex, metric)
			if err != nil {
				m.log.Warn("releaseRoutes: invalid CIDR for replace", "cidr", cidr, "err", err)
				continue
			}
			if err := m.routes.Replace(msg); err != nil {
				m.log.Warn("releaseRoutes: replace to surviving tunnel failed", "cidr", cidr,
					"survivingTunnel", remaining[0].tunnelID, "err", err)
			}
		}
	}
}

func (m *TunnelManager) unregisterRoutes(tunnelID string, cidrs []string, metric int) {
	for _, cidr := range cidrs {
		m.routeRefs.remove(cidr, tunnelID, metric)
	}
}

// tearDown removes the kernel interface and releases its routes. It is
// idempotent: a missing interface is not an error. A deleteLink failure
// IS an error — callers (Disable/Delete sagas) must abort before
// persisting "off" state to disk.
func (m *TunnelManager) tearDown(cfg TunnelConfig) error {
	idx, ok := m.lookupIface(cfg.InterfaceName)
	if !ok {
		return nil
	}
	m.releaseRoutes(cfg.ID, idx, cfg.AllowedIPs, effectiveMetric(cfg.RouteMetric))
	if err := m.deleteLink(idx); err != nil {
		return fmt.Errorf("deleteInterface %s: %w", cfg.InterfaceName, err)
	}
	return nil
}

func (m *TunnelManager) recreateTunnel(cfg *TunnelConfig, teardown bool) error {
	// Preflight: ensure the private key is loadable before we tear down the
	// kernel interface (otherwise we couldn't bring it back up). Skip when
	// a test seam supplants the real bring-up.
	if m.bringUpForTest == nil {
		if _, err := loadPrivateKey(m.configDir, cfg.ID); err != nil {
			return fmt.Errorf("preflight: %w", err)
		}
	}

	if teardown {
		if err := m.tearDown(*cfg); err != nil {
			return fmt.Errorf("tearDown before recreate: %w", err)
		}
	}

	if err := m.bringUpTunnel(*cfg); err != nil {
		cfg.Enabled = false
		return fmt.Errorf("bringUp failed (tunnel disabled): %w", err)
	}

	cfg.Enabled = true
	return nil
}

func (m *TunnelManager) findTunnel(id string) int {
	for i, t := range m.config.Tunnels {
		if t.ID == id {
			return i
		}
	}
	return -1
}

func (m *TunnelManager) save() error {
	if m.saveOverride != nil {
		return m.saveOverride()
	}
	return saveConfig(filepath.Join(m.configDir, configFileName), m.config)
}

// checkPortAvailable verifies the UDP port is free for both v4 and v6
// families. BUG-L11: probing only udp4 missed peers bound on [::]:port
// with IPV6_V6ONLY=1, so manager started a second tunnel on a port that
// wireguard-go could not actually claim end-to-end.
func checkPortAvailable(port int) error {
	v4, err := net.ListenPacket("udp4", fmt.Sprintf(":%d", port))
	if err != nil {
		return fmt.Errorf("port in use or unavailable (udp4): %w", err)
	}
	_ = v4.Close()
	v6, err := net.ListenPacket("udp6", fmt.Sprintf("[::]:%d", port))
	if err != nil {
		return fmt.Errorf("port in use or unavailable (udp6): %w", err)
	}
	_ = v6.Close()
	return nil
}
