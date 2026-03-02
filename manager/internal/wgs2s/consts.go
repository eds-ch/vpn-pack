package wgs2s

import "time"

const (
	// NAT traversal keepalive interval (seconds); standard value to maintain UDP hole-punch.
	defaultPersistentKeepalive = 25

	// WireGuard interface MTU; accounts for WG overhead (60 bytes) below standard 1500 Ethernet MTU.
	defaultMTU = 1420

	// Peer is considered connected if last handshake was within this duration.
	// Matches the WireGuard rekey interval (REKEY_AFTER_TIME).
	handshakeTimeout = 3 * time.Minute
)
