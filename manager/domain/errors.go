package domain

import "errors"

var (
	ErrUnauthorized   = errors.New("integration API: unauthorized (invalid or missing API key)")
	ErrNotFound       = errors.New("integration API: resource not found")
	ErrIntegrationAPI = errors.New("integration API error")
)
