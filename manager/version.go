package main

// Set via -ldflags at build time.
var (
	version          = "dev"
	tailscaleVersion = "unknown"
	gitCommit        = "unknown"
	buildDate        = "unknown"
	githubRepo       = "eds-ch/vpn-pack"
)
