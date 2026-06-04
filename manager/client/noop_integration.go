package client

import (
	"context"
	"errors"

	"unifi-tailscale/manager/domain"
)

// ErrIntegrationDisabled is returned by every NoopIntegrationAPI
// method that signals an error. The manager surfaces this through the
// existing FirewallHealth degraded path so the operator sees why
// integration features are 503-ing.
var ErrIntegrationDisabled = errors.New("integration disabled (no SPKI pin)")

type noopIntegrationAPI struct{}

// NoopIntegrationAPI returns an IntegrationAPI whose HasAPIKey is
// permanently false and whose mutating methods fail with
// ErrIntegrationDisabled. Used at boot when SPKI pin material is
// unavailable (SEC-C5 fail-closed): the manager continues to serve
// non-integration features rather than establishing unverified TLS.
func NoopIntegrationAPI() domain.IntegrationAPI { return noopIntegrationAPI{} }

func (noopIntegrationAPI) HasAPIKey() bool { return false }
func (noopIntegrationAPI) SetAPIKey(string) {}

func (noopIntegrationAPI) Validate(context.Context) (*domain.AppInfo, error) {
	return nil, ErrIntegrationDisabled
}

func (noopIntegrationAPI) DiscoverSiteID(context.Context) (string, error) {
	return "", ErrIntegrationDisabled
}

func (noopIntegrationAPI) CreateZone(context.Context, string, string) (*domain.Zone, error) {
	return nil, ErrIntegrationDisabled
}

func (noopIntegrationAPI) EnsureZone(context.Context, string, string) (*domain.Zone, error) {
	return nil, ErrIntegrationDisabled
}

func (noopIntegrationAPI) EnsurePolicies(context.Context, string, string, string) ([]string, error) {
	return nil, ErrIntegrationDisabled
}

func (noopIntegrationAPI) ListPolicies(context.Context, string) ([]domain.Policy, error) {
	return nil, ErrIntegrationDisabled
}

func (noopIntegrationAPI) DeletePolicy(context.Context, string, string) error {
	return ErrIntegrationDisabled
}

func (noopIntegrationAPI) DeleteZone(context.Context, string, string) error {
	return ErrIntegrationDisabled
}

func (noopIntegrationAPI) FindInternalZoneID(context.Context, string) (string, error) {
	return "", ErrIntegrationDisabled
}

func (noopIntegrationAPI) ListZones(context.Context, string) ([]domain.Zone, error) {
	return nil, ErrIntegrationDisabled
}

func (noopIntegrationAPI) FindSystemZoneIDs(context.Context, string) (string, string, error) {
	return "", "", ErrIntegrationDisabled
}

func (noopIntegrationAPI) EnsureWanPortPolicy(context.Context, string, int, string, string, string) (string, error) {
	return "", ErrIntegrationDisabled
}

func (noopIntegrationAPI) EnsureDNSForwardDomain(context.Context, string, string, string) (*domain.DNSPolicy, error) {
	return nil, ErrIntegrationDisabled
}

func (noopIntegrationAPI) DeleteDNSPolicy(context.Context, string, string) error {
	return ErrIntegrationDisabled
}

func (noopIntegrationAPI) ListDNSPolicies(context.Context, string) ([]domain.DNSPolicy, error) {
	return nil, ErrIntegrationDisabled
}
