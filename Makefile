# vpn-pack — Tailscale for Ubiquiti Cloud Gateway devices
#
# Usage:
#   make build              — fetch source + patch + cross-compile for ARM64
#   make package            — create deployment archive
#   make deploy HOST=<ip>   — deploy to device via SSH
#   make release            — create GitHub release (requires gh CLI + git tag)
#   make clean              — remove build artifacts
#   make patch              — apply patches only (no build)
#   make verify-patches     — dry-run patch application
#   make fetch-tailscale    — clone/checkout Tailscale source

VPNPACK_VERSION   := $(shell cat VERSION 2>/dev/null || echo "0.0.0-dev")
TAILSCALE_VERSION := 1.94.2

TAILSCALE_SRC     := reference/tailscale
BUILD_DIR         := build
PATCHED_SRC       := $(BUILD_DIR)/tailscale-src
DIST_DIR          := $(BUILD_DIR)/dist
PACKAGE_NAME      := vpn-pack
ARCHIVE_NAME      := $(PACKAGE_NAME)-$(VPNPACK_VERSION)

MANAGER_DIR       := manager
UI_DIR            := $(MANAGER_DIR)/ui

GOOS              := linux
GOARCH            := arm64
GOARM64           := v8.0,crypto
CGO_ENABLED       := 0
GO                := go

GIT_COMMIT        := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_DATE        := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
GITHUB_REPO       := eds-ch/vpn-pack

BUILD_TAGS        := ts_package_unifi,ts_omit_ace,ts_omit_acme,ts_omit_appconnectors,ts_omit_aws,ts_omit_bird,ts_omit_capture,ts_omit_captiveportal,ts_omit_clientupdate,ts_omit_cloud,ts_omit_completion,ts_omit_dbus,ts_omit_debugeventbus,ts_omit_debugportmapper,ts_omit_desktop_sessions,ts_omit_drive,ts_omit_hujsonconf,ts_omit_identityfederation,ts_omit_kube,ts_omit_lazywg,ts_omit_linuxdnsfight,ts_omit_netlog,ts_omit_networkmanager,ts_omit_oauthkey,ts_omit_outboundproxy,ts_omit_portlist,ts_omit_posture,ts_omit_qrcodes,ts_omit_resolved,ts_omit_serve,ts_omit_synology,ts_omit_syspolicy,ts_omit_systray,ts_omit_taildrop,ts_omit_tap,ts_omit_tpm,ts_omit_useproxy,ts_omit_wakeonlan,ts_omit_webclient

VERSION_LONG      := $(TAILSCALE_VERSION)-vpnpack$(VPNPACK_VERSION)-g$(GIT_COMMIT)
LDFLAGS           := -s -w -X tailscale.com/version.longStamp=$(VERSION_LONG) \
                     -X tailscale.com/version.shortStamp=$(TAILSCALE_VERSION)
GOFLAGS           := -trimpath -tags $(BUILD_TAGS) -ldflags "$(LDFLAGS)"

MANAGER_LDFLAGS   := -s -w -X main.version=$(VPNPACK_VERSION) \
                     -X main.tailscaleVersion=$(TAILSCALE_VERSION) \
                     -X main.gitCommit=$(GIT_COMMIT) \
                     -X main.buildDate=$(BUILD_DATE) \
                     -X main.githubRepo=$(GITHUB_REPO)

.PHONY: build patch package deploy clean verify-patches fetch-tailscale ui-build manager-build checksums release check check-go check-ui ui-stub

# ── Checks (lint + test) ──────────────────────────────────────────

check: check-go check-ui

check-go: fetch-tailscale ui-stub
	@echo "==> Running go vet..."
	cd $(MANAGER_DIR) && $(GO) vet ./...
	@echo "==> Running golangci-lint..."
	cd $(MANAGER_DIR) && golangci-lint run --config ../.golangci.yml ./...
	@echo "==> Running go test..."
	cd $(MANAGER_DIR) && $(GO) test -race -count=1 ./...
	@echo "==> All Go checks passed."

check-ui:
	@echo "==> Installing UI dependencies..."
	cd $(UI_DIR) && npm ci
	@echo "==> Running svelte-check..."
	cd $(UI_DIR) && npx svelte-check
	@echo "==> Running vitest..."
	cd $(UI_DIR) && npx vitest run
	@echo "==> All UI checks passed."

