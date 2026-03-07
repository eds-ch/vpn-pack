package client

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"unifi-tailscale/manager/config"
	"unifi-tailscale/manager/domain"
)

type IntegrationClient struct {
	mu         sync.RWMutex
	apiKey     string
	baseURL    string
	httpClient *http.Client

	zonesMu     sync.Mutex
	zonesCache  []domain.Zone
	zonesSiteID string
	zonesTime   time.Time
}

type paginatedResponse struct {
	Data json.RawMessage `json:"data"`
}

func NewIntegrationClient(apiKey string) *IntegrationClient {
	return &IntegrationClient{
		apiKey:  apiKey,
		baseURL: config.IntegrationBaseURL,
		httpClient: &http.Client{
			Timeout: config.IntegrationHTTPTimeout,
			Transport: &http.Transport{
				// Integration API on localhost uses self-signed cert.
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

func (c *IntegrationClient) doRequest(ctx context.Context, method, path string, body any) ([]byte, int, error) {
	key := c.getAPIKey()
	if key == "" {
		return nil, 0, domain.ErrUnauthorized
	}

	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, 0, fmt.Errorf("marshal request: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bodyReader)
	if err != nil {
		return nil, 0, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("X-API-Key", key)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("%w: %w", domain.ErrIntegrationAPI, err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return respBody, resp.StatusCode, domain.ErrUnauthorized
	}
	if resp.StatusCode == http.StatusNotFound {
		return respBody, resp.StatusCode, domain.ErrNotFound
	}

	return respBody, resp.StatusCode, nil
}

func (c *IntegrationClient) Validate(ctx context.Context) (*domain.AppInfo, error) {
	body, status, err := c.doRequest(ctx, "GET", "/v1/info", nil)
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("%w: GET /v1/info returned %d: %s", domain.ErrIntegrationAPI, status, body)
	}

	var info domain.AppInfo
	if err := json.Unmarshal(body, &info); err != nil {
		return nil, fmt.Errorf("parse info: %w", err)
	}
	return &info, nil
}

func doListRequest[T any](c *IntegrationClient, ctx context.Context, path string) ([]T, error) {
	body, status, err := c.doRequest(ctx, "GET", path, nil)
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("%w: GET %s returned %d: %s", domain.ErrIntegrationAPI, path, status, body)
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

func (c *IntegrationClient) getSites(ctx context.Context) ([]domain.SiteInfo, error) {
	return doListRequest[domain.SiteInfo](c, ctx, "/v1/sites")
}

func (c *IntegrationClient) ListZones(ctx context.Context, siteID string) ([]domain.Zone, error) {
	c.zonesMu.Lock()
	if c.zonesSiteID == siteID && c.zonesCache != nil && time.Since(c.zonesTime) < 5*time.Second {
		zones := c.zonesCache
		c.zonesMu.Unlock()
		return zones, nil
	}
	c.zonesMu.Unlock()

	zones, err := doListRequest[domain.Zone](c, ctx, fmt.Sprintf("/v1/sites/%s/firewall/zones?limit=%d", siteID, config.PaginationLimit))
	if err != nil {
		return nil, err
	}

	c.zonesMu.Lock()
	c.zonesCache = zones
	c.zonesSiteID = siteID
	c.zonesTime = time.Now()
	c.zonesMu.Unlock()

	return zones, nil
}

func (c *IntegrationClient) CreateZone(ctx context.Context, siteID, name string) (*domain.Zone, error) {
	req := map[string]any{
		"name":       name,
		"networkIds": []string{},
	}

	body, status, err := c.doRequest(ctx, "POST", fmt.Sprintf("/v1/sites/%s/firewall/zones", siteID), req)
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("%w: create zone returned %d: %s", domain.ErrIntegrationAPI, status, body)
	}

	var zone domain.Zone
	if err := json.Unmarshal(body, &zone); err != nil {
		return nil, fmt.Errorf("parse zone: %w", err)
	}
	return &zone, nil
}

func (c *IntegrationClient) findZoneByName(ctx context.Context, siteID, name string) (*domain.Zone, error) {
	zones, err := c.ListZones(ctx, siteID)
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

func (c *IntegrationClient) ListPolicies(ctx context.Context, siteID string) ([]domain.Policy, error) {
	return doListRequest[domain.Policy](c, ctx, fmt.Sprintf("/v1/sites/%s/firewall/policies?limit=%d", siteID, config.PaginationLimit))
}

type CreatePolicyRequest struct {
	Enabled         bool                   `json:"enabled"`
	Name            string                 `json:"name"`
	Action          domain.PolicyAction    `json:"action"`
	Source          domain.PolicyEndpoint  `json:"source"`
	Destination     domain.PolicyEndpoint  `json:"destination"`
	IPProtocolScope domain.IPProtocolScope `json:"ipProtocolScope"`
	LoggingEnabled  bool                   `json:"loggingEnabled"`
}

func (c *IntegrationClient) CreatePolicy(ctx context.Context, siteID string, req CreatePolicyRequest) (*domain.Policy, error) {
	body, status, err := c.doRequest(ctx, "POST", fmt.Sprintf("/v1/sites/%s/firewall/policies", siteID), req)
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("%w: create policy returned %d: %s", domain.ErrIntegrationAPI, status, body)
	}

	var pol domain.Policy
	if err := json.Unmarshal(body, &pol); err != nil {
		return nil, fmt.Errorf("parse policy: %w", err)
	}
	return &pol, nil
}

func (c *IntegrationClient) DeletePolicy(ctx context.Context, siteID, policyID string) error {
	path := fmt.Sprintf("/v1/sites/%s/firewall/policies/%s", siteID, policyID)
	body, status, err := c.doRequest(ctx, "DELETE", path, nil)
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("%w: delete policy returned %d: %s", domain.ErrIntegrationAPI, status, body)
	}
	return nil
}

func (c *IntegrationClient) DeleteZone(ctx context.Context, siteID, zoneID string) error {
	path := fmt.Sprintf("/v1/sites/%s/firewall/zones/%s", siteID, zoneID)
	body, status, err := c.doRequest(ctx, "DELETE", path, nil)
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("%w: delete zone returned %d: %s", domain.ErrIntegrationAPI, status, body)
	}
	return nil
}

func (c *IntegrationClient) FindInternalZoneID(ctx context.Context, siteID string) (string, error) {
	zones, err := c.ListZones(ctx, siteID)
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

func (c *IntegrationClient) EnsureZone(ctx context.Context, siteID, name string) (*domain.Zone, error) {
	existing, err := c.findZoneByName(ctx, siteID, name)
	if err != nil {
		return nil, fmt.Errorf("check existing zone: %w", err)
	}
	if existing != nil {
		return existing, nil
	}
	return c.CreateZone(ctx, siteID, name)
}

func (c *IntegrationClient) EnsurePolicies(ctx context.Context, siteID, zoneName, zoneID string) ([]string, error) {
	internalZoneID, err := c.FindInternalZoneID(ctx, siteID)
	if err != nil {
		return nil, fmt.Errorf("find internal zone: %w", err)
	}

	existing, err := c.ListPolicies(ctx, siteID)
	if err != nil {
		return nil, fmt.Errorf("list existing policies: %w", err)
	}

	wantPolicies := []CreatePolicyRequest{
		{
			Enabled:         true,
			Name:            fmt.Sprintf("VPN Pack: Allow %s to Internal", zoneName),
			Action:          domain.PolicyAction{Type: "ALLOW", AllowReturnTraffic: true},
			Source:          domain.PolicyEndpoint{ZoneID: zoneID},
			Destination:     domain.PolicyEndpoint{ZoneID: internalZoneID},
			IPProtocolScope: domain.IPProtocolScope{IPVersion: "IPV4_AND_IPV6"},
			LoggingEnabled:  false,
		},
		{
			Enabled:         true,
			Name:            fmt.Sprintf("VPN Pack: Allow Internal to %s", zoneName),
			Action:          domain.PolicyAction{Type: "ALLOW", AllowReturnTraffic: true},
			Source:          domain.PolicyEndpoint{ZoneID: internalZoneID},
			Destination:     domain.PolicyEndpoint{ZoneID: zoneID},
			IPProtocolScope: domain.IPProtocolScope{IPVersion: "IPV4_AND_IPV6"},
			LoggingEnabled:  false,
		},
	}

	var ids []string
	for _, want := range wantPolicies {
		if id := findExistingPolicy(existing, want.Name); id != "" {
			ids = append(ids, id)
			continue
		}
		pol, err := c.CreatePolicy(ctx, siteID, want)
		if err != nil {
			return ids, fmt.Errorf("create policy %q: %w", want.Name, err)
		}
		ids = append(ids, pol.ID)
	}
	return ids, nil
}

func findExistingPolicy(policies []domain.Policy, name string) string {
	for _, p := range policies {
		if p.Name == name {
			return p.ID
		}
	}
	return ""
}

func (c *IntegrationClient) DiscoverSiteID(ctx context.Context) (string, error) {
	sites, err := c.getSites(ctx)
	if err != nil {
		return "", err
	}
	if len(sites) == 0 {
		return "", fmt.Errorf("no sites found")
	}
	return sites[0].ID, nil
}

func (c *IntegrationClient) FindSystemZoneIDs(ctx context.Context, siteID string) (externalID, gatewayID string, err error) {
	zones, err := c.ListZones(ctx, siteID)
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

func (c *IntegrationClient) createWanPortPolicy(ctx context.Context, siteID string, port int, name, externalZoneID, gatewayZoneID string) (*domain.Policy, error) {
	req := CreatePolicyRequest{
		Enabled: true,
		Name:    name,
		Action:  domain.PolicyAction{Type: "ALLOW", AllowReturnTraffic: false},
		Source:  domain.PolicyEndpoint{ZoneID: externalZoneID},
		Destination: domain.PolicyEndpoint{
			ZoneID: gatewayZoneID,
			TrafficFilter: &domain.TrafficFilter{
				Type: "PORT",
				PortFilter: domain.PortFilter{
					Type:          "PORTS",
					MatchOpposite: false,
					Items:         []domain.PortFilterItem{{Type: "PORT_NUMBER", Value: port}},
				},
			},
		},
		IPProtocolScope: domain.IPProtocolScope{
			IPVersion: "IPV4",
			ProtocolFilter: &domain.ProtocolFilter{
				Type:          "NAMED_PROTOCOL",
				Protocol:      domain.ProtocolName{Name: "UDP"},
				MatchOpposite: false,
			},
		},
		LoggingEnabled: false,
	}
	return c.CreatePolicy(ctx, siteID, req)
}

func (c *IntegrationClient) EnsureWanPortPolicy(ctx context.Context, siteID string, port int, name, externalZoneID, gatewayZoneID string) (string, error) {
	existing, err := c.ListPolicies(ctx, siteID)
	if err != nil {
		return "", fmt.Errorf("list existing policies: %w", err)
	}
	if id := findExistingPolicy(existing, name); id != "" {
		return id, nil
	}
	pol, err := c.createWanPortPolicy(ctx, siteID, port, name, externalZoneID, gatewayZoneID)
	if err != nil {
		return "", fmt.Errorf("create WAN port policy %q: %w", name, err)
	}
	return pol.ID, nil
}

type createDNSPolicyRequest struct {
	Type      string `json:"type"`
	Domain    string `json:"domain"`
	IPAddress string `json:"ipAddress"`
	Enabled   bool   `json:"enabled"`
}

func (c *IntegrationClient) ListDNSPolicies(ctx context.Context, siteID string) ([]domain.DNSPolicy, error) {
	return doListRequest[domain.DNSPolicy](c, ctx, fmt.Sprintf("/v1/sites/%s/dns/policies?limit=%d", siteID, config.PaginationLimit))
}

func (c *IntegrationClient) CreateDNSPolicy(ctx context.Context, siteID string, req createDNSPolicyRequest) (*domain.DNSPolicy, error) {
	body, status, err := c.doRequest(ctx, "POST", fmt.Sprintf("/v1/sites/%s/dns/policies", siteID), req)
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("%w: create DNS policy returned %d: %s", domain.ErrIntegrationAPI, status, body)
	}
	var pol domain.DNSPolicy
	if err := json.Unmarshal(body, &pol); err != nil {
		return nil, fmt.Errorf("parse DNS policy: %w", err)
	}
	return &pol, nil
}

func (c *IntegrationClient) DeleteDNSPolicy(ctx context.Context, siteID, policyID string) error {
	path := fmt.Sprintf("/v1/sites/%s/dns/policies/%s", siteID, policyID)
	body, status, err := c.doRequest(ctx, "DELETE", path, nil)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil
		}
		return err
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("%w: delete DNS policy returned %d: %s", domain.ErrIntegrationAPI, status, body)
	}
	return nil
}

func (c *IntegrationClient) findDNSPolicyByDomain(ctx context.Context, siteID, domainName string) (*domain.DNSPolicy, error) {
	policies, err := c.ListDNSPolicies(ctx, siteID)
	if err != nil {
		return nil, err
	}
	for _, p := range policies {
		if p.Domain == domainName {
			return &p, nil
		}
	}
	return nil, nil
}

func (c *IntegrationClient) EnsureDNSForwardDomain(ctx context.Context, siteID, domainName, ipAddress string) (*domain.DNSPolicy, error) {
	existing, err := c.findDNSPolicyByDomain(ctx, siteID, domainName)
	if err != nil {
		return nil, fmt.Errorf("check existing DNS policy: %w", err)
	}
	if existing != nil {
		return existing, nil
	}
	return c.CreateDNSPolicy(ctx, siteID, createDNSPolicyRequest{
		Type:      "FORWARD_DOMAIN",
		Domain:    domainName,
		IPAddress: ipAddress,
		Enabled:   true,
	})
}

func WanPortPolicyName(port int, marker string) string {
	if strings.HasPrefix(marker, config.WanMarkerWgS2sPrefix) {
		iface := strings.TrimPrefix(marker, config.WanMarkerWgS2sPrefix)
		return fmt.Sprintf("VPN Pack: WG S2S UDP %d (%s)", port, iface)
	}
	if marker == config.WanMarkerRelay {
		return fmt.Sprintf("VPN Pack: Relay Server UDP %d", port)
	}
	if marker == config.WanMarkerTailscaleWG {
		return fmt.Sprintf("VPN Pack: Tailscale WireGuard UDP %d", port)
	}
	return fmt.Sprintf("VPN Pack: UDP %d (%s)", port, marker)
}
