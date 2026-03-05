package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"unifi-tailscale/manager/udapi"
)

func newMockUDAPISocket(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	sockPath := filepath.Join(dir, "udapi.sock")
	ln, err := net.Listen("unix", sockPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				reader := bufio.NewReader(c)
				sizeLine, err := reader.ReadString('\n')
				if err != nil {
					return
				}
				size, _ := strconv.Atoi(strings.TrimSpace(sizeLine))
				body := make([]byte, size)
				if _, err := io.ReadFull(reader, body); err != nil {
					return
				}

				var env struct {
					ID     string `json:"id"`
					Method string `json:"method"`
					Entity string `json:"entity"`
				}
				json.Unmarshal(body, &env)

				var respData any
				switch {
				case strings.HasPrefix(env.Entity, "/firewall/filter/"):
					respData = map[string]any{"rules": []any{}}
				case env.Entity == "/firewall/sets":
					respData = []any{
						map[string]any{
							"identification": map[string]string{"type": "ipv4", "name": "VPN_subnets"},
							"entries":        []string{},
						},
					}
				default:
					respData = map[string]any{}
				}

				respPayload, _ := json.Marshal(respData)
				resp := map[string]any{
					"id":       env.ID,
					"version":  "v1.0",
					"method":   env.Method,
					"entity":   env.Entity,
					"response": json.RawMessage(respPayload),
				}
				data, _ := json.Marshal(resp)
				fmt.Fprintf(c, "%d\n%s", len(data), data)
			}(conn)
		}
	}()

	return sockPath
}

func newTestManifest(t *testing.T) *Manifest {
	t.Helper()
	dir := t.TempDir()
	m, err := LoadManifest(filepath.Join(dir, "manifest.json"))
	require.NoError(t, err)
	require.NoError(t, m.SetSiteID("site-test"))
	return m
}

func newTestFW(t *testing.T, ic *IntegrationClient, m *Manifest) *FirewallManager {
	t.Helper()
	return newTestFWWithSocket(t, filepath.Join(t.TempDir(), "nonexistent.sock"), ic, m)
}

func newTestFWWithSocket(t *testing.T, sockPath string, ic *IntegrationClient, m *Manifest) *FirewallManager {
	t.Helper()
	return &FirewallManager{
		udapi:    udapi.NewClient(sockPath),
		ic:       ic,
		manifest: m,
	}
}

type integrationMockOpts struct {
	zones          []Zone
	policies       []Policy
	zoneFail       bool
	policyFail     bool
	deleteZoneFail bool
	deleteZoneFn   func(zoneID string)
	deletePolicyFn func(policyID string)
}

func newIntegrationMockHandler(opts integrationMockOpts) http.Handler {
	var policyCounter atomic.Int32
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/firewall/zones"):
			if r.Method == "DELETE" {
				if opts.deleteZoneFail {
					http.Error(w, `{"error":"delete failed"}`, http.StatusInternalServerError)
					return
				}
				parts := strings.Split(r.URL.Path, "/")
				zoneID := parts[len(parts)-1]
				if opts.deleteZoneFn != nil {
					opts.deleteZoneFn(zoneID)
				}
				w.WriteHeader(http.StatusOK)
				return
			}
			if opts.zoneFail {
				http.Error(w, `{"error":"zone error"}`, http.StatusInternalServerError)
				return
			}
			if r.Method == "POST" {
				var req map[string]any
				json.NewDecoder(r.Body).Decode(&req)
				zone := Zone{ID: "zone-created", Name: fmt.Sprint(req["name"])}
				w.WriteHeader(http.StatusCreated)
				json.NewEncoder(w).Encode(zone)
				return
			}
			zs := opts.zones
			if zs == nil {
				zs = []Zone{}
			}
			data, _ := json.Marshal(zs)
			w.WriteHeader(200)
			json.NewEncoder(w).Encode(map[string]json.RawMessage{"data": data})

		case strings.Contains(r.URL.Path, "/firewall/policies"):
			if r.Method == "DELETE" {
				parts := strings.Split(r.URL.Path, "/")
				policyID := parts[len(parts)-1]
				if opts.deletePolicyFn != nil {
					opts.deletePolicyFn(policyID)
				}
				w.WriteHeader(http.StatusOK)
				return
			}
			if opts.policyFail {
				http.Error(w, `{"error":"policy error"}`, http.StatusInternalServerError)
				return
			}
			if r.Method == "POST" {
				n := policyCounter.Add(1)
				pol := Policy{ID: fmt.Sprintf("pol-%d", n), Name: "created"}
				w.WriteHeader(http.StatusCreated)
				json.NewEncoder(w).Encode(pol)
				return
			}
			ps := opts.policies
			if ps == nil {
				ps = []Policy{}
			}
			data, _ := json.Marshal(ps)
			w.WriteHeader(200)
			json.NewEncoder(w).Encode(map[string]json.RawMessage{"data": data})

		default:
			http.Error(w, "not found", 404)
		}
	})
}

