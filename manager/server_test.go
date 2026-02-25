package main

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteJSON(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		value      any
		wantBody   string
		wantStatus int
	}{
		{
			name:       "200 with map",
			status:     200,
			value:      map[string]string{"key": "value"},
			wantBody:   `{"key":"value"}`,
			wantStatus: 200,
		},
		{
			name:       "201 with struct",
			status:     201,
			value:      AppInfo{ApplicationVersion: "1.0"},
			wantBody:   `{"applicationVersion":"1.0"}`,
			wantStatus: 201,
		},
		{
			name:       "200 with nil produces null",
			status:     200,
			value:      nil,
			wantBody:   `null`,
			wantStatus: 200,
		},
		{
			name:       "500 with error object",
			status:     500,
			value:      map[string]string{"error": "something broke"},
			wantBody:   `{"error":"something broke"}`,
			wantStatus: 500,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			writeJSON(w, tt.status, tt.value)

			assert.Equal(t, "application/json", w.Header().Get("Content-Type"))
			assert.Equal(t, tt.wantStatus, w.Code)

			var got, want any
			require.NoError(t, json.Unmarshal([]byte(tt.wantBody), &want))
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
			assert.Equal(t, want, got)
		})
	}
}

func TestWriteError(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		msg        string
		wantStatus int
	}{
		{
			name:       "400 bad request",
			status:     400,
			msg:        "invalid input",
			wantStatus: 400,
		},
		{
			name:       "500 internal error",
			status:     500,
			msg:        "database connection failed",
			wantStatus: 500,
		},
		{
			name:       "404 not found",
			status:     404,
			msg:        "resource not found",
			wantStatus: 404,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			writeError(w, tt.status, tt.msg)

			assert.Equal(t, "application/json", w.Header().Get("Content-Type"))
			assert.Equal(t, tt.wantStatus, w.Code)

			var body map[string]string
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
			assert.Equal(t, tt.msg, body["error"])
			assert.Len(t, body, 1, "response should only contain 'error' key")
		})
	}
}
