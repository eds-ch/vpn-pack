package main

import (
	"errors"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type IntegrationStatus struct {
	Configured bool   `json:"configured"`
	Valid      bool   `json:"valid"`
	SiteID     string `json:"siteId,omitempty"`
	AppVersion string `json:"appVersion,omitempty"`
	Error      string `json:"error,omitempty"`
	Reason     string `json:"reason,omitempty"`
	ZBFEnabled *bool  `json:"zbfEnabled,omitempty"`
}

func loadAPIKey() string {
	return readFileTrimmed(apiKeyPath)
}

func saveAPIKey(key string) error {
	if err := os.MkdirAll(filepath.Dir(apiKeyPath), dirPerm); err != nil {
		return err
	}
	return os.WriteFile(apiKeyPath, []byte(key), secretPerm)
}

func deleteAPIKey() error {
	if err := os.Remove(apiKeyPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (s *Server) fetchIntegrationStatus() *IntegrationStatus {
	if s.ic == nil {
		return &IntegrationStatus{Configured: false}
	}
	if !s.ic.HasAPIKey() {
		return &IntegrationStatus{Configured: false}
	}

	s.integrationCacheMu.Lock()
	if s.integrationCache != nil && time.Since(s.integrationCacheAt) < integrationCacheTTL {
		cached := s.integrationCache
		s.integrationCacheMu.Unlock()
		return cached
	}
	s.integrationCacheMu.Unlock()

	st := &IntegrationStatus{Configured: true}

	if s.manifest != nil && s.manifest.HasSiteID() {
		st.SiteID = s.manifest.GetSiteID()
		st.Valid = true
	}

	info, err := s.ic.Validate()
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
		st.AppVersion = info.ApplicationVersion
	}

	if st.Valid && st.SiteID != "" {
		_, _, err := s.ic.findSystemZoneIDs(st.SiteID)
		enabled := err == nil
		st.ZBFEnabled = &enabled
	}

	s.integrationCacheMu.Lock()
	s.integrationCache = st
	s.integrationCacheAt = time.Now()
	s.integrationCacheMu.Unlock()

	return st
}

func (s *Server) invalidateIntegrationCache() {
	s.integrationCacheMu.Lock()
	s.integrationCache = nil
	s.integrationCacheMu.Unlock()
}

func (s *Server) handleIntegrationStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.fetchIntegrationStatus())
}

func (s *Server) handleSetIntegrationKey(w http.ResponseWriter, r *http.Request) {
	var req struct {
		APIKey string `json:"apiKey"`
	}
	if err := readJSON(w, r, &req); err != nil {
		return
	}

	key := strings.TrimSpace(req.APIKey)
	if key == "" {
		writeError(w, http.StatusBadRequest, "API key is required")
		return
	}

	s.ic.SetAPIKey(key)
	s.invalidateIntegrationCache()

	info, err := s.ic.Validate()
	if err != nil {
		s.ic.SetAPIKey("")
		writeError(w, http.StatusBadRequest, "API key validation failed: "+err.Error())
		return
	}

	if err := saveAPIKey(key); err != nil {
		slog.Warn("failed to save API key", "err", err)
		writeError(w, http.StatusInternalServerError, "failed to save API key")
		return
	}

	siteID, err := s.ic.DiscoverSiteID()
	if err != nil {
		slog.Warn("site discovery failed", "err", err)
	} else if s.manifest != nil {
		s.manifest.SetSiteID(siteID)
		if err := s.manifest.Save(); err != nil {
			slog.Warn("manifest save failed", "err", err)
		}
	}

	st := &IntegrationStatus{
		Configured: true,
		Valid:      true,
		AppVersion: info.ApplicationVersion,
		SiteID:     siteID,
	}

	if siteID != "" {
		_, _, err := s.ic.findSystemZoneIDs(siteID)
		enabled := err == nil
		st.ZBFEnabled = &enabled
	}

	slog.Info("integration API key configured", "appVersion", info.ApplicationVersion, "siteId", siteID)

	if s.fw != nil && siteID != "" {
		if err := s.fw.SetupTailscaleFirewall(); err != nil {
			slog.Warn("firewall setup after key save failed", "err", err)
		}
		s.openTailscaleWanPort()
	}

	s.integrationDegraded.Store(false)

	s.state.mu.Lock()
	s.state.data.IntegrationStatus = st
	s.state.mu.Unlock()
	s.broadcastState()

	writeJSON(w, http.StatusOK, st)
}

func (s *Server) handleDeleteIntegrationKey(w http.ResponseWriter, r *http.Request) {
	if err := deleteAPIKey(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to remove API key")
		return
	}

	if s.ic != nil {
		s.ic.SetAPIKey("")
	}
	s.invalidateIntegrationCache()

	slog.Info("integration API key removed")

	s.state.mu.Lock()
	s.state.data.IntegrationStatus = &IntegrationStatus{Configured: false}
	s.state.mu.Unlock()
	s.broadcastState()

	writeOK(w)
}

func (s *Server) handleTestIntegrationKey(w http.ResponseWriter, r *http.Request) {
	if s.ic == nil || !s.ic.HasAPIKey() {
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":    false,
			"error": "no API key configured",
		})
		return
	}

	info, err := s.ic.Validate()
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":    false,
			"error": err.Error(),
		})
		return
	}

	siteID := ""
	if s.manifest != nil {
		siteID = s.manifest.GetSiteID()
	}
	if siteID == "" {
		if id, err := s.ic.DiscoverSiteID(); err == nil {
			siteID = id
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":         true,
		"siteId":     siteID,
		"appVersion": info.ApplicationVersion,
	})
}
