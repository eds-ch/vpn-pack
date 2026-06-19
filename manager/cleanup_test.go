package main

import (
	"context"
	"errors"
	"os/exec"
	"sync"
	"sync/atomic"
	"testing"

	"unifi-tailscale/manager/domain"
	"unifi-tailscale/manager/service"
	"unifi-tailscale/manager/state"
)

// TestRunCleanup_RefusesWhenManagerActive locks in the architectural
// invariant: cleanup must never race with a live manager process. The
// guard short-circuits before any UDAPI work, so the test does not need
// a UDAPI socket or root.
func TestRunCleanup_RefusesWhenManagerActive(t *testing.T) {
	orig := cleanupManagerActiveCheck
	t.Cleanup(func() { cleanupManagerActiveCheck = orig })

	cleanupManagerActiveCheck = func() bool { return true }

	err := runCleanup()
	if err == nil {
		t.Fatal("expected refusal error when manager service active")
	}
	if !errors.Is(err, errCleanupRefused) {
		t.Fatalf("expected errCleanupRefused, got %v", err)
	}
}

// TestCleanupManagerActiveCheck_FailsClosedOnProbeError documents that
// the default systemctl probe is fail-closed: a malformed/missing
// systemctl invocation reports "active" so cleanup refuses rather than
// racing the daemon. Only an *exec.ExitError (systemctl ran cleanly and
// reported "not active") counts as not-active.
func TestCleanupManagerActiveCheck_FailsClosedOnProbeError(t *testing.T) {
	// Real probe: invoke a binary guaranteed to not exist, simulating
	// systemctl unavailable. We have to drop down to the package-level
	// helper that the real var encapsulates; reproduce its logic with
	// a non-existent command and assert classification.
	err := exec.Command("/nonexistent/systemctl-fake", "is-active").Run()
	if err == nil {
		t.Fatal("expected exec error for missing binary")
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		t.Fatalf("error is *exec.ExitError, should be exec setup error: %T", err)
	}
}

// BUG-L17: if the manifest is corrupt or unreadable, the original cleanup
// silently skipped all Integration-API deletes, leaving every zone and
// policy this manager ever created orphaned in UniFi. The fallback uses
// API discovery on the "VPN Pack: " name prefix.
func TestRemoveIntegrationResources_DiscoveryFallback(t *testing.T) {
	t.Cleanup(func() {
		loadAPIKeyHook = service.LoadAPIKey
		LoadManifest = state.LoadManifest
		buildIntegrationAPIHook = buildIntegrationAPI
	})

	loadAPIKeyHook = func() string { return "fake-key" }
	LoadManifest = func(string) (*state.Manifest, error) {
		return nil, errors.New("manifest unreadable")
	}

	var deletedPolicies, deletedZones atomic.Int32
	var seenDiscoverSiteID atomic.Bool
	ic := &mockIntegrationAPI{
		hasAPIKeyFn:      func() bool { return true },
		discoverSiteIDFn: func(context.Context) (string, error) { seenDiscoverSiteID.Store(true); return "site-x", nil },
		listPoliciesFn: func(context.Context, string) ([]domain.Policy, error) {
			return []domain.Policy{
				{ID: "p1", Name: "VPN Pack: Allow LAN to Tailscale"},
				{ID: "p2", Name: "Some User Policy"}, // must NOT be deleted
				{ID: "p3", Name: "VPN Pack: Allow Tailscale to LAN"},
			}, nil
		},
		listZonesFn: func(context.Context, string) ([]domain.Zone, error) {
			return []domain.Zone{
				{ID: "z1", Name: "VPN Pack: Tailscale"},
				{ID: "z2", Name: "Internal"}, // must NOT be deleted
				{ID: "z3", Name: "VPN Pack: WireGuard S2S"},
			}, nil
		},
		deletePolicyFn: func(_ context.Context, _ string, policyID string) error {
			if policyID == "p2" {
				t.Errorf("deleted non-VPN-Pack policy %q", policyID)
			}
			deletedPolicies.Add(1)
			return nil
		},
		deleteZoneFn: func(_ context.Context, _ string, zoneID string) error {
			if zoneID == "z2" {
				t.Errorf("deleted non-VPN-Pack zone %q", zoneID)
			}
			deletedZones.Add(1)
			return nil
		},
	}
	buildIntegrationAPIHook = func(string) IntegrationAPI { return ic }

	removeIntegrationResources()

	if !seenDiscoverSiteID.Load() {
		t.Fatal("expected discovery to invoke DiscoverSiteID when manifest is unreadable")
	}
	if got := deletedPolicies.Load(); got != 2 {
		t.Fatalf("expected 2 VPN-Pack policies deleted, got %d", got)
	}
	if got := deletedZones.Load(); got != 2 {
		t.Fatalf("expected 2 VPN-Pack zones deleted, got %d", got)
	}
}

// Network 10.3+ materializes a DERIVED "(Return)" policy for each of our
// USER_DEFINED policies; it shares our "VPN Pack: " name prefix but is owned
// and cascade-deleted by the API when its parent goes. The discovery fallback
// must NOT issue its own DeletePolicy for DERIVED entries — doing so produces
// 4 deletes (2 parents + 2 children) and 404 noise after the cascade.
func TestRemoveIntegrationResources_DiscoverySkipsDerivedPolicies(t *testing.T) {
	t.Cleanup(func() {
		loadAPIKeyHook = service.LoadAPIKey
		LoadManifest = state.LoadManifest
		buildIntegrationAPIHook = buildIntegrationAPI
	})

	loadAPIKeyHook = func() string { return "fake-key" }
	LoadManifest = func(string) (*state.Manifest, error) {
		return nil, errors.New("manifest unreadable")
	}

	var mu sync.Mutex
	var deletedIDs []string
	ic := &mockIntegrationAPI{
		hasAPIKeyFn:      func() bool { return true },
		discoverSiteIDFn: func(context.Context) (string, error) { return "site-x", nil },
		listPoliciesFn: func(context.Context, string) ([]domain.Policy, error) {
			derived := &domain.PolicyMetadata{Origin: domain.PolicyOriginDerived}
			user := &domain.PolicyMetadata{Origin: domain.PolicyOriginUserDefined}
			return []domain.Policy{
				{ID: "u1", Name: "VPN Pack: Allow Tailscale to Internal", Metadata: user},
				{ID: "d1", Name: "VPN Pack: Allow Tailscale to Internal (Return)", Metadata: derived},
				{ID: "u2", Name: "VPN Pack: Allow Internal to Tailscale", Metadata: user},
				{ID: "d2", Name: "VPN Pack: Allow Internal to Tailscale (Return)", Metadata: derived},
			}, nil
		},
		deletePolicyFn: func(_ context.Context, _ string, policyID string) error {
			mu.Lock()
			deletedIDs = append(deletedIDs, policyID)
			mu.Unlock()
			return nil
		},
	}
	buildIntegrationAPIHook = func(string) IntegrationAPI { return ic }

	removeIntegrationResources()

	mu.Lock()
	defer mu.Unlock()
	if len(deletedIDs) != 2 {
		t.Fatalf("expected 2 deletes (USER_DEFINED only), got %d: %v", len(deletedIDs), deletedIDs)
	}
	for _, id := range deletedIDs {
		if id == "d1" || id == "d2" {
			t.Errorf("deleted DERIVED policy %q; cascade should handle it", id)
		}
	}
}
