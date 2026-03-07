package udapi

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strings"

	"unifi-tailscale/manager/config"
)

type netCfg struct {
	Interfaces []netCfgInterface `json:"interfaces"`
}

type netCfgInterface struct {
	Identification struct {
		Type string `json:"type"`
		ID   string `json:"id"`
	} `json:"identification"`
	Addresses []netCfgAddress `json:"addresses"`
	Status    struct {
		Comment string `json:"comment"`
	} `json:"status"`
}

type netCfgAddress struct {
	Type    string `json:"type"`
	Address string `json:"address"`
	CIDR    string `json:"cidr"`
	Version string `json:"version"`
}

type SubnetInfo struct {
	CIDR string `json:"cidr"`
	Name string `json:"name"`
	Type string `json:"type"`
}

func loadNetCfg() (*netCfg, error) {
	data, err := os.ReadFile(config.UDAPIConfigPath)
	if err != nil {
		return nil, err
	}
	var cfg netCfg
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func GetWanIP() string {
	cfg, err := loadNetCfg()
	if err != nil {
		return ""
	}

	var wanIfnames []string
	for _, iface := range cfg.Interfaces {
		if strings.HasPrefix(strings.ToUpper(iface.Status.Comment), "WAN") {
			wanIfnames = append(wanIfnames, iface.Identification.ID)
		}
	}

	var publicIPs, privateIPs []string
	for _, name := range wanIfnames {
		iface, err := net.InterfaceByName(name)
		if err != nil {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			ipNet, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ip4 := ipNet.IP.To4()
			if ip4 == nil {
				continue
			}
			s := ip4.String()
			if isPrivateIP(ip4) {
				privateIPs = append(privateIPs, s)
			} else {
				publicIPs = append(publicIPs, s)
			}
		}
	}

	if len(publicIPs) > 0 {
		return publicIPs[0]
	}
	if len(privateIPs) > 0 {
		return privateIPs[0]
	}
	return ""
}

func isPrivateIP(ip net.IP) bool {
	rfc1918 := []net.IPNet{
		{IP: net.IP{10, 0, 0, 0}, Mask: net.CIDRMask(8, 32)},
		{IP: net.IP{172, 16, 0, 0}, Mask: net.CIDRMask(12, 32)},
		{IP: net.IP{192, 168, 0, 0}, Mask: net.CIDRMask(16, 32)},
		{IP: net.IP{100, 64, 0, 0}, Mask: net.CIDRMask(10, 32)},
	}
	for _, n := range rfc1918 {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

func ParseLocalSubnets() []SubnetInfo {
	cfg, err := loadNetCfg()
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
