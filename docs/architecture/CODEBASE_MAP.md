# LightBridge Codebase Map

This document defines where code belongs after the staged structural cleanup. It
is a navigation contract for future development, not a proposal to rewrite the
application.

## Review inventory

Run:

```bash
make audit-codebase
# or
python3 tools/codebase_inventory.py
python3 tools/codebase_inventory.py --check
```

`CODEBASE_INVENTORY.tsv` records every text file included in the repository
review, together with its line count, ownership classification and SHA-256
digest. Generated code and lock files remain indexed, but they are validated by
their generation source and build checks rather than treated as hand-maintained
architecture.

## Backend layers

| Layer | Location | Responsibility | Must not own |
| --- | --- | --- | --- |
| HTTP assembly | `backend/internal/server` | Server construction, route registration, lifecycle wiring | Business policy |
| Middleware | `backend/internal/middleware`, `backend/internal/server/middleware` | Authentication, limits, request context, transport-level concerns | Repository queries or provider routing |
| Handlers | `backend/internal/handler` | Parse and validate HTTP input, call services, serialize output | Pricing rules, scheduling algorithms, SQL |
| Services | `backend/internal/service` | Application workflows, provider orchestration, billing and scheduling policy | Raw schema migrations or route registration |
| Domain | `backend/internal/domain`, `backend/internal/model` | Shared business vocabulary and stable value types | Framework-specific state |
| Repositories | `backend/internal/repository` | Persistence, transactions and cache adapters | HTTP response behavior or provider conversion |
| Outbound adapters | `backend/internal/outbound`, `backend/internal/pkg/*` | External protocols, clients and low-level transport helpers | User authorization or billing policy |
| Modules | `backend/internal/modules` | Optional, independently registered capabilities | Hidden dependencies on unrelated modules |

The intended dependency direction is:

```text
server/routes -> handler -> service -> repository/outbound
                         -> domain/model
```

Low-level packages must not import handlers or server wiring. Repositories must
not depend on Gin. Outbound protocol packages must not decide user billing,
permissions or account-selection policy.

## Large Go types may span focused files

A service or domain type may span multiple files in one Go package. Use the
owning type or capability as a prefix and give every file one responsibility:

```text
GatewayService
  gateway_service.go                  construction and shared types
  gateway_session.go                  session lifecycle
  gateway_scheduler.go                scheduling entry points
  gateway_scheduler_filters.go        candidate eligibility
  gateway_scheduler_prefetch.go       RPM/window-cost prefetch
  gateway_scheduler_selection.go      generic selection algorithms
  gateway_scheduler_platform.go       platform and mixed scheduling
  gateway_scheduler_diagnostics.go    failure diagnosis
  gateway_billing.go                  usage and billing orchestration
  gateway_billing_platform_quota.go   platform-quota persistence

Account
  account.go                          entity and core schedulability
  account_credentials.go              platform detection and credentials
  account_model_mapping.go            model mapping
  account_retry_policy.go             retry/error-code policy
  account_openai_features.go          OpenAI/Grok/WS capabilities
  account_runtime_features.go         runtime feature switches
  account_quota.go                    quota, reset, RPM and window-cost rules

Content moderation
  content_moderation.go               types, ports and lifecycle
  content_moderation_config.go        configuration workflows
  content_moderation_check.go         request check and worker flow
  content_moderation_admin.go         admin/status operations
  content_moderation_provider.go      moderation provider calls
  content_moderation_side_effects.go  persistence, email and account actions
  content_moderation_policy.go        policy normalization and API-key health
  content_moderation_status.go        runtime status and test input
  content_moderation_payload.go       provider payload and pure normalizers

Admin account HTTP workflows
  account_handler.go                  construction, request types and response assembly
  account_handler_crud.go             list/get/create/update/delete
  account_handler_test_sync.go        test, recovery, CRS and OAuth credentials
  account_handler_batch.go            batch and bulk operations
  account_handler_usage.go            usage, limits and schedulability
  account_handler_models.go           model sync, privacy and tier refresh

Rate limits
  ratelimit_service.go                dependencies and policy entry points
  ratelimit_gemini.go                 Gemini preflight usage
  ratelimit_http_errors.go            generic auth/403/429 handling
  ratelimit_anthropic.go              Anthropic window parsing
  ratelimit_openai.go                 OpenAI snapshots and plan metadata
  ratelimit_recovery.go               successful-test recovery
  ratelimit_temp_unsched.go           configurable temporary unscheduling
  ratelimit_stream_timeout.go         stream-timeout policy
```

The same rule applies to `openai_gateway_*`, `antigravity_*`, `gemini_*`,
`setting_*`, `admin_*` and large repository implementations. New behavior must
be added to the smallest matching responsibility file. Do not recreate a
`misc.go`, `helpers.go` or single giant service file.

