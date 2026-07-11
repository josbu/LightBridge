# Progressive Feature Registration

LightBridge uses one feature catalog to decide which optional backend services,
HTTP routes, frontend routes, menus and module contributions are available. The
catalog is deliberately conservative: core request-path services remain
resident, while optional work is either dynamically owned or registered only at
process startup.

## Core boundary

The following features are always eager and cannot be disabled:

- authentication and authorization;
- gateway request handling and account scheduling;
- reliable billing, quota enforcement and usage idempotency;
- OAuth token refresh required by active accounts.

These capabilities share state with every gateway request. Making them optional
would split invariants across routes, workers and repositories, so configuration
validation rejects attempts to disable them.

## Activation modes

| Mode | Meaning | Runtime setting behavior |
| --- | --- | --- |
| `eager` | Core process capability | Always active; cannot be disabled |
| `dynamic` | Restart-safe optional business capability | Worker, routes and UI can pause/resume in the current process |
| `boot` | High-cost subsystem whose implementation is not restart-safe | Registered once during process startup; configuration changes report `requiresRestart` |
| `on_demand` | No resident worker; resource is loaded for a concrete request or UI contribution | Loaded only when used |

A `boot` feature has two states:

- `configuredEnabled`: what the current configuration requests;
- `enabled`: what this running process actually has available.

When they differ, the public manifest sets `requiresRestart=true`. Backend route
guards and the frontend both use `enabled`, so a page cannot appear without its
worker and a running worker is not hidden before restart.

## Resource profiles

Configure the process in `config.yaml`:

```yaml
features:
  profile: full
  overrides: {}
```

Profiles are minimum eligibility levels, not editions or authorization tiers.

| Profile | Intended use | Eligible resident subsystems |
| --- | --- | --- |
| `minimal` | Low-memory edge node or gateway-only deployment | Core path and lightweight dynamic business workers |
| `standard` | Normal single-node deployment | `minimal` plus Ops/statistics, aggregation, cleanup, backups and scheduled tests |
| `full` | Complete platform node | `standard` plus module runtime and LightBridge Connect |

The default is `full` to preserve existing deployments. A hard subsystem config
such as `ops.enabled=false` still wins over the profile. `features.overrides`
can further disable or enable known non-core feature IDs, subject to profile and
hard-config prerequisites.

## Backend lifecycle ownership

`FeatureRuntimeManager` is the single owner of optional background components.
Constructors must not start goroutines, timers or module runtimes. Registration
supplies three operations:

- `Start`: allocate/start the optional component;
- `Pause`: stop dynamic work while keeping a restart-safe object reusable;
- `Shutdown`: final process-exit cleanup, including components paused earlier.

Start failures are rolled back with an independent deadline. If rollback fails,
the component is blocked from retrying and remains marked for final cleanup.
Detailed component errors are available only from the authenticated administrator
runtime endpoint; the public bootstrap manifest contains no paths, database
errors or module diagnostics.

Public manifest:

```text
GET /api/v1/settings/features
```

Administrator diagnostics:

```text
GET /api/v1/admin/features/runtime
```

## Frontend registration

The frontend fetches the public manifest before synchronizing progressive routes.
Disabled routes are not registered, and their page chunks are not imported.
Menus derive from the same effective state. Built-in heavy pages such as Ops,
module management, backups, scheduled tests and proxy runtime therefore do not
load on nodes where the feature is unavailable.

Route synchronization must remain idempotent: enabling a dynamic feature adds
its named routes once; disabling it removes those routes and any associated
module contribution cache.

## Module UI contributions

Enabled module versions may contribute same-origin UI assets under their own
versioned `/modules/` package root. The backend validates contribution metadata,
serves only the currently enabled version and rejects path traversal and symlink
escapes. Module code is trusted extension code after its package permissions are
approved; it executes in the main frontend origin and must be reviewed like any
other installed plugin.

Supported contributions:

### Admin route (`ui.admin.route`)

The remote component is mounted as a route-level Vue component. The route and
menu title may provide localized maps. Contributions cannot replace reserved
core routes, use catch-all paths or register outside the permitted admin/module
namespace.

### Account form (`ui.account.form`)

The host passes:

```ts
{ contribution }
```

The component may emit:

```ts
created
close
```

`created` refreshes the account list and closes the host dialog.

### Entity panel (`ui.entity.panel`)

The host passes:

```ts
{ entity, entityId, context, contribution }
```

The component may emit:

```ts
close
updated
```

`updated` asks the host page to reload the underlying entity.

Remote imports are concurrent-request deduplicated. Failed promises are evicted
from cache, and generation guards prevent a slow earlier selection from mounting
into a newer dialog or route.

## Performance approach and Rust decision

The measured high-cost paths in this phase were dominated by database, Redis,
filesystem and network waits rather than pure computation. The implementation
therefore uses:

- batched setting reads and immutable feature snapshots;
- single-flight cache refresh;
- in-memory route/feature checks on ordinary requests;
- concurrent independent Ops probes;
- no resident module/UI resources when the corresponding feature is unavailable.

A Rust sidecar or FFI layer was not introduced because it would add cross-platform
build, ABI, crash-isolation and release complexity without accelerating these
I/O-bound paths. Rust should be reconsidered only after a CPU profile identifies
a stable, isolated compute kernel such as very large time-series aggregation or
protocol parsing.

## Adding a feature

1. Add one stable ID and definition to `progressive_features.go`.
2. Choose the least powerful activation mode that preserves correctness.
3. Declare every surface: backend route, frontend route, menu, worker or module runtime.
4. Register optional workers only through `FeatureRuntimeManager`.
5. Guard backend routes with the same feature ID.
6. Add a lazy frontend route and menu contribution using the same manifest state.
7. Add tests for default/profile/override behavior and lifecycle shutdown.
8. Document whether changes apply immediately or require restart.

Do not use a feature flag merely to hide a page while leaving its worker running,
and do not make a core invariant progressive to reduce idle memory.
