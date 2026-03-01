package main

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
)

var (
	ErrUnauthorized   = errors.New("integration API: unauthorized (invalid or missing API key)")
	ErrNotFound       = errors.New("integration API: resource not found")
	ErrIntegrationAPI = errors.New("integration API error")
)

type IntegrationClient struct {
	mu         sync.RWMutex
	apiKey     string
	baseURL    string
	httpClient *http.Client
}

type paginatedResponse struct {
	Data json.RawMessage `json:"data"`
}

type Zone struct {
	ID         string       `json:"id"`
	Name       string       `json:"name"`
	NetworkIDs []string     `json:"networkIds"`
}

type Policy struct {
	ID                string             `json:"id"`
	Enabled           bool               `json:"enabled"`
	Name              string             `json:"name"`
	Action            PolicyAction       `json:"action"`
	Source            PolicyEndpoint     `json:"source"`
	Destination       PolicyEndpoint     `json:"destination"`
	IPProtocolScope   IPProtocolScope    `json:"ipProtocolScope,omitempty"`
	LoggingEnabled    bool               `json:"loggingEnabled"`
}

type PolicyAction struct {
	Type               string `json:"type"`
	AllowReturnTraffic bool   `json:"allowReturnTraffic"`
}

type PolicyEndpoint struct {
	ZoneID        string         `json:"zoneId"`
	TrafficFilter *TrafficFilter `json:"trafficFilter,omitempty"`
}

type TrafficFilter struct {
	Type       string     `json:"type"`
	PortFilter PortFilter `json:"portFilter"`
}

type PortFilter struct {
	Type          string           `json:"type"`
	MatchOpposite bool             `json:"matchOpposite"`
	Items         []PortFilterItem `json:"items"`
}

type PortFilterItem struct {
	Type  string `json:"type"`
	Value int    `json:"value"`
}

type IPProtocolScope struct {
	IPVersion      string          `json:"ipVersion,omitempty"`
	ProtocolFilter *ProtocolFilter `json:"protocolFilter,omitempty"`
}

type ProtocolFilter struct {
	Type          string       `json:"type"`
	Protocol      ProtocolName `json:"protocol"`
	MatchOpposite bool         `json:"matchOpposite"`
}

type ProtocolName struct {
	Name string `json:"name"`
}

type SiteInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type AppInfo struct {
	ApplicationVersion string `json:"applicationVersion"`
}

func NewIntegrationClient(apiKey string) *IntegrationClient {
	return &IntegrationClient{
		apiKey:  apiKey,
		baseURL: integrationBaseURL,
		httpClient: &http.Client{
			Timeout: integrationHTTPTimeout,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			},
		},
	}
}

func (c *IntegrationClient) SetAPIKey(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.apiKey = key
}

func (c *IntegrationClient) HasAPIKey() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.apiKey != ""
}

func (c *IntegrationClient) getAPIKey() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.apiKey
}

func (c *IntegrationClient) doRequest(method, path string, body any) ([]byte, int, error) {
	key := c.getAPIKey()
	if key == "" {
		return nil, 0, ErrUnauthorized
	}

	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, 0, fmt.Errorf("marshal request: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequest(method, c.baseURL+path, bodyReader)
	if err != nil {
		return nil, 0, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("X-API-Key", key)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("%w: %v", ErrIntegrationAPI, err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return respBody, resp.StatusCode, ErrUnauthorized
	}
	if resp.StatusCode == http.StatusNotFound {
		return respBody, resp.StatusCode, ErrNotFound
	}

	return respBody, resp.StatusCode, nil
}

func (c *IntegrationClient) Validate() (*AppInfo, error) {
	body, status, err := c.doRequest("GET", "/v1/info", nil)
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("%w: GET /v1/info returned %d: %s", ErrIntegrationAPI, status, body)
	}

	var info AppInfo
	if err := json.Unmarshal(body, &info); err != nil {
		return nil, fmt.Errorf("parse info: %w", err)
	}
	return &info, nil
}

func doListRequest[T any](c *IntegrationClient, path string) ([]T, error) {
	body, status, err := c.doRequest("GET", path, nil)
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("%w: GET %s returned %d: %s", ErrIntegrationAPI, path, status, body)
	}
	var page paginatedResponse
	if err := json.Unmarshal(body, &page); err != nil {
		return nil, fmt.Errorf("parse response for %s: %w", path, err)
	}
	var items []T
	if err := json.Unmarshal(page.Data, &items); err != nil {
		return nil, fmt.Errorf("parse data for %s: %w", path, err)
	}
	return items, nil
}

func (c *IntegrationClient) getSites() ([]SiteInfo, error) {
	return doListRequest[SiteInfo](c, "/v1/sites")
}

