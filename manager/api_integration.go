package main

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

type IntegrationStatus struct {
	Configured bool   `json:"configured"`
	Valid      bool   `json:"valid"`
	SiteID     string `json:"siteId,omitempty"`
	AppVersion string `json:"appVersion,omitempty"`
	Error      string `json:"error,omitempty"`
}

func loadAPIKey() string {
	data, err := os.ReadFile(apiKeyPath)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
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

func (s *Server) integrationStatusSnapshot() *IntegrationStatus {
	if s.ic == nil {
		return &IntegrationStatus{Configured: false}
	}
	if !s.ic.HasAPIKey() {
		return &IntegrationStatus{Configured: false}
	}

	st := &IntegrationStatus{Configured: true}

	if s.manifest != nil && s.manifest.SiteID != "" {
		st.SiteID = s.manifest.SiteID
		st.Valid = true
	}

	info, err := s.ic.Validate()
	if err != nil {
		st.Error = err.Error()
		st.Valid = false
	} else {
		st.Valid = true
		st.AppVersion = info.ApplicationVersion
	}

	return st
}

func (s *Server) handleIntegrationStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.integrationStatusSnapshot())
}

func (s *Server) handleSetIntegrationKey(w http.ResponseWriter, r *http.Request) {
	var req struct {
		APIKey string `json:"apiKey"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	key := strings.TrimSpace(req.APIKey)
	if key == "" {
		writeError(w, http.StatusBadRequest, "API key is required")
		return
	}

	s.ic.SetAPIKey(key)

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
		s.manifest.SiteID = siteID
		s.manifest.Save()
	}

	st := &IntegrationStatus{
		Configured: true,
		Valid:      true,
		AppVersion: info.ApplicationVersion,
		SiteID:     siteID,
	}

	slog.Info("integration API key configured", "appVersion", info.ApplicationVersion, "siteId", siteID)

	if s.fw != nil && siteID != "" {
		if err := s.fw.SetupTailscaleFirewall(); err != nil {
			slog.Warn("firewall setup after key save failed", "err", err)
		}
		if port := readTailscaledPort(); port > 0 {
			if err := s.fw.OpenWanPort(port, "tailscale-wg"); err != nil {
				slog.Warn("tailscale WAN port open after key save failed", "port", port, "err", err)
			}
		}
	}

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

	slog.Info("integration API key removed")

	s.state.mu.Lock()
	s.state.data.IntegrationStatus = &IntegrationStatus{Configured: false}
	s.state.mu.Unlock()
	s.broadcastState()

	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
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
		siteID = s.manifest.SiteID
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
