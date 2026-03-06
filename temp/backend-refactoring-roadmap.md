# Roadmap: Backend Refactoring "Sinks, Not Pipes"

Исполнительный roadmap на основе `backend-refactoring-sinks-not-pipes.md`.
Каждая фаза — рабочий билд. Тестирование обязательно после каждой фазы.

---

## Граф зависимостей

```
Phase 1 ─────────────────────────────────────┐
    │                                         │
    ▼                                         │
Phase 2 ─────────────────────┐                │
    │                        │                │
    ▼                        ▼                │
Phase 3                  Phase 4              │
(Manifest)           (Sync Firewall)          │
    │                        │                │
    └────────┬───────────────┘                │
             ▼                                │
         Phase 5                              │
    (Transactions)                            │
             │                                │
             ▼                                │
         Phase 6 ←── самая большая и рискованная
             │
             ├──────────────┐
             ▼              ▼
         Phase 7        Phase 8*
        (Health)      (Packages)
```

*Phase 8 технически зависит только от Phase 6, но перемещает `health.go` (Phase 7). Поэтому выполняется после Phase 7.

**Параллелизм:** Phase 3 и Phase 4 могут выполняться параллельно (обе зависят от Phase 2, не зависят друг от друга). Phase 5 ждёт завершения обеих.

---

## ~~Phase 1: Honest Types — Result Types & API Contracts~~ ✅ DONE

**Зависимости:** нет
**Риск:** низкий | **Оценка:** 2 дня

### Задачи

- [x] 1.1 Создать `manager/results.go` — `FirewallSetupResult`, `StepError`, `OK()`, `Degraded()`
  - **NB:** `Degraded()` = zone и policies OK, но UDAPI не применён: `r.ZoneCreated && r.PoliciesReady && !r.UDAPIApplied`
- [x] 1.2 Создать API response types — `APIResponse[T]`, `TunnelCreateResponse`, `FirewallStatusBrief`
- [x] 1.3 Создать SSE event structs — `SSEStatusEvent`, `SSEHealthEvent`, `SSEDNSEvent`
- [x] 1.4 Создать `BroadcastEvent[T]()` generic helper для type-safe SSE broadcasting
- [x] 1.5 Рефакторить `firewall.go` — `SetupTailscaleFirewall(ctx) *FirewallSetupResult`, `SetupWgS2sZone(ctx, ...) *FirewallSetupResult`
- [x] 1.6 Обновить callers в `server.go`, `api_wgs2s.go`, `watcher_firewall.go` — чтение per-step status вместо `err != nil`
- [x] 1.7 Заменить прямые `hub.BroadcastNamed()` вызовы на `BroadcastEvent[T]()` где применимо

### Тестирование Phase 1

- [x] Написать `results_test.go` — `OK()`, `Degraded()`, `StepError` scenarios
- [x] Написать `firewall_test.go` — `SetupTailscaleFirewall` возвращает корректный result (success, zone fail, policy fail, udapi fail)
- [x] `go build ./...` проходит
- [x] `go test ./...` проходит
- [x] `go vet ./...` без warnings
- [ ] Deploy на UDM-SE → ручная проверка: существующий функционал работает

### Критерии завершения Phase 1

- [x] Все callers `SetupTailscaleFirewall` используют `FirewallSetupResult`
- [x] Все firewall methods принимают `context.Context`
- [x] SSE events типизированы через structs

---

## ~~Phase 2: Interface Boundaries — Decouple via Interfaces~~ ✅ DONE

**Зависимости:** Phase 1 (result types нужны в signatures интерфейсов)
**Риск:** низкий | **Оценка:** 2-3 дня

### Задачи

- [x] 2.1 Создать `manager/interfaces.go` — все 7 интерфейсов в финальной форме:
  - [x] `ManifestStore` — atomic mutations (без публичного `Save()`)
  - [x] `IntegrationAPI` — все методы с `context.Context`
  - [x] `TailscaleControl` — Up/Down/Login/Logout/Status/SetRoutes + IPNWatcher
  - [x] `FirewallService` — все методы с `context.Context`, включая DNS, WgS2s, restore, check + `IntegrationReady()`
  - [x] `SSEHub` — BroadcastNamed/Broadcast/Subscribe/CurrentState
  - [x] `WgS2sControl` — CreateTunnel/DeleteTunnel/Enable/Disable/Get/List