func (c *IntegrationClient) listZones(siteID string) ([]Zone, error) {
	return doListRequest[Zone](c, fmt.Sprintf("/v1/sites/%s/firewall/zones?limit=%d", siteID, paginationLimit))
}

func (c *IntegrationClient) CreateZone(siteID, name string) (*Zone, error) {
	req := map[string]any{
		"name":       name,
		"networkIds": []string{},
	}

	body, status, err := c.doRequest("POST", fmt.Sprintf("/v1/sites/%s/firewall/zones", siteID), req)
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("%w: create zone returned %d: %s", ErrIntegrationAPI, status, body)
	}

	var zone Zone
	if err := json.Unmarshal(body, &zone); err != nil {
		return nil, fmt.Errorf("parse zone: %w", err)
	}
	return &zone, nil
}

func (c *IntegrationClient) findZoneByName(siteID, name string) (*Zone, error) {
	zones, err := c.listZones(siteID)
	if err != nil {
		return nil, err
	}
	for _, z := range zones {
		if z.Name == name {
			return &z, nil
		}
	}
	return nil, nil
}

func (c *IntegrationClient) ListPolicies(siteID string) ([]Policy, error) {
	return doListRequest[Policy](c, fmt.Sprintf("/v1/sites/%s/firewall/policies?limit=%d", siteID, paginationLimit))
}

type createPolicyRequest struct {
	Enabled         bool            `json:"enabled"`
	Name            string          `json:"name"`
	Action          PolicyAction    `json:"action"`
	Source          PolicyEndpoint  `json:"source"`
	Destination     PolicyEndpoint  `json:"destination"`
	IPProtocolScope IPProtocolScope `json:"ipProtocolScope"`
	LoggingEnabled  bool            `json:"loggingEnabled"`
}

func (c *IntegrationClient) CreatePolicy(siteID string, req createPolicyRequest) (*Policy, error) {
	body, status, err := c.doRequest("POST", fmt.Sprintf("/v1/sites/%s/firewall/policies", siteID), req)
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("%w: create policy returned %d: %s", ErrIntegrationAPI, status, body)
	}

	var pol Policy
	if err := json.Unmarshal(body, &pol); err != nil {
		return nil, fmt.Errorf("parse policy: %w", err)
	}
	return &pol, nil
}

func (c *IntegrationClient) DeletePolicy(siteID, policyID string) error {
	path := fmt.Sprintf("/v1/sites/%s/firewall/policies/%s", siteID, policyID)
	body, status, err := c.doRequest("DELETE", path, nil)
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("%w: delete policy returned %d: %s", ErrIntegrationAPI, status, body)
	}
	return nil
}

func (c *IntegrationClient) DeleteZone(siteID, zoneID string) error {
	path := fmt.Sprintf("/v1/sites/%s/firewall/zones/%s", siteID, zoneID)
	body, status, err := c.doRequest("DELETE", path, nil)
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("%w: delete zone returned %d: %s", ErrIntegrationAPI, status, body)
	}
	return nil
}

func (c *IntegrationClient) FindInternalZoneID(siteID string) (string, error) {
	zones, err := c.listZones(siteID)
	if err != nil {
		return "", err
	}
	var fallback string
	for _, z := range zones {
		if z.Name == "Internal" {
			return z.ID, nil
		}
		if fallback == "" && (z.Name == "LAN" || z.Name == "Default") {
			fallback = z.ID
		}
	}
	if fallback != "" {
		return fallback, nil
	}
	return "", fmt.Errorf("no Internal/LAN/Default zone found")
}

func (c *IntegrationClient) EnsureZone(siteID, name string) (*Zone, error) {
	existing, err := c.findZoneByName(siteID, name)
	if err != nil {
		return nil, fmt.Errorf("check existing zone: %w", err)
	}
	if existing != nil {
		return existing, nil
	}
	return c.CreateZone(siteID, name)
}

