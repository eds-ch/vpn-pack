package udapi

import (
	"context"
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

func findIPSet(ctx context.Context, c *UDAPIClient, ipsetName string) (*ipsetEntry, []ipsetEntry, error) {
	resp, err := c.RequestCtx(ctx, "GET", "/firewall/sets", nil)
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

func EnsureZoneSubnet(ctx context.Context, c *UDAPIClient, ipsetName, cidr string) error {
	return WithIpsetRMW(ipsetName, func() error {
		target, _, err := findIPSet(ctx, c, ipsetName)
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

		_, err = c.RequestCtx(ctx, "PUT", "/firewall/sets/set", target)
		if err != nil {
			return fmt.Errorf("update %s: %w", ipsetName, err)
		}
		return nil
	})
}

func EnsureZoneSubnets(ctx context.Context, c *UDAPIClient, ipsetName string, cidrs []string) error {
	if len(cidrs) == 0 {
		return nil
	}
	return WithIpsetRMW(ipsetName, func() error {
		target, _, err := findIPSet(ctx, c, ipsetName)
		if err != nil {
			return err
		}
		if target == nil {
			return fmt.Errorf("%s ipset not found", ipsetName)
		}

		existing := make(map[string]bool, len(target.Entries))
		for _, e := range target.Entries {
			existing[e] = true
		}

		var added bool
		for _, cidr := range cidrs {
			if !existing[cidr] {
				target.Entries = append(target.Entries, cidr)
				added = true
			}
		}
		if !added {
			return nil
		}

		_, err = c.RequestCtx(ctx, "PUT", "/firewall/sets/set", target)
		if err != nil {
			return fmt.Errorf("update %s: %w", ipsetName, err)
		}
		return nil
	})
}

func EnsureVPNSubnet(ctx context.Context, c *UDAPIClient, cidr string) error {
	return EnsureZoneSubnet(ctx, c, "VPN_subnets", cidr)
}

func RemoveZoneSubnet(ctx context.Context, c *UDAPIClient, ipsetName, cidr string) error {
	return WithIpsetRMW(ipsetName, func() error {
		target, _, err := findIPSet(ctx, c, ipsetName)
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
		_, err = c.RequestCtx(ctx, "PUT", "/firewall/sets/set", target)
		if err != nil {
			return fmt.Errorf("update %s: %w", ipsetName, err)
		}
		return nil
	})
}

func RemoveVPNSubnet(ctx context.Context, c *UDAPIClient, cidr string) error {
	return RemoveZoneSubnet(ctx, c, "VPN_subnets", cidr)
}