func newMockIC(t *testing.T, opts integrationMockOpts) *IntegrationClient {
	t.Helper()
	ts := httptest.NewServer(newIntegrationMockHandler(opts))
	t.Cleanup(ts.Close)
	ic := NewIntegrationClient("test-api-key")
	ic.baseURL = ts.URL
	return ic
}

func hasStepError(r *FirewallSetupResult, step string) bool {
	for _, e := range r.Errors {
		if e.Step == step {
			return true
		}
	}
	return false
}

func TestSetupTailscaleFirewall_IntegrationNotConfigured(t *testing.T) {
	m := newTestManifest(t)
	fm := newTestFW(t, NewIntegrationClient(""), m)

	result := fm.SetupTailscaleFirewall(context.Background())

	assert.False(t, result.ZoneCreated)
	assert.False(t, result.PoliciesReady)
	assert.False(t, result.UDAPIApplied)
	assert.Empty(t, result.Errors)
	assert.False(t, result.OK())
	assert.False(t, result.Degraded())
}

func TestSetupTailscaleFirewall_ZoneFail(t *testing.T) {
	ic := newMockIC(t, integrationMockOpts{zoneFail: true})
	m := newTestManifest(t)
	fm := newTestFW(t, ic, m)

	result := fm.SetupTailscaleFirewall(context.Background())

	assert.False(t, result.ZoneCreated)
	assert.False(t, result.PoliciesReady)
	assert.False(t, result.OK())
	assert.False(t, result.Degraded())
	assert.True(t, hasStepError(result, "zone"))
}

func TestSetupTailscaleFirewall_PolicyFail_RollbackZone(t *testing.T) {
	zones := []Zone{
		{ID: "zone-ts", Name: "VPN Pack: Tailscale"},
		{ID: "zone-int", Name: "Internal"},
	}
	var deletedZoneID string
	ic := newMockIC(t, integrationMockOpts{
		zones:      zones,
		policyFail: true,
		deleteZoneFn: func(zoneID string) {
			deletedZoneID = zoneID
		},
	})
	m := newTestManifest(t)
	fm := newTestFW(t, ic, m)

	result := fm.SetupTailscaleFirewall(context.Background())

	assert.False(t, result.ZoneCreated, "zone should be rolled back")
	assert.Empty(t, result.ZoneID)
	assert.False(t, result.PoliciesReady)
	assert.False(t, result.OK())
	assert.Equal(t, "zone-ts", deletedZoneID, "rollback should delete the zone")

	assert.True(t, hasStepError(result, "policies"))
	assert.Equal(t, "", m.GetTailscaleZone().ZoneID, "manifest should not contain zone")
}

func TestSetupTailscaleFirewall_UDAPIFail(t *testing.T) {
	zones := []Zone{
		{ID: "zone-ts", Name: "VPN Pack: Tailscale"},
		{ID: "zone-int", Name: "Internal"},
	}
	ic := newMockIC(t, integrationMockOpts{zones: zones})
	m := newTestManifest(t)
	fm := newTestFW(t, ic, m)

	result := fm.SetupTailscaleFirewall(context.Background())

	assert.True(t, result.ZoneCreated)
	assert.Equal(t, "zone-ts", result.ZoneID)
	assert.True(t, result.PoliciesReady)
	assert.False(t, result.UDAPIApplied)
	assert.True(t, result.Degraded())
	assert.False(t, result.OK())

	assert.True(t, hasStepError(result, "udapi"))
}

func TestSetupTailscaleFirewall_Success(t *testing.T) {
	zones := []Zone{
		{ID: "zone-ts", Name: "VPN Pack: Tailscale"},
		{ID: "zone-int", Name: "Internal"},
	}
	ic := newMockIC(t, integrationMockOpts{zones: zones})
	m := newTestManifest(t)
	sockPath := newMockUDAPISocket(t)
	fm := newTestFWWithSocket(t, sockPath, ic, m)

	result := fm.SetupTailscaleFirewall(context.Background())

	assert.True(t, result.ZoneCreated)
	assert.Equal(t, "zone-ts", result.ZoneID)
	assert.Equal(t, "VPN Pack: Tailscale", result.ZoneName)
	assert.True(t, result.PoliciesReady)
	assert.True(t, result.UDAPIApplied)
	assert.True(t, result.OK())
	assert.False(t, result.Degraded())
	assert.Empty(t, result.Errors)
}

