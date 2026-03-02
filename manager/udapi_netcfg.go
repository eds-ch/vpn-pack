package main

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
)

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

type subnetInfo struct {
	CIDR string `json:"cidr"`
	Name string `json:"name"`
	Type string `json:"type"`
}

func loadUDAPINetCfg() (*udapiNetCfg, error) {
	data, err := os.ReadFile(udapiConfigPath)
	if err != nil {
		return nil, err
	}
	var cfg udapiNetCfg
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func getWanIP() string {
	cfg, err := loadUDAPINetCfg()
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

func parseLocalSubnets() []subnetInfo {
	cfg, err := loadUDAPINetCfg()
	if err != nil {
		return nil
	}
	var subnets []subnetInfo
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
			subnets = append(subnets, subnetInfo{
				CIDR: ipNet.String(),
				Name: fmt.Sprintf("%s (%s)", name, iface.Identification.ID),
				Type: ifType,
			})
		}
	}
	return subnets
}