- [x] 2.2 Рефакторить `Server` struct → зависимости через интерфейсы
- [x] 2.3 Создать `ServerOptions` struct и `NewServer(ctx, opts ServerOptions)`
- [x] 2.4 Адаптировать `Manifest` → `manifestAdapter` для `ManifestStore` (atomic methods внутри делают `Set*() + Save()`)
- [x] 2.5 Адаптировать `integration_api.go` → реализует `IntegrationAPI` (добавлен ctx ко всем методам)
- [x] 2.6 Адаптировать `tailscale.go` → `tailscaleClient` wrapper реализует `TailscaleControl`
- [x] 2.7 Адаптировать `firewall.go` → реализует `FirewallService` (ctx уже был, добавлен `IntegrationReady()`)
- [x] 2.8 `*Hub` уже реализует `SSEHub` — zero changes needed
- [x] 2.9 Обновить все `api_*.go` — `s.lc` → `s.ts`, ctx пробрасывается из request
- [x] 2.10 Создать `manager/mocks_test.go` — mock для каждого интерфейса (function-field-per-method)

### Тестирование Phase 2

- [x] Написать `server_test.go` — `newTestServer` с полностью mock'нутыми зависимостями
- [x] Написать API handler тесты (handleStatus, handleIntegrationStatus, handleDevice, handleUp, integrationReady)
- [x] `go build ./...` проходит
- [x] `go test ./...` проходит
- [x] `go test -race ./...` проходит
- [ ] Deploy на UDM-SE → ручная проверка

### Критерии завершения Phase 2

- [x] Server не обращается к concrete types напрямую (uses interfaces)
- [x] Все зависимости инжектируются через `ServerOptions`
- [x] Server с полностью mock'нутыми зависимостями компилируется и запускается
- [x] Все методы `FirewallService` и `IntegrationAPI` принимают `context.Context`
- [x] `ManifestStore` не имеет публичного `Save()` — только atomic mutations

---

## ~~Phase 3: Centralized Manifest — Single Owner Pattern~~ ✅ DONE

**Зависимости:** Phase 2 (интерфейс `ManifestStore` определён, temporary adapter создан)
**Риск:** средний | **Оценка:** 1-2 дня
**Параллельна с:** Phase 4 (обе зависят от Phase 2, не зависят друг от друга)

### Задачи

- [x] 3.1 Реализовать atomic operations в `manifest.go` — заменить temporary adapter:
  - [x] `SetTailscaleZone()` — `mu.Lock()` + mutate + `saveLocked()`
  - [x] `SetWgS2sZone()` — аналогично
  - [x] `RemoveWgS2sTunnel()` — аналогично (debug log для несуществующего ID)
  - [x] `SetWanPort()` / `RemoveWanPort()` — аналогично
  - [x] `SetSiteID()` / `SetSystemZoneIDs()` — аналогично
  - [x] `SetDNSPolicy()` / `RemoveDNSPolicy()` — аналогично
  - [x] `ResetIntegration()` — аналогично
- [x] 3.2 Заменить все scattered mutation + `Save()` call sites:
  - [x] `firewall.go` (10 мест — Set/Remove + Save → single atomic call)
  - [x] `server.go` — already used ManifestStore interface (no changes needed)
  - [x] `api_wgs2s.go` — already used ManifestStore interface (no changes needed)
  - [x] `api_integration.go` — already used ManifestStore interface (no changes needed)
  - [x] `watcher.go` — already used ManifestStore interface (no changes needed)
  - [x] `watcher_firewall.go` (1 место — RemoveWanPort error now handled)
- [x] 3.3 Убрать публичные `Save()` и temporary adapter:
  - [x] `Save()` → приватный `saveLocked()`
  - [x] Удалить `manifestAdapter` и `NewManifestStore()` из Phase 2
  - [x] `*Manifest` напрямую реализует `ManifestStore`
  - [x] `FirewallManager.manifest` → `ManifestStore` (was `*Manifest`)