ui-stub:
	@if [ ! -f $(UI_DIR)/dist/index.html ]; then \
		echo "==> Creating UI stub for go:embed..."; \
		mkdir -p $(UI_DIR)/dist; \
		echo '<!doctype html>' > $(UI_DIR)/dist/index.html; \
	fi

# ── Fetch Tailscale source ───────────────────────────────────────

fetch-tailscale:
	@if [ -d "$(TAILSCALE_SRC)/.git" ]; then \
		CURRENT=$$(cd $(TAILSCALE_SRC) && git describe --tags --exact-match HEAD 2>/dev/null || echo "none"); \
		if [ "$$CURRENT" = "v$(TAILSCALE_VERSION)" ]; then \
			echo "==> Tailscale v$(TAILSCALE_VERSION) already checked out."; \
			exit 0; \
		fi; \
		echo "==> Switching Tailscale to v$(TAILSCALE_VERSION) (was $$CURRENT)..."; \
		cd $(TAILSCALE_SRC) && git fetch --tags && git checkout v$(TAILSCALE_VERSION); \
	else \
		echo "==> Cloning Tailscale v$(TAILSCALE_VERSION)..."; \
		rm -rf $(TAILSCALE_SRC); \
		git clone --branch v$(TAILSCALE_VERSION) --depth 1 \
			https://github.com/tailscale/tailscale.git $(TAILSCALE_SRC); \
	fi

# ── Build ──────────────────────────────────────────────────────────

build: fetch-tailscale patch manager-build
	@echo "==> Building tailscaled for $(GOOS)/$(GOARCH)..."
	cd $(PATCHED_SRC) && \
		GOOS=$(GOOS) GOARCH=$(GOARCH) GOARM64=$(GOARM64) CGO_ENABLED=$(CGO_ENABLED) \
		$(GO) build $(GOFLAGS) -o ../../$(BUILD_DIR)/tailscaled ./cmd/tailscaled
	@echo "==> Building tailscale CLI for $(GOOS)/$(GOARCH)..."
	cd $(PATCHED_SRC) && \
		GOOS=$(GOOS) GOARCH=$(GOARCH) GOARM64=$(GOARM64) CGO_ENABLED=$(CGO_ENABLED) \
		$(GO) build $(GOFLAGS) -o ../../$(BUILD_DIR)/tailscale ./cmd/tailscale
	@echo "==> Build complete:"
	@ls -lh $(BUILD_DIR)/tailscale $(BUILD_DIR)/tailscaled $(BUILD_DIR)/vpn-pack-manager

# ── UI Build ──────────────────────────────────────────────────────

ui-build:
	@echo "==> Building Svelte UI..."
	cd $(UI_DIR) && npm ci && npx vite build
	@echo "==> UI build complete: $(UI_DIR)/dist/"

# ── Manager Build ─────────────────────────────────────────────────

manager-build: ui-build
	@echo "==> Building vpn-pack-manager for $(GOOS)/$(GOARCH) (vpn-pack $(VPNPACK_VERSION), tailscale $(TAILSCALE_VERSION))..."
	cd $(MANAGER_DIR) && \
		GOOS=$(GOOS) GOARCH=$(GOARCH) GOARM64=$(GOARM64) CGO_ENABLED=$(CGO_ENABLED) \
		$(GO) build -trimpath -ldflags "$(MANAGER_LDFLAGS)" -o ../$(BUILD_DIR)/vpn-pack-manager .
	@echo "==> vpn-pack-manager build complete."

# ── Patch ──────────────────────────────────────────────────────────

patch: $(PATCHED_SRC)/.patched

