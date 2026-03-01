package wgs2s

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/jsimonetti/rtnetlink"
	"golang.zx2c4.com/wireguard/wgctrl"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

const (
	errFmtTunnelNotFound = "tunnel %s not found"
	configFileName       = "tunnels.json"
)

type TunnelManager struct {
	mu        sync.Mutex
	config    *TunnelsConfig
	configDir string
	wgClient  *wgctrl.Client
	rtConn    *rtnetlink.Conn
	log       *slog.Logger
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
		config:    cfg,
		configDir: configDir,
		wgClient:  wgClient,
		rtConn:    rtConn,
		log:       log,
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

	cfg.ID = generateID()
	cfg.InterfaceName = nextInterfaceName(m.config.Tunnels)
	cfg.CreatedAt = time.Now()
	cfg.Enabled = true

	if cfg.PersistentKeepalive == 0 {
		cfg.PersistentKeepalive = 25
	}
	if cfg.MTU == 0 {
		cfg.MTU = 1420
	}

	var privKey wgtypes.Key
	var err error
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
		m.tearDown(cfg)
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
	if cfg.Enabled {
		m.tearDown(cfg)
	}
	deleteKeyFiles(m.configDir, cfg.ID)

	m.config.Tunnels = append(m.config.Tunnels[:idx], m.config.Tunnels[idx+1:]...)
	if err := m.save(); err != nil {
		return err
	}

	m.log.Info("tunnel deleted", "id", id, "name", cfg.Name)
	return nil
}

func (m *TunnelManager) EnableTunnel(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	idx := m.findTunnel(id)
	if idx < 0 {
		return fmt.Errorf(errFmtTunnelNotFound, id)
	}

	cfg := &m.config.Tunnels[idx]
	if cfg.Enabled {
		return nil
	}

	privKey, err := loadPrivateKey(m.configDir, cfg.ID)
	if err != nil {
		return err
	}

	if err := m.bringUp(*cfg, privKey); err != nil {
		return err
	}

	cfg.Enabled = true
	return m.save()
}

func (m *TunnelManager) DisableTunnel(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	idx := m.findTunnel(id)
	if idx < 0 {
		return fmt.Errorf(errFmtTunnelNotFound, id)
	}

	cfg := &m.config.Tunnels[idx]
	if !cfg.Enabled {
		return nil
	}

	m.tearDown(*cfg)
	cfg.Enabled = false
	return m.save()
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

	merged := *cfg
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
	if updates.PersistentKeepalive != 0 {
		merged.PersistentKeepalive = updates.PersistentKeepalive
	}
	if updates.MTU != 0 {
		merged.MTU = updates.MTU
	}

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

	if wasEnabled && recreate {
		if _, err := loadPrivateKey(m.configDir, cfg.ID); err != nil {
			return nil, fmt.Errorf("preflight: %w", err)
		}
		m.tearDown(*cfg)
	}

	*cfg = merged

	if wasEnabled && recreate {
		privKey, err := loadPrivateKey(m.configDir, cfg.ID)
		if err != nil {
			cfg.Enabled = false
			if saveErr := m.save(); saveErr != nil {
				m.log.Warn("failed to save config after disabling tunnel", "id", cfg.ID, "err", saveErr)
			}
			return nil, err
		}
		if err := m.bringUp(*cfg, privKey); err != nil {
			cfg.Enabled = false
			if saveErr := m.save(); saveErr != nil {
				m.log.Warn("failed to save config after disabling tunnel", "id", cfg.ID, "err", saveErr)
			}
			return nil, fmt.Errorf("bringUp after update failed (tunnel disabled): %w", err)
		}
		cfg.Enabled = true
	} else if wasEnabled && !recreate {
		if err := m.hotUpdate(*cfg, oldAllowedIPs); err != nil {
			m.log.Warn("hot update failed, falling back to recreate", "id", cfg.ID, "err", err)
			m.tearDown(*cfg)
			privKey, err := loadPrivateKey(m.configDir, cfg.ID)
			if err != nil {
				cfg.Enabled = false
				if saveErr := m.save(); saveErr != nil {
					m.log.Warn("failed to save config after disabling tunnel", "id", cfg.ID, "err", saveErr)
				}
				return nil, err
			}
			if err := m.bringUp(*cfg, privKey); err != nil {
				cfg.Enabled = false
				if saveErr := m.save(); saveErr != nil {
					m.log.Warn("failed to save config after disabling tunnel", "id", cfg.ID, "err", saveErr)
				}
				return nil, fmt.Errorf("bringUp after hot-update fallback failed (tunnel disabled): %w", err)
			}
			cfg.Enabled = true
		}
	}

	if err := m.save(); err != nil {
		return nil, err
	}

	result := *cfg
	return &result, nil
}