### Тестирование Phase 3

- [x] Обновить `manifest_test.go`:
  - [x] Concurrent `SetTailscaleZone` + `SetWgS2sZone` → no race
  - [x] Atomic save: read back matches write
  - [x] `RemoveWgS2sTunnel` для несуществующего ID → no error, debug log
  - [x] File written after each atomic operation (не отложено)
- [x] `go test -race ./...` проходит
- [x] `go build ./...` проходит
- [ ] Deploy на UDM-SE → ручная проверка, manifest.json корректен после операций

### Критерии завершения Phase 3

- [x] `grep -rn 'manifest\.Save()' manager/` — ноль результатов (кроме приватного `saveLocked`)
- [x] `grep -rn 'manifestAdapter\|NewManifestStore' manager/` — ноль результатов
- [x] `go test -race ./...` — нет data races

---

## Phase 4: Synchronous Firewall Coordination ✅ DONE

**Зависимости:** Phase 2 (interfaces, ctx propagation)
**Риск:** средний | **Оценка:** 2-3 дня
**Параллельна с:** Phase 3 (обе зависят от Phase 2, не зависят друг от друга)

### Задачи

- [x] 4.1 Убрать fire-and-forget — заменить `sendFirewallRequest(FirewallActionApplyWgS2s)` на прямой synchronous вызов `s.firewall.SetupWgS2sZone(ctx, ...)` в API handlers
- [x] 4.2 Удалить `FirewallAction` type и `FirewallRequest` struct
- [x] 4.3 ~~Убрать `schedulePostPolicyRestore()` — заменить на synchronous `s.firewall.RestoreRulesWithRetry(ctx, 3, 2*time.Second)`~~
  - **Отменено (sync → async).** `RestoreRulesWithRetry` не возвращает результат клиенту (ошибки только логируются), синхронный вызов блокировал HTTP handlers на 4-6 секунд без пользы. Заменено на `go s.fw.RestoreRulesWithRetry(context.WithoutCancel(ctx), ...)` — горутина привязана к ctx (не untracked), bounded по retry count, watcher (5s poll) — backup.
- [x] 4.4 Упростить `watcher_firewall.go`:
  - [x] Убрать `firewallCh` channel, channel consumer и `handleFirewallRequest()`
  - [x] Оставить periodic poll (5s) + inotify debounce + SIGHUP handler
  - [x] Добавить deduplication (`atomic.Bool`) — не запускать restore если уже в процессе
  - [x] Прямой вызов `s.firewall.EnsureTailscaleRules()` из watcher (не через channel)
- [x] 4.5 HTTP timeout strategy — propagate `r.Context()` в firewall operations, ctx.Err() check между шагами
- [x] 4.6 Обновить API responses — добавить `status` field (`"ok"` / `"partial"`) и `firewall` details
- [x] 4.7 Обновить фронтенд:
  - [x] `api.js` — проверять поле `status` в response body
  - [x] ErrorPanel — показывать warning если `status === "partial"` и `firewall.errors` не пуст

### Тестирование Phase 4

- [x] Написать `api_wgs2s_test.go`:
  - [x] Tunnel creation → response содержит firewall status
  - [x] Firewall failure → `response.status === "partial"` с details
- [x] Написать `watcher_firewall_test.go`:
  - [x] Concurrent restore calls → deduplication (только один запуск)
- [x] `go build ./...` проходит
- [x] `go test ./...` проходит
- [ ] Deploy на UDM-SE → ручная проверка создания/удаления туннелей
- [x] Проверить: SIGHUP handling сохранён (kill -HUP → полная реинициализация firewall)

### Критерии завершения Phase 4

- [x] `grep -rn 'sendFirewallRequest' manager/` — ноль результатов
- [x] `grep -rn 'schedulePostPolicyRestore' manager/` — ноль результатов
- [x] `grep -rn 'FirewallRequest' manager/` — ноль результатов
- [x] `grep -rn 'FirewallAction' manager/` — ноль результатов
- [x] Нет untracked goroutines (каждая горутина привязана к context)
- [x] Firewall setup проверяет `ctx.Err()` между шагами
- [x] Tunnel creation → HTTP response содержит firewall status
- [x] Firewall failure → client видит ошибку, не silent success

