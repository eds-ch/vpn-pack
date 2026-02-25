package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		name string
		a, b string
		want int
	}{
		{"equal", "1.0.0", "1.0.0", 0},
		{"newer patch", "1.0.1", "1.0.0", 1},
		{"older patch", "1.0.0", "1.0.1", -1},
		{"newer minor", "1.1.0", "1.0.0", 1},
		{"newer major", "2.0.0", "1.99.99", 1},
		{"different lengths equal", "1.0", "1.0.0", 0},
		{"different lengths newer", "1.0.1", "1.0", 1},
		{"empty a", "", "1.0.0", 0},
		{"empty b", "1.0.0", "", 0},
		{"both empty", "", "", 0},
		{"dev a", "dev", "1.0.0", 0},
		{"dev b", "1.0.0", "dev", 0},
		{"real world newer", "1.95.0", "1.94.0", 1},
		{"real world equal", "1.95.0", "1.95.0", 0},
		{"real world downgrade", "1.94.0", "1.95.0", -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, compareVersions(tt.a, tt.b))
		})
	}
}