func (m *TunnelManager) hotUpdate(cfg TunnelConfig, oldAllowedIPs []string) error {
	peer := &peerConfig{
		PublicKey:           cfg.PeerPublicKey,
		Endpoint:            cfg.PeerEndpoint,
		AllowedIPs:          cfg.AllowedIPs,
		PersistentKeepalive: cfg.PersistentKeepalive,
	}
	if err := updatePeer(m.wgClient, cfg.InterfaceName, peer); err != nil {
		return err
	}

	ifIndex, ok := getInterfaceIndex(cfg.InterfaceName)
	if !ok {
		return fmt.Errorf("interface %s not found", cfg.InterfaceName)
	}

	if !slicesEqual(oldAllowedIPs, cfg.AllowedIPs) {
		if err := deleteRoutes(m.rtConn, ifIndex, oldAllowedIPs); err != nil {
			m.log.Warn("hot update: deleteRoutes failed", "iface", cfg.InterfaceName, "err", err)
		}
		if err := addRoutes(m.rtConn, ifIndex, cfg.AllowedIPs, m.log); err != nil {
			return fmt.Errorf("hot update: addRoutes: %w", err)
		}
	}

	m.log.Info("tunnel hot-updated", "id", cfg.ID, "name", cfg.Name)
	return nil
}

func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func (m *TunnelManager) RestoreAll() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var lastErr error
	for _, cfg := range m.config.Tunnels {
		if !cfg.Enabled {
			continue
		}

		privKey, err := loadPrivateKey(m.configDir, cfg.ID)
		if err != nil {
			m.log.Warn("restore: failed to load key", "id", cfg.ID, "err", err)
			lastErr = err
			continue
		}

		if err := m.bringUp(cfg, privKey); err != nil {
			m.log.Warn("restore: failed to bring up tunnel", "id", cfg.ID, "err", err)
			lastErr = err
			continue
		}

		m.log.Info("tunnel restored", "id", cfg.ID, "name", cfg.Name, "iface", cfg.InterfaceName)
	}
	return lastErr
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
	defer m.mu.Unlock()

	return getAllStatuses(m.wgClient, m.config.Tunnels, m.log)
}

func (m *TunnelManager) GetPublicKey(id string) (string, error) {
	pubPath := filepath.Join(m.configDir, id+".pub")
	data, err := os.ReadFile(pubPath)
	if err != nil {
		return "", fmt.Errorf("read public key: %w", err)
	}
	return strings.TrimSpace(string(data)), nil
}

const udapiNetCfgPath = "/data/udapi-config/udapi-net-cfg.json"

type udapiNetCfg struct {
	Interfaces []udapiInterface `json:"interfaces"`
}

type udapiInterface struct {
	Identification struct {
		Type string `json:"type"`
		ID   string `json:"id"`
	} `json:"identification"`
	Addresses []udapiAddress `json:"addresses"`
	Status    struct {
		Comment string `json:"comment"`
	} `json:"status"`
}

