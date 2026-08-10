package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"

	"unifi-tailscale/manager/config"
)

func detectDevice() DeviceInfo {
	info := DeviceInfo{}

	info.Hostname, _ = os.Hostname()
	info.Model = cmdOutput(config.DeviceInfoCmd, "model")
	if info.Model == "" {
		info.Model = readFileTrimmed("/sys/firmware/devicetree/base/model")
	}
	info.ModelShort = cmdOutput(config.DeviceInfoCmd, "model_short")
	info.Firmware = cmdOutput(config.DeviceInfoCmd, "firmware")
	info.UniFiVersion = readUniFiVersion()
	info.PackageVersion = config.Version
	info.TailscaleVersion = config.TailscaleVersion

	if _, err := os.Stat("/dev/net/tun"); err == nil {
		info.HasTUN = true
	}
	if _, err := os.Stat(config.UDAPISocketPath); err == nil {
		info.HasUDAPISocket = true
	}

	var stat syscall.Statfs_t
	if err := syscall.Statfs("/persistent/", &stat); err == nil {
		info.PersistentFree = int64(stat.Bavail) * int64(stat.Bsize)
	}

	info.ActiveVPNClients = detectVPNClients()

	return info
}

func detectVPNClients() []string {
	out, err := exec.Command("ip", "-j", "link", "show", "type", "wireguard").Output()
	if err != nil {
		return nil
	}

	var links []struct {
		IfName string `json:"ifname"`
	}
	if err := json.Unmarshal(out, &links); err != nil {
		return nil
	}

	var clients []string
	for _, l := range links {
		if strings.HasPrefix(l.IfName, config.VPNClientPrefix) {
			clients = append(clients, l.IfName)
		}
	}
	return clients
}

func (s *Server) refreshVPNClients() {
	clients := detectVPNClients()
	s.vpnClientsMu.Lock()
	s.deviceInfo.ActiveVPNClients = clients
	s.vpnClientsMu.Unlock()
}

func cmdOutput(name string, args ...string) string {
	out, err := exec.Command(name, args...).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func readFileTrimmed(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(strings.TrimRight(string(data), "\x00"))
}

// UniFi OS applies package upgrades after the manager is already running, so
// the value read at startup goes stale on a firmware update. Callers that
// render it re-read instead of trusting the boot-time snapshot.
func readUniFiVersion() string {
	if v := cmdOutput("dpkg-query", "-W", "-f=${Version}", "unifi"); v != "" {
		return v
	}
	return cmdOutput("dpkg-query", "-W", "-f=${Version}", "unifi-native")
}

const minNetworkMajor = 10
const minNetworkMinor = 1

// UniFi OS finishes applying package upgrades after the manager is already
// running, so the version read at startup can be the pre-upgrade one. Failing
// the gate on it is not recoverable — the exit-78 verdict is made permanent by
// RestartPreventExitStatus — so a failed check is re-read for a bounded window
// while the system is still early in its boot. Past that window the answer
// cannot change under us, and an unsupported device fails immediately instead
// of stalling every socket activation.
const (
	unifiGateBootWindow = 10 * time.Minute
	unifiGateInterval   = 10 * time.Second
	unifiGateAttempts   = 18 // 3 minutes; the observed gap was ~2
)

type unifiGateDeps struct {
	read   func() string
	uptime func() time.Duration
	sleep  func(time.Duration)
}

func defaultUniFiGateDeps() unifiGateDeps {
	return unifiGateDeps{
		read:   readUniFiVersion,
		uptime: systemUptime,
		sleep:  time.Sleep,
	}
}

// awaitSupportedUniFiVersion returns the version that satisfied the gate.
func awaitSupportedUniFiVersion(raw string, d unifiGateDeps) (string, error) {
	err := checkMinUniFiVersion(raw)
	if err == nil {
		return raw, nil
	}
	if d.uptime() > unifiGateBootWindow {
		return raw, err
	}

	slog.Warn("UniFi Network version not acceptable yet; the system is still early in its boot, re-reading before giving up",
		"found", raw, "retryFor", time.Duration(unifiGateAttempts)*unifiGateInterval)

	for range unifiGateAttempts {
		d.sleep(unifiGateInterval)
		raw = d.read()
		if err = checkMinUniFiVersion(raw); err == nil {
			slog.Info("UniFi Network version became acceptable", "version", raw)
			return raw, nil
		}
	}
	return raw, err
}

func systemUptime() time.Duration {
	var si syscall.Sysinfo_t
	if err := syscall.Sysinfo(&si); err != nil {
		// Unknown uptime: assume we are past the boot window so an
		// unsupported device still fails fast.
		return unifiGateBootWindow + time.Second
	}
	return time.Duration(si.Uptime) * time.Second
}

type uniFiVersion struct {
	Major int
	Minor int
}

func (v uniFiVersion) String() string {
	return fmt.Sprintf("%d.%d", v.Major, v.Minor)
}

func parseUniFiVersion(raw string) (uniFiVersion, error) {
	if raw == "" {
		return uniFiVersion{}, fmt.Errorf("empty version string")
	}
	if i := strings.IndexByte(raw, ':'); i >= 0 {
		if _, err := strconv.Atoi(raw[:i]); err == nil {
			raw = raw[i+1:]
		}
	}
	parts := strings.SplitN(raw, ".", 3)
	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return uniFiVersion{}, fmt.Errorf("invalid major version %q: %w", parts[0], err)
	}
	minor := 0
	if len(parts) >= 2 {
		minor, err = strconv.Atoi(parts[1])
		if err != nil {
			return uniFiVersion{}, fmt.Errorf("invalid minor version %q: %w", parts[1], err)
		}
	}
	return uniFiVersion{Major: major, Minor: minor}, nil
}

func checkMinUniFiVersion(raw string) error {
	if raw == "" {
		return fmt.Errorf("UniFi Network Application not found. A working UniFi Network 10.1+ installation is required")
	}
	v, err := parseUniFiVersion(raw)
	if err != nil {
		return fmt.Errorf("UniFi Network version unreadable (%q): %w", raw, err)
	}
	if v.Major > minNetworkMajor || (v.Major == minNetworkMajor && v.Minor >= minNetworkMinor) {
		return nil
	}
	return fmt.Errorf("UniFi Network %d.%d or later is required (found: %s). Please update via Settings > System > Updates in the UniFi console", minNetworkMajor, minNetworkMinor, v)
}
