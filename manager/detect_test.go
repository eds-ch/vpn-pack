package main

import (
	"testing"

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