## Configuration ownership

Configuration is intentionally separated by phase:

```text
backend/internal/config/
  config_types.go            root and connector-facing types
  config_runtime_types.go    server, gateway, database and runtime types
  config_wechat_connect.go   legacy environment compatibility
  config_load.go             Viper loading and normalization
  config_defaults.go         default registration only
  config_validate.go         validation and URL security helpers
```

Adding a field normally requires edits in three explicit places: its owning type,
its default, and its validation. Compatibility migrations belong in a named
compatibility file rather than in the general loader.

## Frontend layers

| Layer | Location | Responsibility |
| --- | --- | --- |
| Views | `frontend/src/views` | Route-level composition and page orchestration |
| Feature components | next to the owning view/component | Focused panels that are not globally reusable |
| Shared components | `frontend/src/components` | Reusable presentation and interaction units |
| Feature composables | `views/<feature>/composables`, `components/<feature>/composables` | Stateful workflows owned by one feature |
| Shared composables | `frontend/src/composables` | Cross-feature stateful workflows |
| Feature models | `<feature>/model` | Pure constants, parsers, validators and local contracts |
| API | `frontend/src/api` | Typed HTTP calls and transport normalization |
| Stores | `frontend/src/stores` | Cross-route application state |
| Types | `frontend/src/types` | Truly shared contracts; local types stay with their feature |
| i18n | `frontend/src/i18n/locales/*-sections` | Translation domains split by feature while preserving public keys |

For very large Vue pages, separate physical concerns before inventing unstable
component APIs:

```vue
<template src="./feature/Page.template.html"></template>
<script setup lang="ts">...</script>
<style scoped src="./feature/Page.css"></style>
```

Then extract only workflows with a clear input/output boundary. The Settings
page demonstrates the intended progression:

```text
views/admin/settings/
  SettingsView.template.html
  SettingsView.css
  model/settingsForm.ts
  model/settingsViewModel.ts
  model/paymentProviderRules.ts
  composables/usePaymentProviderSettings.ts
  composables/useAffiliateUserSettings.ts
```

A feature-local composable may own API calls and reactive workflow state. Pure
normalization, conflict detection and navigation rules belong in `model/` so
they can be tested without Vue. Do not extract dozens of tiny components that
only forward a large prop list.

## Billing and background-work boundaries

Critical financial work is not telemetry:

- billing tasks must never use a drop/sample overflow policy;
- unknown pricing must create a durable pending record, not a normal zero-cost
  usage row;
- Redis enforcement state and its database mirror must share one persistence
  function;
- email delivery may be asynchronous, but fan-out must be bounded and must use
  backpressure rather than silent loss;
- every background component must have an owner, a stop/drain strategy or an
  intentionally bounded one-shot execution model.

## Structural change rules

1. Preserve public interfaces unless a migration is part of the same change.
2. Separate physical file cleanup from behavioral fixes in different commits.
3. For Go file splits, compare normalized top-level declarations before and
   after; syntax-only success is not sufficient.
4. For locale splits, compare exported initializers by AST rather than text.
5. For externalized Vue blocks, verify template, script and style content.
6. Add tests before moving behavior across package boundaries.
7. Do not hand-edit generated Ent code or lock files to make a check pass.
8. Keep billing, concurrency and authentication fail-safe and observable.
9. Prefer a narrow named file over a generic helper dumping ground.
10. Regenerate `CODEBASE_INVENTORY.tsv` before completing a large refactor.

## Remaining high-complexity areas

The current cleanup substantially reduced physical aggregation, but these areas
still require staged work with full CI and integration-test support:

- durable billing outbox and crash recovery;
- automatic reconciliation and an admin workflow for pending pricing failures;
- refresh-token cookie migration and browser-session hardening;
- provider capability negotiation and streaming conversion boundaries;
- further decomposition of OAuth pending flow and remaining account test workflows;
- proxy-side enforcement for remote DNS resolution;
- continued extraction of large route-level Vue scripts where stable workflow
  boundaries can be demonstrated.

## Progressive feature ownership

See [`PROGRESSIVE_FEATURES.md`](./PROGRESSIVE_FEATURES.md) for the complete
registration and module UI contract. In summary:

- core gateway/auth/billing/token-refresh services remain eager;
- restart-safe optional workers belong to `FeatureRuntimeManager` as `dynamic`;
- high-cost, non-restart-safe subsystems use `boot` plus a minimum resource profile;
- request-only or module UI resources use `on_demand` and lazy frontend imports;
- route, menu, worker and module availability must derive from the same effective
  feature state, not independent local flags.
