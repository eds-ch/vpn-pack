# Tailscale Patches

Patches applied to upstream Tailscale source before cross-compiling for UniFi devices.

**Current target version: 1.94.1**

## Patch Files

| Patch | Purpose |
|-------|---------|
| `001-fwmark-0x800000.patch` | Change subnet route fwmark from `0x40000` to `0x800000` (avoids conflict with UniFi `wgclt1` VPN client) |
| `002-ubnt-device-model.patch` | Report DeviceModel as actual product name (e.g. "UDM-SE") instead of SoC name |
| `003-ubnt-distro-version.patch` | Report Distro="ubnt", Version=firmware version, CodeName from os-release |
| `004-package-type-unifi.patch` | Set Package="unifi-tailscale" via build tag `ts_package_unifi` |

## Verifying Patches

```bash
make verify-patches
```

Runs a dry-run application against the reference source. No files are modified. If all patches apply cleanly, you'll see:

```
==> All patches apply cleanly.
```

## Updating Tailscale Version

1. Edit `TAILSCALE_VERSION` in `Makefile` (e.g. `1.94.1` -> `1.96.0`)
2. Clean old artifacts:
   ```bash
   make clean
   ```
3. Fetch the new version:
   ```bash
   make fetch-tailscale
   ```
4. Verify patches apply:
   ```bash
   make verify-patches
   ```
5. If patches fail — see "Fixing Broken Patches" below
6. Update Go dependencies:
   ```bash
   cd manager && go mod tidy
   ```
7. Build:
   ```bash
   make build
   ```
8. Update the `Tailscale-Version` header in each patch file

## Creating a New Patch

1. Copy the reference source to a working directory:
   ```bash
   cp -a reference/tailscale /tmp/tailscale-work
   ```
2. Make your changes in `/tmp/tailscale-work/`
3. Generate the patch:
   ```bash
   diff -ruN reference/tailscale /tmp/tailscale-work > patches/NNN-description.patch
   ```
   Use the next sequential number (e.g. `005-...`).
4. Add a version header as the first two lines of the patch file:
   ```
   # Tailscale-Version: 1.94.1
   # Description: Brief description of what this patch does
   ```
5. Verify:
   ```bash
   make verify-patches
   ```
6. Build:
   ```bash
   make build
   ```

## Fixing Broken Patches

When a patch fails after a version update:

1. Identify which patch fails:
   ```bash
   make verify-patches
   ```
2. Apply patches one by one to find the breaking point:
   ```bash
   cp -a reference/tailscale /tmp/tailscale-fix
   patch -d /tmp/tailscale-fix -p1 --dry-run < patches/001-fwmark-0x800000.patch
   patch -d /tmp/tailscale-fix -p1 --dry-run < patches/002-ubnt-device-model.patch
   # ... continue until one fails
   ```
3. Apply all patches up to (but not including) the broken one:
   ```bash
   cp -a reference/tailscale /tmp/tailscale-fix
   patch -d /tmp/tailscale-fix -p1 < patches/001-fwmark-0x800000.patch
   # ... apply all good patches
   ```
4. Manually apply the intended change in `/tmp/tailscale-fix/`
5. Regenerate the patch:
   ```bash
   # Reset to state before the broken patch was applied
   cp -a reference/tailscale /tmp/tailscale-clean
   # Apply all prior patches to the clean copy
   patch -d /tmp/tailscale-clean -p1 < patches/001-fwmark-0x800000.patch
   # Generate new patch
   diff -ruN /tmp/tailscale-clean /tmp/tailscale-fix > patches/NNN-description.patch
   ```
6. Verify all patches apply cleanly:
   ```bash
   make verify-patches
   ```

## Patch Format

Each patch must apply with:

```bash
patch -d <source-dir> -p1 --no-backup-if-mismatch < patches/NNN-name.patch
```

Patches are applied in filename sort order (`001`, `002`, ...).
