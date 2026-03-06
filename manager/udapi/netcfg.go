package udapi

import (
	"encoding/json"
	"fmt"
	"net"
	"os"

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
