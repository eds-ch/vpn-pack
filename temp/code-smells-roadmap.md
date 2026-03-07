# Code Smells Refactoring Roadmap

**Source:** `temp/code-smells-audit-report.md` (2026-03-06, 66 findings)
**Approach:** Wave-based execution. Architectural solutions that close multiple findings at once. No over-engineering.

---

## Principles

1. **One change — many fixes.** Prefer architectural solutions that close a class of problems over point fixes.
2. **Safe ordering.** Deletions first, then moves, then new abstractions. Each wave leaves code compilable and tests passing.
3. **No speculative abstraction.** Every extraction has 3+ concrete call sites. No "future-proof" wrappers.
4. **Parallelizable.** Frontend and backend waves are independent. Within backend, waves are sequential.

---

## Wave 1 — Dead Code Sweep ✅

**Effort:** 30 min | **Findings closed:** D1, D2, D3, D5, D7, D8, S13 (7 findings)

Mechanical deletions. Zero risk. Run `go test ./...` after each step.

| # | File | Action | Finding | Status |
|---|------|--------|---------|--------|
| 1 | `errors.go` | Deleted entire file (+ `errors_test.go`) — all contents were dead code | D1 | Done |
| 2 | `errors.go` | (included in #1) | D2 | Done |
| 3 | `domain/results.go` | Deleted `SSEStatusEvent` alias | D3 | Done |
| 4 | `detect.go` | Removed `readFileString`, merged null-byte handling into `readFileTrimmed`, updated caller | D5 | Done |
| 5 | `service/integration.go` | Inlined `deleteAPIKey()` body into `DeleteAPIKey()` | D7 | Done |
| 6 | `health.go` | Removed `OverallStatus()`, updated tests to use `Snapshot().Status` | D8 | Done |
| 7 | `internal/wgs2s/manager.go` | Replaced `slicesEqual()` with `slices.Equal` | S13 | Done |

**Verify:** `cd manager && go test ./... && go vet ./...` — passed

---

## Wave 2 — Quick Safety Fixes ✅

**Effort:** 30 min | **Findings closed:** E1, E3, E9, E16 (4 findings)

Point fixes that prevent panics and cache stampede. Low risk, high value.

| # | File | Action | Finding | Status |
|---|------|--------|---------|--------|
| 1 | `handlers.go:20` | Check `fs.Sub` error: `sub, err := fs.Sub(...)` → `slog.Error` + `os.Exit(1)` on error | E1 | Done |
| 2 | `firewall.go:379-399` | Add `singleflight.Group` to `cachedFilterRules` — prevents concurrent `iptables-save` forks on ARM | E3 | Done |
| 3 | `sse/hub.go:34-42` | Add `sync.Once` to `unsubscribe` to prevent double-close panic | E9 | Done |
| 4 | `sse/hub.go:73` | Safe type assertion: `data, ok := v.([]byte)` with fallback | E16 | Done |

**Verify:** `cd manager && go test ./... && go vet ./...` — passed

---

## Wave 3 — Service Error Infrastructure ✅

**Effort:** 30 min | **Findings closed:** A6, S15 (2 findings) + improves E6

Structural move: error types belong in their own file, not buried in settings.go.

| # | Action | Finding | Status |
|---|--------|---------|--------|
| 1 | Create `service/errors.go` | A6 | Done |
| 2 | Move from `service/settings.go`: `Error` struct, `ErrorKind` type, all `ErrorKind` constants, `validationError()`, `upstreamError()`, `internalError()` constructors | A6 | Done |
| 3 | Add missing constructors: `preconditionError(msg)`, `notFoundError(msg)` — replaced 7 inline `&Error{...}` literals. Skipped `unavailableError` (zero usages). | A6 | Done |
| 4 | Verified `errIntegrationNotConfigured` — identical in both packages, can't share, added cross-reference comments | S15 | Done |

**Verify:** `cd manager && go test ./... && go vet ./...` — passed

---

## Wave 4 — Firewall Setup Deduplication ✅

**Effort:** 1h | **Findings closed:** S2, S3, S7 (3 findings)

Extract rollback helpers from `service/firewall.go` to eliminate 4-5 copies of the same pattern.

| # | Action | Finding | Status |
|---|--------|---------|--------|
| 1 | Add `SetupResult.resetZone()` method — zeroes `ZoneCreated`, `ZoneID`, `ZoneName` | S2 | Done |
| 2 | Add `SetupResult.resetPolicies()` method — zeroes `PoliciesReady`, `PolicyIDs` | S2 | Done |
| 3 | Replace all inline rollback blocks in `SetupTailscaleFirewall` and `SetupWgS2sZone` with `result.resetZone()` / `result.resetPolicies()` calls | S2, S7 | Done |
| 4 | Extract `logFirewallError(iface, err)` helper in `service/wgs2s.go` — replaces 3 identical dual-logging blocks | S3 | Done |

**Verify:** `cd manager && go test ./... && go vet ./...` — passed; `make build` — passed

---

## Wave 5 — Domain Type Ownership (Dependency Inversion) ✅

**Effort:** 2h | **Findings closed:** A3 (1 finding, architectural)

Fix `domain/` → `internal/wgs2s` import inversion. Domain must be a leaf.

| # | Action | Status |
|---|--------|--------|
| 1 | Created `domain/wgs2s_types.go` with domain-owned `TunnelConfig`, `WgS2sStatus` (`TunnelState` doesn't exist) | Done |
| 2 | Updated `domain/interfaces.go` (`WgS2sControl`) and `domain/types.go` (`StateData.WgS2sTunnels`) to use domain types | Done |
| 3 | Updated `internal/wgs2s/config.go` and `status.go` — replaced struct definitions with type aliases from `domain/` | Done |
| 4 | No consumer changes needed — type aliases make `wgs2s.TunnelConfig` identical to `domain.TunnelConfig` | Done |
| 5 | Removed `internal/wgs2s` import from `domain/` — verified with `go vet` and `grep` | Done |
| 6 | `aliases.go` — no changes needed (no wgs2s type aliases existed) | Done |

**Verify:** `cd manager && go test ./... && go vet ./...` — passed

---

## Wave 6 — Frontend: UI Primitives ✅

**Effort:** 2h | **Findings closed:** F1, F2, F4 (3 findings) + partially F3

Extract reusable components to eliminate ~200+ lines of duplication.

| # | Action | Finding | Status |
|---|--------|---------|--------|
| 1 | Created `FormField.svelte` — label + input + error wrapper with `$bindable` value, error, extraClass, onpaste props | F1 | Done |
| 2 | Replaced 9 inline fields in `TunnelCard.svelte` with `<FormField>` | F1 | Done |
| 3 | Replaced 7 inline fields in `TunnelForm.svelte` with `<FormField>` (kept listenPort WAN hint and textarea inline) | F1 | Done |
| 4 | Created `Button.svelte` — primary/secondary/ghost variants, sm/md sizes, Svelte 5 `{@render children()}` | F2 | Done |
| 5 | Replaced ~13 inline buttons across TunnelCard, TunnelForm, SetupRequired, SettingsIntegration, SettingsTab | F2 | Done |
| 6 | Extracted `{#snippet desktopTab}` and `{#snippet mobileTab}` in `SettingsTab.svelte`, replacing 6 identical `{#each}` bodies | F4, F3 | Done |

**Verify:** `cd manager/ui && npm run build && npx svelte-check && npm test` — passed (0 errors, 0 warnings, 165/165 tests)

---

## Wave 7 — Frontend: Typed API Layer ✅

**Effort:** 2-3h | **Findings closed:** F7, F8, F9 (3 findings)

Add TypeScript types to the API module and store.

| # | Action | Finding | Status |
|---|--------|---------|--------|
| 1 | Created `lib/types.ts` — 30+ interfaces for all API response shapes matching Go backend types | F7, F8 | Done |
| 2 | Renamed `lib/api.js` → `lib/api.ts` — added generic `apiFetch<T>` with return types to all 27 exported functions | F8 | Done |
| 3 | Fixed empty `catch {}` in CSRF retry → `catch (e) { console.warn('CSRF refresh failed:', e); }` | F9 | Done |
| 4 | Added JSDoc `@type` annotations to `status`, `errors`, `updateInfo` in `stores/tailscale.svelte.js` | F7 | Done |

**Verify:** `cd manager/ui && npx svelte-check && npm run build && npm test` — passed (0 errors, 0 warnings, 165/165 tests)

---

## Wave 8 — Remaining Error Handling ✅

**Effort:** 1-2h | **Findings closed:** E2, E6, E7, E8, E10, E11, E17 (7 findings)

Mop-up: add logging to swallowed errors, track goroutines, wrap bare returns.

| # | File | Action | Finding | Status |
|---|------|--------|---------|--------|
| 1 | `server.go`, `watcher.go` | Log manifest write errors at `slog.Warn` instead of `_ =` (9 sites) | E2 | Done |
| 2 | `service/tailscale.go` | Wrap error: `fmt.Errorf("disable corp DNS: %w", err)` | E6 | Done |
| 3 | `server.go` | Add `context.WithTimeout(ctx, 30*time.Second)` to `restartTailscaled` goroutine, use `exec.CommandContext` | E7 | Done |
| 4 | `internal/wgs2s/manager.go` | `RestoreAll`: accumulate errors with `errors.Join` instead of returning only last | E8 | Done |
| 5 | `firewall.go` | `EnsureTailscaleRules`: return ipset error instead of swallowing | E10 | Done |
| 6 | `internal/wgs2s/routes.go` | `deleteRoutes`: log `buildRouteMessage` errors | E11 | Done |
| 7 | `firewall.go`, `adapters.go`, `service/settings.go`, `domain/interfaces.go` | `RestoreRulesWithRetry` spawns goroutine internally with `sync.WaitGroup`; `WaitBackground()` called in graceful shutdown | E17 | Done |

**Verify:** `cd manager && go test ./... && go vet ./...` — passed

---

## Wave 9 — Remaining Structural & Low-Priority ✅

**Effort:** 2-3h | **Findings closed:** S4, S5, S14, D4, D9, D10, D11 (7 findings) | **Skipped:** S8 (leave as-is), D6 (contradicts "What NOT to refactor")

Lower priority structural improvements. Can be done incrementally.

| # | File | Action | Finding | Status |
|---|------|--------|---------|--------|
| 1 | `internal/wgs2s/manager.go` | Extracted `applyUpdates()` from `UpdateTunnel` 25-line field merge | S4 | Done |
| 2 | `service/wgs2s.go` | Grouped 7 constructor params into `WgS2sConfig` struct | S5 | Done |
| 3 | `service/settings.go` | Left as-is — special-case logic makes descriptor loop more complex | S8 | Skipped |
| 4 | `watcher.go` | Extracted `buildDERPInfo` helper from `processNetMap` | S14 | Done |
| 5 | `adapters.go` | Extracted `toZoneManifestData` converter, replaced 3 literals | D4 | Done |
| 6 | `handlers.go` | Left as-is — contradicts "What NOT to refactor" section | D6 | Skipped |
| 7 | `service/settings.go`, `watcher.go` | Extracted `isDNSForwardingEnabled()` method on each struct, replaced 4 computations | D9 | Done |
| 8 | `firewall.go`, `cleanup.go` | Defined `const wgS2sMarkerPrefix = "wg-s2s-manager:"`, replaced 3 literals | D10 | Done |
| 9 | `service/wgs2s.go` | Simplified `EnableTunnel` — removed redundant nil check on re-fetched tunnel | D11 | Done |

**Verify:** `cd manager && go test ./... && go vet ./...` — passed

---

## Wave 10 — Remaining Frontend & Low-Priority ✅

**Effort:** 1-2h | **Findings closed:** F5, F6, F10, F11, F12, F13, F14, F15 (8 findings)

| # | File | Action | Finding | Status |
|---|------|--------|---------|--------|
| 1 | `SettingsTab.svelte` | Exported `valuesEqual` from store, replaced 3x `JSON.stringify` | F5 | Done |
| 2 | `SettingsGeneral.svelte`, `LogsTab.svelte` | Replaced inline SVGs with `<Icon>` component; added `info` icon to `icons.js` | F6 | Done |
| 3 | `ConnectionFlow.svelte` | Added `$effect` cleanup return to clear `setTimeout` on destroy | F10 | Done |
| 4 | `stores/tailscale.svelte.js` | Extracted `const LOG_CAP = 500` | F11 | Done |
| 5 | `Sidebar.svelte` | Added `--bg-tooltip` theme token, replaced 4x `bg-[#1c1e21]` with `bg-tooltip` | F12 | Done |
| 6 | `icons.js` | Added missing `download` icon (used by `TopBar.svelte`) | F13 | Done |
| 7 | `AuthKeyInput.svelte` | Already has self-contained `loading`/`error`/`authKey` state — no changes needed | F14 | Done |
| 8 | `ApiKeyForm.svelte` | Correctly scoped as reusable field component — no changes needed | F15 | Done |

**Verify:** `cd manager/ui && npm run build && npx svelte-check && npm test` — passed (0 errors, 0 warnings, 165/165 tests)

---

## Wave 11 — Architecture & Infra (Optional)

**Effort:** varies | **Findings closed:** A4, A5, A7, A8, E4, E5, E12, E13, E14, E15, E18, A10-A15 (remaining)

Lower priority. Do when touching these areas naturally.

| # | Action | Finding |
|---|--------|---------|
| 1 | Remove no-value type aliases in `service/*.go` that just re-export `domain` types | A4 |
| 2 | Split `service/settings.go` (583 lines): extract file I/O or prefs-building into helper | A5 |
| 3 | `firewall.go`: extract shell-out functions behind interface for testability | A7 |
| 4 | `server.go:303-383`: move startup validation into service layer | A8 |
| 5 | `service/diagnostics.go:84`, `service/wgs2s.go:174`: use `atomic.Pointer` for `SetWgS2s`/`SetWireGuard` | E4, E5 |
| 6 | `cleanup.go:132-166`: add timeout to `context.Background()` | E12 |
| 7 | `udapi/client.go:153`: check `rand.Read` error | E13 |
| 8 | `nginx.go:25-44`: wrap errors with context | E14 |
| 9 | `dpi.go:16-21`: return `false` on read error instead of `true` | E15 |
| 10 | `handler_sse.go:28-30,45-47`: log SSE write errors at debug level | E18 |
| 11 | Various A10-A15: config validation, API versioning, Makefile optimization | A10-A15 |

---

## Parallelization Strategy

```
Wave 1 (dead code)     ──sequential──> Wave 2 (safety) ──> Wave 3 (errors.go) ──> Wave 4 (firewall)
                                                                                         │
                                                                                         v
                                                                                   Wave 5 (domain types)
                                                                                         │
                                                                                         v
                                                                              Wave 8 (error handling)
                                                                                         │
                                                                                         v
                                                                              Wave 9 (structural)
                                                                                         │
                                                                                         v
                                                                              Wave 11 (optional)

Wave 6 (UI primitives)  ──parallel with backend──> Wave 7 (typed API) ──> Wave 10 (frontend cleanup)
```

Frontend waves (6, 7, 10) can run **fully in parallel** with all backend waves — zero file overlap.

Backend waves are **sequential** within themselves: each wave builds on the previous (e.g., wave 3 creates errors.go that wave 4 may reference).

---

## Summary

| Wave | Findings | Count | Effort | Type |
|:----:|----------|:-----:|--------|------|
| 1 | D1,D2,D3,D5,D7,D8,S13 | 7 | 30 min | Delete dead code |
| 2 | E1,E3,E9,E16 | 4 | 30 min | Safety fixes |
| 3 | A6,S15 | 2 | 30 min | Error infrastructure |
| 4 | S2,S3,S7 | 3 | 1h | Firewall dedup |
| 5 | A3 | 1 | 2h | Dependency inversion |
| 6 | F1,F2,F3,F4 | 4 | 2h | UI primitives |
| 7 | F7,F8,F9 | 3 | 2-3h | Typed API |
| 8 | E2,E6,E7,E8,E10,E11,E17 | 7 | 1-2h | Error handling |
| 9 | S4,S5,S8,S14,D4,D6,D9,D10,D11 | 9 | 2-3h | Structural cleanup |
| 10 | F5,F6,F10,F11,F12,F13,F14,F15 | 8 | 1-2h | Frontend cleanup |
| 11 | A4,A5,A7,A8,E4,E5,E12-E15,E18,A10-A15 | 18 | varies | Optional / opportunistic |
| **Total** | | **66** | **~15h** | |

### What NOT to refactor

- **Root package split** (A1) — size is fine (deep modules). Splitting creates more problems than it solves.
- **Server struct sub-grouping** (S1) — 24 fields in an entry-point struct is acceptable. Sub-structs add indirection.
- **WgS2s availability middleware** (D6) — 7 one-line guards are clearer than middleware indirection.
- **Adapter abstraction** (A2/S9) — boilerplate serves a purpose (layer isolation). Don't abstract the adapter layer.
- **API versioning** (A14) — premature for an embedded device UI with no external consumers.
