package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

type DeviceInfo struct {
	Hostname         string   `json:"hostname"`
	Model            string   `json:"model"`
	ModelShort       string   `json:"modelShort"`
	Firmware         string   `json:"firmware"`
	UniFiVersion     string   `json:"unifiVersion"`
	PackageVersion   string   `json:"packageVersion"`
	TailscaleVersion string   `json:"tailscaleVersion"`
	HasTUN           bool     `json:"hasTUN"`
	HasUDAPISocket   bool     `json:"hasUDAPISocket"`
	PersistentFree   int64    `json:"persistentFree"`
	ActiveVPNClients []string `json:"activeVPNClients"`
}

func detectDevice() DeviceInfo {
	info := DeviceInfo{}

	info.Hostname, _ = os.Hostname()
	info.Model = cmdOutput(deviceInfoCmd, "model")
	if info.Model == "" {
		info.Model = readFileString("/sys/firmware/devicetree/base/model")
	}
	info.ModelShort = cmdOutput(deviceInfoCmd, "model_short")
	info.Firmware = cmdOutput(deviceInfoCmd, "firmware")
	info.UniFiVersion = cmdOutput("dpkg-query", "-W", "-f=${Version}", "unifi")
	if info.UniFiVersion == "" {
		info.UniFiVersion = cmdOutput("dpkg-query", "-W", "-f=${Version}", "unifi-native")
	}
	info.PackageVersion = version
	info.TailscaleVersion = tailscaleVersion

	if _, err := os.Stat("/dev/net/tun"); err == nil {
		info.HasTUN = true
	}
	if _, err := os.Stat(udapiSocketPath); err == nil {
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
		if strings.HasPrefix(l.IfName, vpnClientPrefix) {
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

func readFileString(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimRight(string(data), "\x00\n")
}

const minNetworkMajor = 10
const minNetworkMinor = 1

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
