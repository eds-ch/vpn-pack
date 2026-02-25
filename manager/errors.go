package main

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
)

type apiError struct {
	status  int
	message string
}

func (e *apiError) Error() string { return e.message }

func writeAPIError(w http.ResponseWriter, err error) {
	var ae *apiError
	if errors.As(err, &ae) {
		writeError(w, ae.status, ae.message)
		return
	}
	writeError(w, http.StatusInternalServerError, err.Error())
}

func humanizeLocalAPIError(err error) string {
	if err == nil {
		return ""
	}

	msg := err.Error()
	lower := strings.ToLower(msg)

	switch {
	case strings.Contains(lower, "not logged in"):
		return "Tailscale is not connected. Use the Status tab to log in first."
	case strings.Contains(lower, "not running"):
		return "Tailscale service is stopped. Start it from the Status tab."
	case strings.Contains(lower, "backend state: needslogin"):
		return "Authentication required. Complete login in the Status tab."
	case strings.Contains(lower, "already running"):
		return "Tailscale is already connected."
	case strings.Contains(lower, "timeout") || strings.Contains(lower, "deadline exceeded"):
		return "Tailscale service is not responding. Check if tailscaled is running: systemctl status tailscaled"
	case strings.Contains(lower, "connection refused"):
		return "Cannot reach tailscaled. Verify the service is running: systemctl status tailscaled"
	case strings.Contains(lower, "auth key") && strings.Contains(lower, "expired"):
		return "The auth key has expired. Generate a new one in the Tailscale admin console."
	case strings.Contains(lower, "auth key") && strings.Contains(lower, "invalid"):
		return "The auth key is not recognized. Verify it was copied correctly from the Tailscale admin console."
	case strings.Contains(lower, "node already registered"):
		return "This device is already registered to a tailnet. Log out first if you want to re-register."
	default:
		return fmt.Sprintf("Tailscale error: %s. If this persists, check logs: journalctl -u tailscaled -n 50", msg)
	}
}