$(PATCHED_SRC)/.patched: patches/*.patch
	@echo "==> Preparing patched source tree..."
	rm -rf $(PATCHED_SRC)
	@mkdir -p $(BUILD_DIR)
	cp -a $(TAILSCALE_SRC) $(PATCHED_SRC)
	@echo "==> Applying patches..."
	@for p in patches/*.patch; do \
		echo "    $$p"; \
		patch -d $(PATCHED_SRC) -p1 --no-backup-if-mismatch < $$p || exit 1; \
	done
	@touch $(PATCHED_SRC)/.patched
	@echo "==> All patches applied successfully."

verify-patches: fetch-tailscale
	@echo "==> Dry-run patch application..."
	@tmpdir=$$(mktemp -d) && \
	cp -a $(TAILSCALE_SRC) $$tmpdir/src && \
	for p in patches/*.patch; do \
		echo "    $$p"; \
		patch -d $$tmpdir/src -p1 --dry-run --no-backup-if-mismatch < $$p || exit 1; \
	done && \
	rm -rf $$tmpdir && \
	echo "==> All patches apply cleanly."

# ── Package ────────────────────────────────────────────────────────

package: build
	@echo "==> Creating deployment package..."
	rm -rf $(DIST_DIR)/$(PACKAGE_NAME)
	mkdir -p $(DIST_DIR)/$(PACKAGE_NAME)/bin
	mkdir -p $(DIST_DIR)/$(PACKAGE_NAME)/systemd
	cp $(BUILD_DIR)/tailscale $(BUILD_DIR)/tailscaled $(BUILD_DIR)/vpn-pack-manager \
		$(DIST_DIR)/$(PACKAGE_NAME)/bin/
	cp deploy/tailscaled.service deploy/tailscaled.defaults deploy/vpn-pack-manager.service \
		$(DIST_DIR)/$(PACKAGE_NAME)/systemd/
	cp deploy/nginx-vpnpack.conf \
		$(DIST_DIR)/$(PACKAGE_NAME)/
	cp deploy/install.sh deploy/uninstall.sh \
		$(DIST_DIR)/$(PACKAGE_NAME)/
	chmod +x $(DIST_DIR)/$(PACKAGE_NAME)/install.sh \
		$(DIST_DIR)/$(PACKAGE_NAME)/uninstall.sh
	@echo "$(VPNPACK_VERSION)" > $(DIST_DIR)/$(PACKAGE_NAME)/VERSION
	@echo "tailscale_version: $(TAILSCALE_VERSION)" >> $(DIST_DIR)/$(PACKAGE_NAME)/VERSION
	@echo "build_date: $$(date -u +%Y-%m-%dT%H:%M:%SZ)" >> $(DIST_DIR)/$(PACKAGE_NAME)/VERSION
	@echo "git_commit: $$(git rev-parse --short HEAD 2>/dev/null || echo 'unknown')" >> $(DIST_DIR)/$(PACKAGE_NAME)/VERSION
	cd $(DIST_DIR) && tar czf $(ARCHIVE_NAME).tar.gz $(PACKAGE_NAME)/
	@echo "==> Package created:"
	@ls -lh $(DIST_DIR)/$(ARCHIVE_NAME).tar.gz

# ── Checksums ──────────────────────────────────────────────────────

checksums: package
	@echo "==> Generating SHA256 checksums..."
	cd $(DIST_DIR) && sha256sum $(ARCHIVE_NAME).tar.gz > checksums.txt
	@cat $(DIST_DIR)/checksums.txt

# ── Release ────────────────────────────────────────────────────────

release: checksums
	@echo "==> Creating GitHub release v$(VPNPACK_VERSION)..."
	@printf 'Tailscale %s for UniFi Cloud Gateway devices.\n\n## Install\n\n```bash\ncurl -fsSL https://raw.githubusercontent.com/%s/main/get.sh | sudo bash\n```\n' \
		"$(TAILSCALE_VERSION)" "$(GITHUB_REPO)" > $(DIST_DIR)/release-notes.md
	gh release create "v$(VPNPACK_VERSION)" \
		$(DIST_DIR)/$(ARCHIVE_NAME).tar.gz \
		$(DIST_DIR)/checksums.txt \
		get.sh \
		--title "vpn-pack v$(VPNPACK_VERSION)" \
		--notes-file $(DIST_DIR)/release-notes.md
	@echo "==> Release v$(VPNPACK_VERSION) created."

# ── Deploy ─────────────────────────────────────────────────────────

deploy: package
ifndef HOST
	$(error HOST is required. Usage: make deploy HOST=192.168.1.1)
endif
	@echo "==> Deploying to root@$(HOST)..."
	scp $(DIST_DIR)/$(ARCHIVE_NAME).tar.gz root@$(HOST):/tmp/
	ssh root@$(HOST) "cd /tmp && \
		rm -rf $(PACKAGE_NAME) && \
		tar xzf $(ARCHIVE_NAME).tar.gz && \
		cd $(PACKAGE_NAME) && \
		bash install.sh"

# ── Clean ──────────────────────────────────────────────────────────

clean:
	rm -rf $(BUILD_DIR)
	@echo "==> Build directory cleaned."