func (c *IntegrationClient) EnsurePolicies(siteID, zoneName, zoneID string) ([]string, error) {
	internalZoneID, err := c.FindInternalZoneID(siteID)
	if err != nil {
		return nil, fmt.Errorf("find internal zone: %w", err)
	}

	existing, err := c.ListPolicies(siteID)
	if err != nil {
		return nil, fmt.Errorf("list existing policies: %w", err)
	}

	wantPolicies := []createPolicyRequest{
		{
			Enabled: true,
			Name:    fmt.Sprintf("VPN Pack: Allow %s to Internal", zoneName),
			Action:  PolicyAction{Type: "ALLOW", AllowReturnTraffic: true},
			Source:  PolicyEndpoint{ZoneID: zoneID},
			Destination: PolicyEndpoint{ZoneID: internalZoneID},
			IPProtocolScope: IPProtocolScope{IPVersion: "IPV4_AND_IPV6"},
			LoggingEnabled:  false,
		},
		{
			Enabled: true,
			Name:    fmt.Sprintf("VPN Pack: Allow Internal to %s", zoneName),
			Action:  PolicyAction{Type: "ALLOW", AllowReturnTraffic: true},
			Source:  PolicyEndpoint{ZoneID: internalZoneID},
			Destination: PolicyEndpoint{ZoneID: zoneID},
			IPProtocolScope: IPProtocolScope{IPVersion: "IPV4_AND_IPV6"},
			LoggingEnabled:  false,
		},
	}

	var ids []string
	for _, want := range wantPolicies {
		if id := findExistingPolicy(existing, want.Name); id != "" {
			ids = append(ids, id)
			continue
		}
		pol, err := c.CreatePolicy(siteID, want)
		if err != nil {
			return ids, fmt.Errorf("create policy %q: %w", want.Name, err)
		}
		ids = append(ids, pol.ID)
	}
	return ids, nil
}

func findExistingPolicy(policies []Policy, name string) string {
	for _, p := range policies {
		if p.Name == name {
			return p.ID
		}
	}
	return ""
}

func (c *IntegrationClient) DiscoverSiteID() (string, error) {
	sites, err := c.getSites()
	if err != nil {
		return "", err
	}
	if len(sites) == 0 {
		return "", fmt.Errorf("no sites found")
	}
	return sites[0].ID, nil
}

func (c *IntegrationClient) findSystemZoneIDs(siteID string) (externalID, gatewayID string, err error) {
	zones, err := c.listZones(siteID)
	if err != nil {
		return "", "", err
	}
	for _, z := range zones {
		switch z.Name {
		case "External":
			externalID = z.ID
		case "Gateway":
			gatewayID = z.ID
		}
	}
	if externalID == "" {
		return "", "", fmt.Errorf("no External zone found")
	}
	if gatewayID == "" {
		return "", "", fmt.Errorf("no Gateway zone found")
	}
	return externalID, gatewayID, nil
}

func (c *IntegrationClient) createWanPortPolicy(siteID string, port int, name, externalZoneID, gatewayZoneID string) (*Policy, error) {
	req := createPolicyRequest{
		Enabled: true,
		Name:    name,
		Action:  PolicyAction{Type: "ALLOW", AllowReturnTraffic: false},
		Source:  PolicyEndpoint{ZoneID: externalZoneID},
		Destination: PolicyEndpoint{
			ZoneID: gatewayZoneID,
			TrafficFilter: &TrafficFilter{
				Type: "PORT",
				PortFilter: PortFilter{
					Type:          "PORTS",
					MatchOpposite: false,
					Items:         []PortFilterItem{{Type: "PORT_NUMBER", Value: port}},
				},
			},
		},
		IPProtocolScope: IPProtocolScope{
			IPVersion: "IPV4",
			ProtocolFilter: &ProtocolFilter{
				Type:          "NAMED_PROTOCOL",
				Protocol:      ProtocolName{Name: "UDP"},
				MatchOpposite: false,
			},
		},
		LoggingEnabled: false,
	}
	return c.CreatePolicy(siteID, req)
}

func (c *IntegrationClient) EnsureWanPortPolicy(siteID string, port int, name, externalZoneID, gatewayZoneID string) (string, error) {
	existing, err := c.ListPolicies(siteID)
	if err != nil {
		return "", fmt.Errorf("list existing policies: %w", err)
	}
	if id := findExistingPolicy(existing, name); id != "" {
		return id, nil
	}
	pol, err := c.createWanPortPolicy(siteID, port, name, externalZoneID, gatewayZoneID)
	if err != nil {
		return "", fmt.Errorf("create WAN port policy %q: %w", name, err)
	}
	return pol.ID, nil
}

func wanPortPolicyName(port int, marker string) string {
	if strings.HasPrefix(marker, wanMarkerWgS2sPrefix) {
		iface := strings.TrimPrefix(marker, wanMarkerWgS2sPrefix)
		return fmt.Sprintf("VPN Pack: WG S2S UDP %d (%s)", port, iface)
	}
	if marker == wanMarkerRelay {
		return fmt.Sprintf("VPN Pack: Relay Server UDP %d", port)
	}
	if marker == wanMarkerTailscaleWG {
		return fmt.Sprintf("VPN Pack: Tailscale WireGuard UDP %d", port)
	}
	return fmt.Sprintf("VPN Pack: UDP %d (%s)", port, marker)
}