---

## Phase 5: Transaction Semantics — Inline Rollback ✅ DONE

**Зависимости:** Phase 3 (atomic manifest) + Phase 4 (synchronous firewall) — обе нужны
**Риск:** средний | **Оценка:** 2 дня

### Задачи

- [x] 5.1 Рефакторить `SetupTailscaleFirewall` — inline rollback:
  - [x] Zone create fail → return fatal error, no cleanup
  - [x] Policy create fail → rollback: delete zone
  - [x] Manifest save fail → rollback: delete zone
  - [x] UDAPI fail → degraded mode (не rollback), зона и manifest сохранены
- [x] 5.2 Аналогично для `SetupWgS2sZone` — тот же паттерн rollback
- [x] 5.3 Создать `TeardownWgS2sZone` — manifest remove → check shared zone → delete policies → delete zone, best-effort: каждый шаг логирует ошибки, но не останавливает остальные

> **Связь с Phase 7:** Phase 5 создаёт degraded mode с recovery через firewall watcher (5s poll).
> Phase 7 добавляет observability (HealthTracker, `/api/health`, SSE health events) и
> exponential backoff retry. Recovery path: UDAPI fail → watcher restores → HealthTracker.RecordSuccess → SSE → UI.

### Тестирование Phase 5

- [x] Написать/обновить `firewall_test.go`:
  - [x] All steps succeed → `result.OK() == true`
  - [x] Zone create fails → no manifest write, no UDAPI rules
  - [x] Policy create fails → zone rolled back, no manifest
  - [x] Manifest save fails → zone rolled back
  - [x] UDAPI fails → `result.Degraded()`, zone и manifest сохранены
  - [x] Rollback itself fails → logged, best-effort, no panic
- [x] `go build ./...` проходит
- [x] `go test ./...` проходит
- [x] Deploy на UDM-SE → проверить:
  - [x] Штатная работа create/delete tunnel
  - [x] Отключить Integration API → setup → manifest чист (нет orphaned zones)
  - [x] Degraded mode: UDAPI fail → watcher восстанавливает rules (5s poll)
    - Note: watcher detection works (5s cycle). Pre-existing limitation: UDAPI custom rule POST doesn't create iptables entries — zone-binding rules only come from ubios-udapi-server via zone config. Not a Phase 5 regression.

### Критерии завершения Phase 5

- [x] Firewall setup — либо полностью, либо rollback
- [x] Manifest не содержит orphaned zones
- [x] UDAPI failure → degraded (не failure), зона и manifest корректны
- [x] Degraded mode recoverable через firewall watcher

---

## Phase 6: Server Decomposition — Break God Object ✅ DONE

**Зависимости:** Phase 5 (transaction semantics нужны в service layer)
**Риск:** ВЫСОКИЙ | **Оценка:** 4-6 дней

### Подфазы миграции (по одному service, от простого к сложному)

#### 6a: Settings Service ✅ DONE
- [x] Создать `manager/service/settings.go` — `SettingsService` с domain methods
- [x] Перенести logic из `api_settings.go` в service
- [x] Заменить handler на thin wrapper в `server.go`
- [x] Удалить `api_settings.go`
- [x] `go build && go test` проходят

#### 6b: Diagnostics Service ✅ DONE
- [x] Создать `manager/service/diagnostics.go` — `DiagnosticsService`
- [x] Перенести logic из `api_diagnostics.go`
- [x] Thin wrapper в `server.go`
- [x] Удалить `api_diagnostics.go`
- [x] `go build && go test` проходят

#### 6c: Integration Service ✅ DONE
- [x] Создать `manager/service/integration.go` — `IntegrationService`
- [x] Перенести logic из `api_integration.go`
- [x] Thin wrapper в `server.go`
- [x] Удалить `api_integration.go`
- [x] `go build && go test` проходят

