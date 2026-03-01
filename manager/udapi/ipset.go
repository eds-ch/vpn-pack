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

func findIPSet(c *UDAPIClient, ipsetName string) (*ipsetEntry, []ipsetEntry, error) {
	resp, err := c.Request("GET", "/firewall/sets", nil)
	if err != nil {
		return nil, nil, fmt.Errorf("get firewall sets: %w", err)
	}

	var sets []ipsetEntry
	if err := json.Unmarshal(resp.Response, &sets); err != nil {
		return nil, nil, fmt.Errorf("parse firewall sets: %w", err)
	}

	for i := range sets {
		if sets[i].Identification.Name == ipsetName {
			return &sets[i], sets, nil
		}
	}
	return nil, sets, nil
}

func EnsureZoneSubnet(c *UDAPIClient, ipsetName, cidr string) error {
	target, _, err := findIPSet(c, ipsetName)
	if err != nil {
		return err
	}
	if target == nil {
		return fmt.Errorf("%s ipset not found", ipsetName)
	}

	for _, entry := range target.Entries {
		if entry == cidr {
			return nil
		}
	}

	target.Entries = append(target.Entries, cidr)

	_, err = c.Request("PUT", "/firewall/sets/set", target)
	if err != nil {
		return fmt.Errorf("update %s: %w", ipsetName, err)
	}
	return nil
}

func EnsureVPNSubnet(c *UDAPIClient, cidr string) error {
	return EnsureZoneSubnet(c, "VPN_subnets", cidr)
}

func RemoveZoneSubnet(c *UDAPIClient, ipsetName, cidr string) error {
	target, _, err := findIPSet(c, ipsetName)
	if err != nil {
		return err
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
		return fmt.Errorf("update %s: %w", ipsetName, err)
	}
	return nil
}

func RemoveVPNSubnet(c *UDAPIClient, cidr string) error {
	return RemoveZoneSubnet(c, "VPN_subnets", cidr)
}