func TestSetupTailscaleFirewall_ManifestFail_RollbackZoneAndPolicies(t *testing.T) {
	zones := []Zone{
		{ID: "zone-ts", Name: "VPN Pack: Tailscale"},
		{ID: "zone-int", Name: "Internal"},
	}
	var deletedZoneID string
	var deletedPolicies []string
	ic := newMockIC(t, integrationMockOpts{
		zones: zones,
		deleteZoneFn: func(zoneID string) {
			deletedZoneID = zoneID
		},
		deletePolicyFn: func(policyID string) {
			deletedPolicies = append(deletedPolicies, policyID)
		},
	})
	m := newTestManifest(t)
	// Make manifest save fail by making path unwritable
	m.path = "/nonexistent-dir/manifest.json"
	fm := newTestFW(t, ic, m)

	result := fm.SetupTailscaleFirewall(context.Background())

	assert.False(t, result.ZoneCreated, "zone should be rolled back")
	assert.Empty(t, result.ZoneID)
	assert.False(t, result.PoliciesReady)
	assert.Nil(t, result.PolicyIDs)
	assert.False(t, result.OK())
	assert.Equal(t, "zone-ts", deletedZoneID, "rollback should delete the zone")
	assert.NotEmpty(t, deletedPolicies, "rollback should delete policies")
	assert.True(t, hasStepError(result, "manifest"))
}

func TestSetupTailscaleFirewall_RollbackFails_BestEffort(t *testing.T) {
	zones := []Zone{
		{ID: "zone-ts", Name: "VPN Pack: Tailscale"},
		{ID: "zone-int", Name: "Internal"},
	}
	ic := newMockIC(t, integrationMockOpts{zones: zones, policyFail: true, deleteZoneFail: true})
	m := newTestManifest(t)
	fm := newTestFW(t, ic, m)

	result := fm.SetupTailscaleFirewall(context.Background())

	assert.False(t, result.ZoneCreated)
	assert.True(t, hasStepError(result, "policies"))
	assert.Equal(t, "", m.GetTailscaleZone().ZoneID, "manifest should not contain zone")
}

func TestRollbackZone_CancelledCtx_StillDeletes(t *testing.T) {
	var deletedZoneID string
	var deletedPolicies []string
	ic := newMockIC(t, integrationMockOpts{
		deleteZoneFn: func(zoneID string) {
			deletedZoneID = zoneID
		},
		deletePolicyFn: func(policyID string) {
			deletedPolicies = append(deletedPolicies, policyID)
		},
	})
	m := newTestManifest(t)
	fm := newTestFW(t, ic, m)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	fm.rollbackZone(ctx, "site-test", "zone-orphan", "test rollback", "pol-1", "pol-2")

	assert.Equal(t, "zone-orphan", deletedZoneID, "zone should be deleted despite cancelled ctx")
	assert.ElementsMatch(t, []string{"pol-1", "pol-2"}, deletedPolicies, "policies should be deleted despite cancelled ctx")
}

func TestSetupWgS2sZone_PolicyFail_RollbackZone(t *testing.T) {
	zones := []Zone{
		{ID: "zone-int", Name: "Internal"},
	}
	var deletedZoneID string
	ic := newMockIC(t, integrationMockOpts{
		zones:      zones,
		policyFail: true,
		deleteZoneFn: func(zoneID string) {
			deletedZoneID = zoneID
		},
	})
	m := newTestManifest(t)
	fm := newTestFW(t, ic, m)

	result := fm.SetupWgS2sZone(context.Background(), "tun-1", "", "Test Tunnel")

	assert.False(t, result.ZoneCreated, "zone should be rolled back")
	assert.Empty(t, result.ZoneID)
	assert.False(t, result.PoliciesReady)
	assert.True(t, hasStepError(result, "policies"))
	assert.Equal(t, "zone-created", deletedZoneID, "rollback should delete the zone")

	_, ok := m.GetWgS2sZone("tun-1")
	assert.False(t, ok, "manifest should not contain tunnel zone")
}