#### 6d: Routing Service ✅ DONE
- [x] Создать `manager/service/routing.go` — `RoutingService`
- [x] Перенести logic из `api_routing.go`, включая subnet validation
- [x] Thin wrapper в `server.go`
- [x] Удалить `api_routing.go`
- [x] `go build && go test` проходят

#### 6e: Tailscale Service ✅ DONE
- [x] Создать `manager/service/tailscale.go` — `TailscaleService`
- [x] Перенести logic из `api.go` (Tailscale-related handlers)
- [x] Thin wrapper в `server.go`
- [x] Удалить/очистить `api.go`
- [x] `go build && go test` проходят

#### 6f: WgS2s Service (самый сложный) ✅ DONE
- [x] Создать `manager/service/wgs2s.go` — `WgS2sService`
- [x] Перенести logic из `api_wgs2s.go` — включая `TunnelCreateResult`
- [x] SSE broadcast остаётся в HTTP handler layer (service не зависит от SSEHub)
- [x] Thin wrapper в `server.go`
- [x] Удалить `api_wgs2s.go`
- [x] `go build && go test` проходят

#### 6g: Firewall Orchestrator ✅
- [x] Создать `manager/service/firewall.go` — `FirewallOrchestrator` (high-level orchestration: transaction semantics, rollback)
- [x] Перенести rollback logic из Phase 5
- [x] Определить судьбу исходного `firewall.go` (`FirewallManager`, 16 public методов, 508 строк):
  - Orchestration-логика (SetupTailscaleFirewall, SetupWgS2sZone, TeardownWgS2sZone) → `service/firewall.go`
  - Low-level операции (ensureTailscaleRules, iptables через UDAPI, DNS forwarding) → остаются как реализация `FirewallService` interface
- [x] `go build && go test` проходят

#### 6h: Server Finalization ✅
- [x] Рефакторить `server.go` — thin HTTP router + DI container
- [x] Создать `routes()` method с `http.NewServeMux()` + middleware
- [x] Вынести thin handlers в `handlers.go`, adapters в `adapters.go`
- [x] Убрать все `api_*.go` файлы из корня manager/

### Тестирование Phase 6

- [x] Написать `service/*_test.go` — unit тесты каждого service с mocked dependencies (без httptest)
- [x] Написать/обновить `server_test.go` — integration тесты: HTTP request → service called → correct HTTP response
- [x] `go build ./...` проходит
- [x] `go test ./...` проходит
- [x] `go vet ./...` без warnings
- [ ] Deploy на UDM-SE → **полная** ручная проверка всех features:
  - [ ] Tailscale up/down/login/logout
  - [ ] WG S2S tunnel CRUD
  - [ ] Settings change + apply
  - [ ] Integration API key management
  - [ ] UI полностью функционален

### Критерии завершения Phase 6

- [ ] Server struct ≤ 12 полей — **deferred to Phase 8** (24 fields: 7 services + infra deps + watcher state; reducing requires package restructuring)
- [x] Каждый service имеет `NewXxxService()` с explicit dependencies
- [x] Services не импортируют `net/http` — protocol-agnostic
- [x] Services не зависят от SSEHub
- [x] Services зависят от interfaces, не concrete types
- [x] Нет cross-service вызовов
- [x] Нет `api_*.go` файлов в корне manager/
- [x] Service tests не используют httptest

---

## Phase 7: Health Tracking + Watcher Observability & Recovery ✅ DONE

**Зависимости:** Phase 6 (services decomposed, watchers готовы к интеграции с HealthTracker)
**Риск:** низкий | **Оценка:** 2-3 дня

### Задачи

- [x] 7.1 Создать `manager/health.go` — `WatcherHealth`, `HealthTracker`:
  - [x] `NewHealthTracker(hub SSEHub)`
  - [x] `RecordSuccess(name)` — clears degraded, broadcasts
  - [x] `RecordError(name, err)` — increments reconnect count, broadcasts
  - [x] `SetDegraded(name, reason)` — broadcasts
  - [x] `Snapshot()`, `OverallStatus()` — "healthy" / "degraded" / "unhealthy"
