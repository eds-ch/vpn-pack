package main

import (
	"encoding/json"
	"os"
	"os/exec"
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
