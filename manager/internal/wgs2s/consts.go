package wgs2s

import "time"

const (
	// NAT traversal keepalive interval (seconds); standard value to maintain UDP hole-punch.
	defaultPersistentKeepalive = 25

	// WireGuard interface MTU; accounts for WG overhead (60 bytes) below standard 1500 Ethernet MTU.
	defaultMTU = 1420

	// Default kernel route metric for S2S routes in main table.
	// Wins over default route (metric 0 connected routes excluded) but loses to connected routes.
	defaultRouteMetric = 100

	// Peer is considered connected if last handshake was within this duration.
	// Matches the WireGuard rekey interval (REKEY_AFTER_TIME).
	handshakeTimeout = 3 * time.Minute
)
