package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseUniFiVersion(t *testing.T) {
	tests := []struct {
		name       string
		raw        string
		wantMajor  int
		wantMinor  int
		wantString string
		wantErr    bool
	}{
		{"full version", "10.1.85-32713-1", 10, 1, "10.1", false},
		{"major only", "10", 10, 0, "10.0", false},
		{"two parts", "10.1", 10, 1, "10.1", false},
		{"three parts", "9.0.7", 9, 0, "9.0", false},
		{"empty", "", 0, 0, "", true},
		{"garbage", "abc.def", 0, 0, "", true},
		{"major garbage", "abc", 0, 0, "", true},
		// BUG-L4: dpkg reports installed UniFi Network with a Debian epoch
		// prefix ("1:3.2.18-21345"). Without stripping, parsing fails and
		// boot bails out as if Network were missing.
		{"debian epoch single-digit", "1:3.2.18-21345", 3, 2, "3.2", false},
		{"debian epoch multi-digit", "10:11.0.0", 11, 0, "11.0", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v, err := parseUniFiVersion(tt.raw)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantMajor, v.Major)
			assert.Equal(t, tt.wantMinor, v.Minor)
			assert.Equal(t, tt.wantString, v.String())
		})
	}
}

func TestCheckMinUniFiVersion(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		wantErr bool
		errMsg  string
	}{
		{"10.1 ok", "10.1.85-32713-1", false, ""},
		{"10.2 ok", "10.2.0", false, ""},
		{"11.0 ok", "11.0.0", false, ""},
		{"10.0 too old", "10.0.7-1234-1", true, "10.1 or later is required"},
		{"9.x too old", "9.1.100", true, "10.1 or later is required"},
		{"empty", "", true, "not found"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkMinUniFiVersion(tt.raw)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// The gate used to be a one-shot check at startup, but UniFi OS applies
// package upgrades after the manager is already running: on the 5.1.26 update
// the manager started at 12:56:40 and unifi-native was installed at 12:58:49.
// A device coming from a pre-10.1 Network would have been judged by the
// pre-upgrade version and exited 78 — which RestartPreventExitStatus makes
// permanent — although the requirement was met two minutes later.
func TestAwaitSupportedUniFiVersionWaitsOutAPackageUpgradeAfterBoot(t *testing.T) {
	reads := []string{"9.9.1-30000-1", "10.5.67-35187-1"}
	calls, slept := 0, 0

	got, err := awaitSupportedUniFiVersion("9.9.1-30000-1", unifiGateDeps{
		read: func() string {
			v := reads[min(calls, len(reads)-1)]
			calls++
			return v
		},
		uptime: func() time.Duration { return time.Minute },
		sleep:  func(time.Duration) { slept++ },
	})

	require.NoError(t, err)
	assert.Equal(t, "10.5.67-35187-1", got)
	assert.Equal(t, 2, slept, "waits one interval per re-read until the upgrade lands")
}

// Long after boot the answer cannot flip under us, so an unsupported device
// must fail immediately rather than hang every socket activation for minutes.
func TestAwaitSupportedUniFiVersionFailsFastLongAfterBoot(t *testing.T) {
	calls, slept := 0, 0

	_, err := awaitSupportedUniFiVersion("9.9.1-30000-1", unifiGateDeps{
		read:   func() string { calls++; return "9.9.1-30000-1" },
		uptime: func() time.Duration { return 2 * time.Hour },
		sleep:  func(time.Duration) { slept++ },
	})

	require.Error(t, err)
	assert.Zero(t, slept, "must not wait when the system is past its boot window")
	assert.Zero(t, calls, "must not re-read when the system is past its boot window")
}

// A genuinely unsupported device still loses, just not before the upgrade
// window has passed.
func TestAwaitSupportedUniFiVersionGivesUpAfterTheBoundedWindow(t *testing.T) {
	calls := 0

	_, err := awaitSupportedUniFiVersion("9.9.1-30000-1", unifiGateDeps{
		read:   func() string { calls++; return "9.9.1-30000-1" },
		uptime: func() time.Duration { return time.Minute },
		sleep:  func(time.Duration) {},
	})

	require.Error(t, err)
	assert.Equal(t, unifiGateAttempts, calls)
}

// An absent Network package right after boot is the same transient situation.
func TestAwaitSupportedUniFiVersionRetriesAnEmptyReadAfterBoot(t *testing.T) {
	calls := 0

	got, err := awaitSupportedUniFiVersion("", unifiGateDeps{
		read: func() string {
			calls++
			return "10.5.67-35187-1"
		},
		uptime: func() time.Duration { return time.Minute },
		sleep:  func(time.Duration) {},
	})

	require.NoError(t, err)
	assert.Equal(t, "10.5.67-35187-1", got)
	assert.Equal(t, 1, calls)
}
