package main

import (
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHumanizeLocalAPIError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		want string
	}{
		{"nil", nil, ""},
		{"not logged in", errors.New("state: not logged in"), "Tailscale is not connected. Use the Status tab to log in first."},
		{"not running", errors.New("tailscaled not running"), "Tailscale service is stopped. Start it from the Status tab."},
		{"NeedsLogin", errors.New("backend state: NeedsLogin"), "Authentication required. Complete login in the Status tab."},
		{"already running", errors.New("already running"), "Tailscale is already connected."},
		{"deadline exceeded", errors.New("context deadline exceeded"), "Tailscale service is not responding. Check if tailscaled is running: systemctl status tailscaled"},
		{"connection refused", errors.New("connection refused"), "Cannot reach tailscaled. Verify the service is running: systemctl status tailscaled"},
		{"auth key expired", errors.New("auth key has expired"), "The auth key has expired. Generate a new one in the Tailscale admin console."},
		{"auth key invalid", errors.New("auth key is invalid"), "The auth key is not recognized. Verify it was copied correctly from the Tailscale admin console."},
		{"node already registered", errors.New("node already registered"), "This device is already registered to a tailnet. Log out first if you want to re-register."},
		{"unknown error", errors.New("some weird thing"), "Tailscale error: some weird thing. If this persists, check logs: journalctl -u tailscaled -n 50"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := humanizeLocalAPIError(tt.err)
			assert.Equal(t, tt.want, result)
		})
	}
}

func TestWriteAPIError(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantBody   string
	}{
		{"apiError 400", &apiError{400, "bad"}, 400, "bad"},
		{"apiError 502", &apiError{502, "gw"}, 502, "gw"},
		{"generic error", errors.New("internal"), 500, "internal"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			writeAPIError(w, tt.err)
			assert.Equal(t, tt.wantStatus, w.Code)
			assert.Contains(t, w.Body.String(), tt.wantBody)
		})
	}
}