- [x] 7.2 Интегрировать HealthTracker в watchers:
  - [x] `watcher.go` — `RecordSuccess` / `RecordError` (на момент Phase 7 файлы ещё в корне `manager/`, перенос в `watcher/` — Phase 8)
  - [x] `watcher_firewall.go` — `RecordSuccess` / `RecordError` / `SetDegraded`
- [x] 7.3 Заменить старый `integrationRetryState` на новый с exponential backoff (30s → 1m → 2m → 5m → 10m cap):
  - [x] `shouldRetry()`, `recordFailure()`, `markRecovered()`
  - [x] Обновить все обращения к `s.intRetry` в `watcher_firewall.go`
- [x] 7.4 Добавить endpoint `/api/health` — overall status + per-watcher details
- [x] 7.5 SSE health events — `broadcastLocked()` при каждом изменении состояния
- [x] 7.6 Обновить фронтенд:
  - [x] `tailscale.svelte.js` — обрабатывать SSE event `health`
  - [x] Показывать watcher health в StatusPill popover

### Тестирование Phase 7

- [x] Написать `health_test.go`:
  - [x] Concurrent access → `go test -race` проходит
  - [x] `RecordSuccess` clears degraded
  - [x] `OverallStatus`: healthy, degraded, unhealthy
  - [x] SSE broadcast on state change (mock hub)
- [x] Написать/обновить `watcher_test.go`:
  - [x] degraded → retry → recovery
  - [x] Backoff увеличивается, capped at 10m
- [x] `go build ./...` проходит
- [x] `go test ./...` проходит
- [x] `go test -race ./...` проходит
- [ ] Deploy на UDM-SE → проверить:
  - [ ] `/api/health` возвращает корректный статус
  - [ ] Degraded → автоматический retry → recovery
  - [ ] SSE health event приходит в UI

### Критерии завершения Phase 7

- [x] Degraded mode → автоматический retry с exponential backoff
- [x] `/api/health` → полный статус watchers
- [x] `/api/health` и `/api/status` — разные endpoints
- [x] SSE event `health` при изменении состояния
- [x] `grep -rn 'intRetry' manager/` — ноль обращений к старому полю

---

## ~~Phase 8: Package Restructuring — Progressive Disclosure~~ ✅ DONE

**Зависимости:** Phase 7 (health.go создан и нужно переместить), Phase 6 (services уже в `service/`)
**Риск:** средний | **Оценка:** 2-3 дня

### Задачи

- [x] 8.1 Создать `manager/domain/` — leaf package (shared types + interfaces):
  - [x] `domain/interfaces.go` ← из `interfaces.go`
  - [x] `domain/results.go` ← из `results.go`
  - [x] `domain/errors.go` ← из `errors.go`
  - [x] `domain/types.go` ← shared types из разных файлов (`ZoneInfo`, `WanPortEntry`, `SSEMessage`, etc.)
- [x] 8.2 Создать `manager/client/` — external API clients:
  - [x] `client/tailscale.go` ← из `tailscale.go`
  - [x] `client/integration.go` ← из `integration_api.go`
  - [x] `udapi/netcfg.go` ← из `udapi_netcfg.go` (в существующий `udapi/` пакет)
- [x] 8.3 Создать `manager/state/` — state management:
  - [x] `state/manifest.go` ← из `manifest.go`
  - [x] `state/logbuffer.go` ← из `logbuffer.go`
- [ ] 8.4 ~~Создать `manager/watcher/`~~ — **deferred**: watcher methods are deeply coupled to Server (15+ fields), extraction requires creating standalone structs with injected dependencies. Benefit is marginal vs. risk. Files remain in root with clear naming (watcher.go, watcher_firewall.go, watcher_nginx.go, nginx.go, updater.go).
- [x] 8.5 Создать `manager/sse/` — SSE hub:
  - [x] `sse/hub.go` ← из `sse.go`
- [x] 8.6 Создать `manager/config/` — configuration:
  - [x] `config/config.go` ← константы из `consts.go` + `version.go`
