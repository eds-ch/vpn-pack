package service

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"unifi-tailscale/manager/config"
	"unifi-tailscale/manager/domain"
)

var ErrUnauthorized = errors.New("unauthorized")

// Dependency interfaces — minimal subsets, satisfied by adapters via duck typing.

type IntegrationServiceIC interface {
	SetAPIKey(key string)
	HasAPIKey() bool
	Validate(ctx context.Context) (appVersion string, err error)
	DiscoverSiteID(ctx context.Context) (string, error)
	FindSystemZoneIDs(ctx context.Context, siteID string) (string, string, error)
}

type IntegrationServiceManifest interface {
	GetSiteID() string
	HasSiteID() bool
	SetSiteID(siteID string) error
}

type IntegrationNotifier interface {
	OnBeforeKeyDelete(ctx context.Context)
	OnKeyConfigured(ctx context.Context, st *IntegrationStatus)
	OnKeyDeleted()
}

type KeyStore interface {
	Save(key string) error
	Delete() error
}

// MemKeyStore is a no-op KeyStore for use in tests.
type MemKeyStore struct{}

func (MemKeyStore) Save(string) error { return nil }
func (MemKeyStore) Delete() error     { return nil }

// Types — exported for use in HTTP handlers and SSE state.

type IntegrationStatus = domain.IntegrationStatus

type TestKeyResult struct {
	OK         bool   `json:"ok"`
	Error      string `json:"error,omitempty"`
	SiteID     string `json:"siteId,omitempty"`
	AppVersion string `json:"appVersion,omitempty"`
}

// IntegrationService encapsulates integration API key management logic.
type IntegrationService struct {
	ic       IntegrationServiceIC
	manifest IntegrationServiceManifest
	notify   IntegrationNotifier
	keyStore KeyStore
	cache    integrationCache
}

func NewIntegrationService(ic IntegrationServiceIC, manifest IntegrationServiceManifest, notify IntegrationNotifier, ks KeyStore) *IntegrationService {
	return &IntegrationService{ic: ic, manifest: manifest, notify: notify, keyStore: ks}
}

func (svc *IntegrationService) GetStatus(ctx context.Context) *IntegrationStatus {
	if svc.ic == nil || !svc.ic.HasAPIKey() {
		return &IntegrationStatus{Configured: false}
	}

	if cached := svc.cache.get(cacheTTL); cached != nil {
		return cached
	}

	st := &IntegrationStatus{Configured: true}

	if svc.manifest != nil && svc.manifest.HasSiteID() {
		st.SiteID = svc.manifest.GetSiteID()
		st.Valid = true
	}

	appVersion, err := svc.ic.Validate(ctx)
	if err != nil {
		st.Valid = false
		if errors.Is(err, ErrUnauthorized) {
			st.Error = "API key is no longer valid. This may happen after a factory reset. Please enter a new API key."
			st.Reason = "key_expired"
		} else {
			st.Error = err.Error()
		}
	} else {
		st.Valid = true
		st.AppVersion = appVersion
	}

	if st.Valid && st.SiteID != "" {
		st.ZBFEnabled = svc.checkZBFEnabled(ctx, st.SiteID)
	}

	svc.cache.set(st)
	return st
}

func (svc *IntegrationService) SetKey(ctx context.Context, key string) (*IntegrationStatus, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, validationError("API key is required")
	}

	svc.ic.SetAPIKey(key)
	svc.cache.invalidate()

	appVersion, err := svc.ic.Validate(ctx)
	if err != nil {
		svc.ic.SetAPIKey("")
		svc.cache.invalidate()
		return nil, validationError("API key validation failed: " + err.Error())
	}

	if err := svc.keyStore.Save(key); err != nil {
		slog.Warn("failed to save API key", "err", err)
		return nil, internalError("failed to save API key")
	}

	siteID, err := svc.ic.DiscoverSiteID(ctx)
	if err != nil {
		slog.Warn("site discovery failed", "err", err)
	} else if svc.manifest != nil {
		if err := svc.manifest.SetSiteID(siteID); err != nil {
			slog.Warn("manifest save failed", "err", err)
		}
	}

	st := &IntegrationStatus{
		Configured: true,
		Valid:      true,
		AppVersion: appVersion,
		SiteID:     siteID,
	}

	if siteID != "" {
		st.ZBFEnabled = svc.checkZBFEnabled(ctx, siteID)
	}

	slog.Info("integration API key configured", "appVersion", appVersion, "siteId", siteID)

	if svc.notify != nil {
		svc.notify.OnKeyConfigured(ctx, st)
	}

	return st, nil
}

func (svc *IntegrationService) DeleteKey(ctx context.Context) error {
	if svc.notify != nil {
		svc.notify.OnBeforeKeyDelete(ctx)
	}
	if err := svc.keyStore.Delete(); err != nil {
		return err
	}
	if svc.ic != nil {
		svc.ic.SetAPIKey("")
	}
	svc.cache.invalidate()
	if svc.notify != nil {
		svc.notify.OnKeyDeleted()
	}
	return nil
}

func (svc *IntegrationService) TestKey(ctx context.Context) *TestKeyResult {
	if svc.ic == nil || !svc.ic.HasAPIKey() {
		return &TestKeyResult{OK: false, Error: "no API key configured"}
	}

	appVersion, err := svc.ic.Validate(ctx)
	if err != nil {
		return &TestKeyResult{OK: false, Error: err.Error()}
	}

	siteID := ""
	if svc.manifest != nil {
		siteID = svc.manifest.GetSiteID()
	}
	if siteID == "" {
		if id, err := svc.ic.DiscoverSiteID(ctx); err == nil {
			siteID = id
		}
	}

	return &TestKeyResult{OK: true, SiteID: siteID, AppVersion: appVersion}
}

func (svc *IntegrationService) InvalidateCache() {
	svc.cache.invalidate()
}

// --- Private helpers ---

func (svc *IntegrationService) checkZBFEnabled(ctx context.Context, siteID string) *bool {
	_, _, err := svc.ic.FindSystemZoneIDs(ctx, siteID)
	enabled := err == nil
	return &enabled
}

// --- Cache ---

type integrationCache struct {
	mu    sync.Mutex
	data  *IntegrationStatus
	setAt time.Time
}

func (c *integrationCache) get(ttl time.Duration) *IntegrationStatus {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.data != nil && time.Since(c.setAt) < ttl {
		return c.data
	}
	return nil
}

func (c *integrationCache) set(st *IntegrationStatus) {
	c.mu.Lock()
	c.data = st
	c.setAt = time.Now()
	c.mu.Unlock()
}

func (c *integrationCache) invalidate() {
	c.mu.Lock()
	c.data = nil
	c.mu.Unlock()
}

// --- File I/O ---

func LoadAPIKey() string {
	data, err := os.ReadFile(config.APIKeyPath)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// SaveAPIKey persists the API key to disk. Exported for use by fileKeyStore adapter in main package.
func SaveAPIKey(key string) error {
	if err := os.MkdirAll(filepath.Dir(config.APIKeyPath), config.DirPerm); err != nil {
		return err
	}
	return os.WriteFile(config.APIKeyPath, []byte(key), config.SecretPerm)
}

func DeleteAPIKey() error {
	if err := os.Remove(config.APIKeyPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

const cacheTTL = 30 * time.Second
