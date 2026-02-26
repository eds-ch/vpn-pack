package main

import (
	"log/slog"
	"os"
	"strings"
)

const dpiFingerprintPath = "/proc/nf_dpi/fingerprint"

func hasDPIFingerprint() bool {
	_, err := os.Stat(dpiFingerprintPath)
	return err == nil
}

func readDPIFingerprint() bool {
	data, err := os.ReadFile(dpiFingerprintPath)
	if err != nil {
		return true
	}
	return strings.TrimSpace(string(data)) != "0"
}

func setDPIFingerprint(enabled bool) error {
	val := "1\n"
	if !enabled {
		val = "0\n"
	}
	return os.WriteFile(dpiFingerprintPath, []byte(val), 0200)
}

// syncDPIFingerprint ensures /proc/nf_dpi/fingerprint matches the desired state.
// When exit node is active, fingerprinting must be disabled to prevent
// dpi-flow-stats crashes caused by TUN interface lacking MAC addresses.
// Returns nil if DPI fingerprinting is not available (dev machine).
func syncDPIFingerprint(exitNode bool) *bool {
	if !hasDPIFingerprint() {
		return nil
	}

	current := readDPIFingerprint()
	desired := !exitNode

	if current != desired {
		if err := setDPIFingerprint(desired); err != nil {
			slog.Warn("DPI fingerprint write failed", "desired", desired, "err", err)
			return &current
		}
		if desired {
			slog.Info("DPI fingerprinting restored (exit node disabled)")
		} else {
			slog.Info("DPI fingerprinting disabled (exit node enabled)")
		}
	}

	return &desired
}
