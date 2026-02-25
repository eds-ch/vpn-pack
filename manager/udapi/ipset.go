package udapi

import (
	"encoding/json"
	"fmt"
)

type ipsetEntry struct {
	Identification struct {
		Type string `json:"type"`
		Name string `json:"name"`
	} `json:"identification"`
	Entries []string `json:"entries"`
}

func EnsureVPNSubnet(c *UDAPIClient, cidr string) error {
	resp, err := c.Request("GET", "/firewall/sets", nil)
	if err != nil {
		return fmt.Errorf("get firewall sets: %w", err)
	}

	var sets []ipsetEntry
	if err := json.Unmarshal(resp.Response, &sets); err != nil {
		return fmt.Errorf("parse firewall sets: %w", err)
	}

	var target *ipsetEntry
	for i := range sets {
		if sets[i].Identification.Name == "VPN_subnets" {
			target = &sets[i]
			break
		}
	}

	if target == nil {
		return fmt.Errorf("VPN_subnets ipset not found")
	}

	for _, entry := range target.Entries {
		if entry == cidr {
			return nil
		}
	}

	target.Entries = append(target.Entries, cidr)

	_, err = c.Request("PUT", "/firewall/sets/set", target)
	if err != nil {
		return fmt.Errorf("update VPN_subnets: %w", err)
	}
	return nil
}

func RemoveVPNSubnet(c *UDAPIClient, cidr string) error {
	resp, err := c.Request("GET", "/firewall/sets", nil)
	if err != nil {
		return fmt.Errorf("get firewall sets: %w", err)
	}

	var sets []ipsetEntry
	if err := json.Unmarshal(resp.Response, &sets); err != nil {
		return fmt.Errorf("parse firewall sets: %w", err)
	}

	var target *ipsetEntry
	for i := range sets {
		if sets[i].Identification.Name == "VPN_subnets" {
			target = &sets[i]
			break
		}
	}

	if target == nil {
		return nil
	}

	filtered := make([]string, 0, len(target.Entries))
	for _, entry := range target.Entries {
		if entry != cidr {
			filtered = append(filtered, entry)
		}
	}
	if len(filtered) == len(target.Entries) {
		return nil
	}

	target.Entries = filtered
	_, err = c.Request("PUT", "/firewall/sets/set", target)
	if err != nil {
		return fmt.Errorf("update VPN_subnets: %w", err)
	}
	return nil
}
