package main

import (
	"context"
	"net/http"

	"tailscale.com/ipn"
)

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	snap := s.state.snapshot()

	writeJSON(w, http.StatusOK, snap)
}

func (s *Server) handleUp(w http.ResponseWriter, r *http.Request) {
	if !s.integrationReady() {
		writeError(w, http.StatusPreconditionFailed, "Integration API key required before activating Tailscale")
		return
	}

	st, err := s.lc.Status(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, humanizeLocalAPIError(err))
		return
	}

	if st.BackendState == "NeedsLogin" {
		if err := s.disableCorpDNS(r.Context()); err != nil {
			writeError(w, http.StatusInternalServerError, humanizeLocalAPIError(err))
			return
		}
		err = s.lc.StartLoginInteractive(r.Context())
	} else {
		_, err = s.lc.EditPrefs(r.Context(), &ipn.MaskedPrefs{
			Prefs:          ipn.Prefs{WantRunning: true},
			WantRunningSet: true,
		})
	}

	if err != nil {
		writeError(w, http.StatusInternalServerError, humanizeLocalAPIError(err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleDown(w http.ResponseWriter, r *http.Request) {
	_, err := s.lc.EditPrefs(r.Context(), &ipn.MaskedPrefs{
		Prefs:          ipn.Prefs{WantRunning: false},
		WantRunningSet: true,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, humanizeLocalAPIError(err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if !s.integrationReady() {
		writeError(w, http.StatusPreconditionFailed, "Integration API key required before activating Tailscale")
		return
	}

	if err := s.disableCorpDNS(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, humanizeLocalAPIError(err))
		return
	}
	if err := s.lc.StartLoginInteractive(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, humanizeLocalAPIError(err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if err := s.lc.Logout(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, humanizeLocalAPIError(err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) disableCorpDNS(ctx context.Context) error {
	_, err := s.lc.EditPrefs(ctx, &ipn.MaskedPrefs{
		Prefs:      ipn.Prefs{CorpDNS: false},
		CorpDNSSet: true,
	})
	return err
}

func (s *Server) handleDevice(w http.ResponseWriter, r *http.Request) {
	s.vpnClientsMu.Lock()
	info := s.deviceInfo
	s.vpnClientsMu.Unlock()
	writeJSON(w, http.StatusOK, info)
}
