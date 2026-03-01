package udapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

const firewallFilterBase = "/firewall/filter/"

func firewallFilterPath(chain string, parts ...string) string {
	p := firewallFilterBase + chain
	for _, s := range parts {
		p += "/" + s
	}
	return p
}

type FirewallRule struct {
	Chain     string
	Target    string
	Interface string
	Direction string
	Marker    string
	Desc      string
}

func AddRule(c *UDAPIClient, r FirewallRule) error {
	exists, err := HasMarkerRule(c, r.Chain, r.Marker)
	if err != nil {
		return fmt.Errorf("check %s: %w", r.Chain, err)
	}
	if exists {
		return nil
	}

	rule := map[string]any{
		"target":          r.Target,
		"description":     r.Desc,
		r.Direction:       map[string]string{"id": r.Interface},
		"ipVersion":       "both",
		"protocol":        "all",
		"connectionState": []string{},
	}

	_, err = c.Request("POST", firewallFilterPath(r.Chain, "rule"), rule)
	if err != nil {
		return fmt.Errorf("add %s rule: %w", r.Chain, err)
	}
	return nil
}

func AddInterfaceRulesForZone(c *UDAPIClient, iface, marker, chainPrefix string) error {
	var errs []error
	for _, r := range zoneRules(iface, marker, chainPrefix) {
		if err := AddRule(c, r); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func zoneRules(iface, marker, chainPrefix string) []FirewallRule {
	inTarget := chainPrefix + "_IN"
	localTarget := chainPrefix + "_LOCAL"
	outTarget := "LOCAL_" + chainPrefix

	return []FirewallRule{
		{Chain: "FORWARD_IN", Target: inTarget, Interface: iface, Direction: "inInterface", Marker: marker, Desc: fmt.Sprintf("%s %s (%s)", iface, inTarget, marker)},
		{Chain: "INPUT", Target: localTarget, Interface: iface, Direction: "inInterface", Marker: marker, Desc: fmt.Sprintf("%s %s (%s)", iface, localTarget, marker)},
		{Chain: "OUTPUT", Target: outTarget, Interface: iface, Direction: "outInterface", Marker: marker, Desc: fmt.Sprintf("%s %s (%s)", iface, outTarget, marker)},
	}
}

func RemoveInterfaceRules(c *UDAPIClient, iface, marker string) error {
	chains := []string{"FORWARD_IN", "INPUT", "OUTPUT"}
	var errs []error
	for _, chain := range chains {
		if err := removeMarkerRules(c, chain, marker); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func removeMarkerRules(c *UDAPIClient, chain, marker string) error {
	resp, err := c.Request("GET", firewallFilterPath(chain), nil)
	if err != nil {
		return fmt.Errorf("list %s rules: %w", chain, err)
	}

	var cr struct {
		Rules []struct {
			ID          int    `json:"id"`
			Description string `json:"description"`
		} `json:"rules"`
	}
	if err := json.Unmarshal(resp.Response, &cr); err != nil {
		return fmt.Errorf("parse %s rules: %w", chain, err)
	}

	for _, r := range cr.Rules {
		if strings.Contains(r.Description, marker) {
			if _, err := c.Request("DELETE", firewallFilterPath(chain, "rule"), map[string]any{"id": r.ID}); err != nil {
				return fmt.Errorf("delete %s rule %d: %w", chain, r.ID, err)
			}
		}
	}
	return nil
}

func HasMarkerRule(c *UDAPIClient, chain, marker string) (bool, error) {
	resp, err := c.Request("GET", firewallFilterPath(chain), nil)
	if err != nil {
		return false, err
	}

	var cr struct {
		Rules []struct {
			Description string `json:"description"`
		} `json:"rules"`
	}
	if err := json.Unmarshal(resp.Response, &cr); err != nil {
		return false, fmt.Errorf("parse %s response: %w", chain, err)
	}

	for _, r := range cr.Rules {
		if strings.Contains(r.Description, marker) {
			return true, nil
		}
	}
	return false, nil
}