type udapiAddress struct {
	Type    string `json:"type"`
	Address string `json:"address"`
	CIDR    string `json:"cidr"`
	Version string `json:"version"`
}

func loadUDAPICfg() (*udapiNetCfg, error) {
	data, err := os.ReadFile(udapiNetCfgPath)
	if err != nil {
		return nil, err
	}
	var cfg udapiNetCfg
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (m *TunnelManager) GetWanIP() string {
	cfg, err := loadUDAPICfg()
	if err != nil {
		return ""
	}
	for _, iface := range cfg.Interfaces {
		if iface.Identification.Type == "wan" {
			for _, addr := range iface.Addresses {
				if addr.Type == "dhcp" || addr.Type == "static" {
					return addr.Address
				}
			}
		}
	}
	return ""
}

type SubnetInfo struct {
	CIDR string `json:"cidr"`
	Name string `json:"name"`
	Type string `json:"type"`
}

func ParseLocalSubnets() []SubnetInfo {
	cfg, err := loadUDAPICfg()
	if err != nil {
		return nil
	}
	var subnets []SubnetInfo
	for _, iface := range cfg.Interfaces {
		ifType := iface.Identification.Type
		if ifType != "bridge" && ifType != "vlan" {
			continue
		}
		for _, addr := range iface.Addresses {
			if addr.Version != "v4" || addr.Type != "static" {
				continue
			}
			_, ipNet, err := net.ParseCIDR(addr.CIDR)
			if err != nil {
				continue
			}
			name := iface.Status.Comment
			if name == "" {
				name = iface.Identification.ID
			}
			subnets = append(subnets, SubnetInfo{
				CIDR: ipNet.String(),
				Name: fmt.Sprintf("%s (%s)", name, iface.Identification.ID),
				Type: ifType,
			})
		}
	}
	return subnets
}

func (m *TunnelManager) cleanupExistingInterface(cfg TunnelConfig) error {
	idx, ok := getInterfaceIndex(cfg.InterfaceName)
	if !ok {
		return nil
	}
	if err := deleteRoutes(m.rtConn, idx, cfg.AllowedIPs); err != nil {
		m.log.Warn("cleanup: deleteRoutes failed", "iface", cfg.InterfaceName, "err", err)
	}
	if err := deleteInterface(m.rtConn, idx); err != nil {
		m.log.Warn("delete existing interface failed, reconnecting rtnetlink",
			"iface", cfg.InterfaceName, "err", err)
		if err := m.reconnectRtnetlink(); err != nil {
			return fmt.Errorf("reconnect rtnetlink: %w", err)
		}
		if idx2, ok2 := getInterfaceIndex(cfg.InterfaceName); ok2 {
			if err := deleteInterface(m.rtConn, idx2); err != nil {
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
		if delErr := deleteInterface(m.rtConn, ifIndex); delErr != nil {
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
		if err := addRoutes(m.rtConn, ifIndex, cfg.AllowedIPs, m.log); err != nil {
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
	return nil
}

func (m *TunnelManager) tearDown(cfg TunnelConfig) {
	idx, ok := getInterfaceIndex(cfg.InterfaceName)
	if !ok {
		return
	}
	if err := deleteRoutes(m.rtConn, idx, cfg.AllowedIPs); err != nil {
		m.log.Warn("tearDown: deleteRoutes failed", "iface", cfg.InterfaceName, "err", err)
	}
	if err := deleteInterface(m.rtConn, idx); err != nil {
		m.log.Warn("tearDown: deleteInterface failed", "iface", cfg.InterfaceName, "err", err)
	}
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
	return saveConfig(filepath.Join(m.configDir, configFileName), m.config)
}

func checkPortAvailable(port int) error {
	conn, err := net.ListenPacket("udp4", fmt.Sprintf(":%d", port))
	if err != nil {
		return fmt.Errorf("port in use or unavailable: %w", err)
	}
	_ = conn.Close()
	return nil
}
