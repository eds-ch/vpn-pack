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
	m.SetSiteID("site-test")
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
	zones        []Zone
	policies     []Policy
	zoneFail     bool
	policyFail   bool
}

func newIntegrationMockHandler(opts integrationMockOpts) http.Handler {
	var policyCounter atomic.Int32
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/firewall/zones"):
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

	hasZoneErr := false
	for _, e := range result.Errors {
		if e.Step == "zone" {
			hasZoneErr = true
		}
	}
	assert.True(t, hasZoneErr, "expected zone error in result")
}

func TestSetupTailscaleFirewall_PolicyFail(t *testing.T) {
	zones := []Zone{
		{ID: "zone-ts", Name: "VPN Pack: Tailscale"},
		{ID: "zone-int", Name: "Internal"},
	}
	ic := newMockIC(t, integrationMockOpts{zones: zones, policyFail: true})
	m := newTestManifest(t)
	fm := newTestFW(t, ic, m)

	result := fm.SetupTailscaleFirewall(context.Background())

	assert.True(t, result.ZoneCreated)
	assert.Equal(t, "zone-ts", result.ZoneID)
	assert.False(t, result.PoliciesReady)
	assert.False(t, result.OK())

	hasPolicyErr := false
	for _, e := range result.Errors {
		if e.Step == "policies" {
			hasPolicyErr = true
		}
	}
	assert.True(t, hasPolicyErr, "expected policies error in result")
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

	hasUDAPIErr := false
	for _, e := range result.Errors {
		if e.Step == "udapi" {
			hasUDAPIErr = true
		}
	}
	assert.True(t, hasUDAPIErr, "expected udapi error in result")
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