func TestSetupWgS2sZone_ManifestFail_RollbackZoneAndPolicies(t *testing.T) {
	zones := []Zone{
		{ID: "zone-int", Name: "Internal"},
	}
	var deletedZoneID string
	var deletedPolicies []string
	ic := newMockIC(t, integrationMockOpts{
		zones: zones,
		deleteZoneFn: func(zoneID string) {
			deletedZoneID = zoneID
		},
		deletePolicyFn: func(policyID string) {
			deletedPolicies = append(deletedPolicies, policyID)
		},
	})
	m := newTestManifest(t)
	fm := newTestFW(t, ic, m)
	// Break manifest after siteID is set
	m.path = "/nonexistent-dir/manifest.json"

	result := fm.SetupWgS2sZone(context.Background(), "tun-1", "", "Test Tunnel")

	assert.False(t, result.ZoneCreated, "zone should be rolled back")
	assert.Empty(t, result.ZoneID)
	assert.False(t, result.PoliciesReady)
	assert.Nil(t, result.PolicyIDs)
	assert.True(t, hasStepError(result, "manifest"))
	assert.Equal(t, "zone-created", deletedZoneID, "rollback should delete the zone")
	assert.NotEmpty(t, deletedPolicies, "rollback should delete policies")
}

func TestSetupWgS2sZone_ZoneReuse_NoRollback(t *testing.T) {
	ic := newMockIC(t, integrationMockOpts{})
	m := newTestManifest(t)
	require.NoError(t, m.SetWgS2sZone("tun-existing", ZoneManifest{
		ZoneID: "zone-shared", ZoneName: "Shared", PolicyIDs: []string{"pol-1"}, ChainPrefix: "VPN",
	}))
	fm := newTestFW(t, ic, m)

	result := fm.SetupWgS2sZone(context.Background(), "tun-2", "zone-shared", "")

	assert.True(t, result.ZoneCreated)
	assert.Equal(t, "zone-shared", result.ZoneID)
	assert.True(t, result.PoliciesReady)
	zm, ok := m.GetWgS2sZone("tun-2")
	assert.True(t, ok)
	assert.Equal(t, "zone-shared", zm.ZoneID)
}

func TestTeardownWgS2sZone_LastTunnel_DeletesZoneAndPolicies(t *testing.T) {
	var deletedPolicies []string
	var deletedZoneID string
	ic := newMockIC(t, integrationMockOpts{
		deleteZoneFn: func(zoneID string) {
			deletedZoneID = zoneID
		},
		deletePolicyFn: func(policyID string) {
			deletedPolicies = append(deletedPolicies, policyID)
		},
	})
	m := newTestManifest(t)
	require.NoError(t, m.SetWgS2sZone("tun-1", ZoneManifest{
		ZoneID: "zone-wg", ZoneName: "WG S2S", PolicyIDs: []string{"pol-a", "pol-b"}, ChainPrefix: "CUSTOM1",
	}))
	fm := newTestFW(t, ic, m)

	fm.TeardownWgS2sZone(context.Background(), "tun-1")

	_, ok := m.GetWgS2sZone("tun-1")
	assert.False(t, ok, "tunnel should be removed from manifest")
	assert.Equal(t, "zone-wg", deletedZoneID, "zone should be deleted")
	assert.ElementsMatch(t, []string{"pol-a", "pol-b"}, deletedPolicies, "all policies should be deleted")
}

func TestTeardownWgS2sZone_SharedZone_KeepsZone(t *testing.T) {
	var deletedZoneID string
	ic := newMockIC(t, integrationMockOpts{
		deleteZoneFn: func(zoneID string) {
			deletedZoneID = zoneID
		},
	})
	m := newTestManifest(t)
	zm := ZoneManifest{ZoneID: "zone-shared", ZoneName: "Shared", PolicyIDs: []string{"pol-1"}, ChainPrefix: "VPN"}
	require.NoError(t, m.SetWgS2sZone("tun-1", zm))
	require.NoError(t, m.SetWgS2sZone("tun-2", zm))
	fm := newTestFW(t, ic, m)

	fm.TeardownWgS2sZone(context.Background(), "tun-1")

	_, ok := m.GetWgS2sZone("tun-1")
	assert.False(t, ok, "tun-1 should be removed from manifest")
	_, ok = m.GetWgS2sZone("tun-2")
	assert.True(t, ok, "tun-2 should remain")
	assert.Empty(t, deletedZoneID, "shared zone should NOT be deleted")
}

func TestTeardownWgS2sZone_NotFound_Noop(t *testing.T) {
	ic := newMockIC(t, integrationMockOpts{})
	m := newTestManifest(t)
	fm := newTestFW(t, ic, m)

	// Should not panic
	fm.TeardownWgS2sZone(context.Background(), "nonexistent")
}