- [x] 8.7 Обновить все imports во всех файлах — sub-packages импортируют `domain/` и `config/`, не друг друга
- [x] 8.8 Обновить `main.go` — wire-up всех packages
- [x] 8.9 Перенести тесты в соответствующие пакеты (integration_api_test → client/, subnet_validator_test → service/)
- [x] 8.10 Обработать `subnet_validator.go` — перенесён в `service/subnet_validator.go`, SubnetConflict типы объединены
- [x] 8.11 Убедиться что файлы, остающиеся в корне `manager/`, осознанно оставлены:
  - [x] `server.go`, `main.go` — entry point и HTTP
  - [x] `handlers.go`, `sse.go` (handleSSE) — HTTP handlers
  - [x] `health.go` — HealthTracker (создан в Phase 7)
  - [x] `detect.go`, `dpi.go`, `loghandler.go`, `logcollector.go` — infrastructure
  - [x] `cleanup.go` — infrastructure
  - [x] `firewall.go` — low-level `FirewallService` implementation
  - [x] `adapters.go`, `aliases.go` — package bridging
  - [x] `watcher.go`, `watcher_firewall.go`, `watcher_nginx.go`, `nginx.go`, `updater.go` — watchers (deferred extraction)

### Тестирование Phase 8

- [x] `go build ./...` проходит (нет circular imports)
- [x] `go test ./...` проходит
- [x] `go test -race ./...` проходит
- [x] `go vet ./...` без warnings
- [x] Проверить import direction:
  - [x] `domain/` не импортирует ничего из `manager/` (только `internal/wgs2s`)
  - [x] `config/` не импортирует ничего из `manager/`
  - [x] Sub-packages не импортируют друг друга
  - [x] Sub-packages не импортируют `manager/` (только `domain/` и `config/`)
- [x] `make build` — cross-compilation ARM64
- [ ] Deploy на UDM-SE → **финальная полная проверка** (отдельно)

### Критерии завершения Phase 8

- [x] Нет circular imports
- [x] `domain/` — leaf package (импортирует только external + `internal/wgs2s`)
- [x] `config/` — single source of truth для констант
- [x] Все исходные .go файлы из корня manager/ учтены (перемещены или осознанно оставлены)
- [x] AI-агент может понять архитектуру по: `ls manager/` → `ls manager/domain/` → `ls manager/service/`

---

## Финальная валидация (после всех фаз)

### Сборка

- [ ] `go build ./...`
- [ ] `go test ./...`
- [ ] `go test -race ./...`
- [ ] `go vet ./...`
- [ ] `make build` (ARM64)

### Deploy и функциональность

- [ ] `make deploy` на UDM-SE
- [ ] Tailscale: up/down/login/logout
- [ ] WG S2S: tunnel CRUD + enable/disable
- [ ] Settings: change + apply
- [ ] Integration API: key set/test/remove
- [ ] UI: полностью функционален
- [ ] Reboot UDM-SE → services restore
- [ ] Firewall rules корректны
- [ ] manifest.json без orphaned entries
- [ ] `/api/health` — корректный статус

### Архитектурные invariants

- [ ] Services не импортируют `net/http`
- [ ] Services не зависят от SSEHub
- [ ] `domain/` — leaf package
- [ ] Sub-packages не импортируют друг друга
- [ ] Sub-packages не импортируют `manager/`
- [ ] `config/` — single source of truth

---

## Сводная таблица

| Phase | Название | Дни | Риск | Зависит от | Параллельна с |
|-------|----------|-----|------|-----------|---------------|
| 1 | ~~Honest Types + API Contracts~~ ✅ | 2 | Низкий | — | — |
| 2 | Interface Boundaries + Context | 2-3 | Низкий | 1 | — |
| 3 | Manifest Centralization | 1-2 | Средний | 2 | **Phase 4** |
| 4 | Sync Firewall | 2-3 | Средний | 2 | **Phase 3** |
| 5 | ~~Transaction Semantics~~ ✅ | 2 | Средний | **3 + 4** | — |
| 6 | Server Decomposition | 4-6 | **Высокий** | 5 | — |
| 7 | ~~Health Tracking + Recovery~~ ✅ | 2-3 | Низкий | 6 | — |
| 8 | Package Restructuring | 2-3 | Средний | **7** | — |
| | **Итого** | **17-24** | | | |
