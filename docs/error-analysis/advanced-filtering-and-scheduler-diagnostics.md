# Error Analysis Advanced Filtering and Scheduler Diagnostics

## Purpose

The admin error analysis page helps operators inspect failed gateway requests and identify where a request failed in the LightBridge request pipeline. The page is designed for operational debugging rather than high-level reporting: every visible field should either narrow the failed request set, explain the selected request, or point to the next concrete remediation step.

This document describes the advanced filtering layout, compact request pagination, step-by-step failure analysis, and account scheduler diagnostics added to the error analysis view.

## Page Entry

- Route: `/admin/error-analysis`
- Router name: `AdminErrorAnalysis`
- Required permissions: authenticated admin user
- Feature gate: the sidebar entry follows the existing ops monitoring feature flag
- Page title and description: supplied by the application layout through route metadata; the page content must not render an additional page-level title or description

## Advanced Filtering Bar

The top toolbar contains all global filters for the failed request list. From left to right, the controls are:

1. Search input
2. Time range selector
3. Status code selector
4. Refresh action

The search input was intentionally moved out of the left request-list module and into the global toolbar because it affects the same request query as the time range and status selectors. Keeping all query controls together makes it clear that they act as one filter set.

### Search Input

The search input writes to `searchQuery` and is debounced before reloading the request list.

Search currently maps to the backend `q` query parameter and is intended for:

- `request_id`
- `client_request_id`
- Error message text

Behavior:

- Empty search text removes the `q` parameter.
- Non-empty search text is trimmed before sending.
- Changing the search text resets the failed request list to page 1.
- Search reloads are debounced to avoid firing a request for every keystroke.
- Stale list responses are ignored by a monotonically increasing request sequence guard.

### Time Range Selector

The time range selector writes to `timeRange` and maps to the backend `time_range` query parameter.

Supported values in the UI:

- `5m`
- `30m`
- `1h`
- `6h`
- `24h`
- `7d`
- `30d`

Behavior:

- Changing the time range resets the failed request list to page 1.
- The selected detail is refreshed from the new result set.
- If the previously selected error is no longer visible, the first item in the new list is selected automatically.

### Status Code Selector

The status code selector writes to `statusCodeFilter` and maps to the backend `status_codes` query parameter.

Supported values in the UI:

- All
- `403`
- `429`
- `500`
- `502`
- `503`
- `504`

Behavior:

- Empty value means all status codes.
- Non-empty value sends `status_codes=<code>`.
- Changing the status code resets the failed request list to page 1.

### Removed Quick Status Buttons

The old quick filters inside the failed request module were removed:

- All
- `403`
- `503`

Reason:

- They duplicated the global status selector.
- They consumed horizontal space in the narrow failed request panel.
- They made the request-list card look like it had its own independent filter state, even though it affected the same global query.

## Failed Request List Module

The failed request module is the left column of the page. It now focuses only on the result list and compact pagination.

### Header

The module header contains:

- Module label: Failed Requests
- Total count text

It no longer contains search or quick status controls.

### Request Row Summary

Each row shows the key fields needed to decide which failed request to inspect:

- HTTP status code
- Failure phase
- Error owner
- Request ID or client request ID
- Created time
- Short error message
- Platform
- Requested/upstream model label
- Group name or group ID when present

The selected request is highlighted so operators can keep their place while reading the right-side analysis.

## Compact Pagination

The left request-list panel is intentionally narrow, so the generic table pagination component is not used there. A compact pagination footer is rendered directly inside the page.

### Result Count Text

The result count text is forced onto one horizontal line using a non-wrapping layout.

The text includes:

- First result number on the current page
- Last result number on the current page
- Total result count

Example:

```text
Showing 1 to 10 of 148 results
```

This fixes the previous narrow-panel issue where pagination text collapsed into a vertical layout.

### Page Button Width

The compact paginator limits visible page buttons to fit the module width.

Rules:

- If total pages are 5 or fewer, show every page.
- If the current page is near the beginning, show `1 2 3 ... last`.
- If the current page is near the end, show `1 ... last-2 last-1 last`.
- If the current page is in the middle, show `1 ... current ... last`.
- Previous and next buttons are fixed-width icon buttons.

This keeps the pagination controls inside the failed request module and prevents horizontal overflow.

### Page Size

The page size remains fixed at the page's `pageSize` state. The compact footer does not expose a page-size selector because the left panel does not have enough width for a full table pagination control.

## Step-by-Step Analysis

The right panel analyzes the selected request across these pipeline steps:

1. Request intake
2. Auth check
3. Routing and model
4. Account scheduling
5. Provider adapter
6. Upstream request
7. Response handling

Each step has:

- Step label
- Internal module name
- State badge
- Evidence fields

Step states:

- Passed
- Failed
- Warning
- Skipped
- Unknown

The failed step is derived from request error metadata such as phase, owner, status code, upstream evidence, and known error text like `No available accounts`.

## Account Scheduler Diagnostics

When the selected error includes a `group_id`, the page loads all accounts in that group and displays them under the Account Scheduling step.

This panel answers two operational questions:

1. Which accounts were in the current group at analysis time?
2. Why would each account not be called by the scheduler?

### Data Source

The diagnostics reuse the existing admin account list API:

```text
GET /admin/accounts?page=<n>&page_size=100&group=<group_id>&sort_by=priority&sort_order=desc
```

The frontend fetches all pages until the number of loaded accounts reaches the backend `total` value. This is required because the feature must show all accounts in the current group, not only the first page.

### Loading Behavior

Scheduler account diagnostics are loaded after the main error detail is loaded.

This keeps the primary analysis responsive:

- Error detail and upstream correlation load first.
- The right panel can render the root cause and steps immediately.
- Account diagnostics fill in afterward.

Stale account responses are ignored with a request sequence guard. If the operator quickly switches between failed requests, an older account query cannot overwrite the currently selected request's diagnostics.

### Available Count

The scheduler diagnostics header shows:

```text
<available>/<total> available
```

If the count is `0/<total>`, the panel shows an explicit warning that no available account was found in the current group.

### Per-Account Fields

Each account row shows:

- Availability badge
- Account display name
- Account ID
- Platform
- Account status
- Capacity summary
- Blocking reasons, if any

Capacity summary can include:

- Concurrency usage: `CC current/limit`
- RPM usage: `RPM current/limit`

### Availability Decision

The frontend marks an account as available only when no blocking reason is detected from the account fields available in the admin API response.

This is a diagnostic approximation of scheduler eligibility. The authoritative scheduler still lives on the backend, but the frontend explanation is useful because it exposes the common reasons operators need to fix.

### Blocking Reasons

The diagnostics can mark an account unavailable for these reasons:

| Reason | Meaning |
| --- | --- |
| Not bound to this group | The account does not include the selected request's group ID. |
| Platform mismatch | The account platform differs from the failed request platform. |
| Account is inactive | `status` is `inactive`. |
| Account is in error status | `status` is `error`; the error message is shown when available. |
| Account scheduling is disabled | `schedulable` is false. |
| Account is rate-limited until | `rate_limit_reset_at` is in the future. |
| Account is temporarily unschedulable | `temp_unschedulable_until` is in the future; the reason is shown when available. |
| Account is overloaded until | `overload_until` is in the future. |
| Account is expired | `expires_at` is set and already passed. |
| Concurrency limit reached | `current_concurrency` is greater than or equal to `concurrency`. |
| RPM limit reached | `current_rpm` is greater than or equal to `base_rpm`. |
| Total quota exhausted | `quota_used` is greater than or equal to `quota_limit`. |
| Daily quota exhausted | `quota_daily_used` is greater than or equal to `quota_daily_limit`. |
| Weekly quota exhausted | `quota_weekly_used` is greater than or equal to `quota_weekly_limit`. |
| Session window rejected | `session_window_status` is `rejected`. |
| Requested model is not allowed | Account model whitelist does not match the requested model. |

### Model Matching

The diagnostic checks model allow-list data from account `extra` when available.

Supported frontend sources:

- `extra.model_whitelist`
- `extra.models`
- `extra.supported_models`

Supported pattern behavior:

- Empty allow list means the account is considered model-compatible.
- Exact string matches are supported.
- `*` wildcard patterns are supported.

Examples:

| Pattern | Matches |
| --- | --- |
| `gpt-4o-mini` | Only `gpt-4o-mini` |
| `gpt-4o*` | `gpt-4o`, `gpt-4o-mini`, `gpt-4o-2024-xx` |
| `*` | Any model |

### Empty States

The scheduler diagnostics panel has two empty states:

1. The error detail has no `group_id`; the page cannot determine current group accounts.
2. The group has no accounts; the panel reports that no accounts were found in the current group.

## Request Race Protection

The page uses sequence counters to avoid stale response writes:

- `listFetchSeq` protects failed request list loading.
- `detailFetchSeq` protects selected error detail loading.
- `schedulerAccountFetchSeq` protects scheduler account diagnostics loading.

When a new request starts, the corresponding counter increments. When an async response returns, it updates state only if its captured sequence still matches the latest sequence.

This avoids UI bugs where:

- Old searches overwrite newer search results.
- Old selected error details replace the current selected request.
- Old scheduler account lists appear under the wrong error.

## Recommended Operator Workflow

For a `503 No Available Account` request:

1. Open `/admin/error-analysis`.
2. Use the time range and status code filters to narrow the list to `503` failures.
3. Search by request ID when available.
4. Select the failed request.
5. Confirm the root cause is `No Available Account`.
6. Inspect the Account Scheduling step.
7. Review every account in the current group.
8. Fix the blocking reasons shown for the relevant accounts.
9. Refresh the failed request list or retry the original request.

Common fixes:

- Bind an account to the request group.
- Enable the account.
- Clear account error status after resolving credentials.
- Re-enable scheduling for the account.
- Wait for or clear rate-limit and temporary unschedulable state.
- Increase concurrency or RPM limits if appropriate.
- Update the account model whitelist or requested model mapping.
- Check quota usage and reset/renew quota if appropriate.

## Implementation Files

- `frontend/src/views/admin/ops/ErrorAnalysisView.vue`
- `frontend/src/views/admin/ops/utils/errorAnalysis.ts`
- `frontend/src/views/admin/ops/utils/errorAnalysis.spec.ts`
- `frontend/src/i18n/locales/en.ts`
- `frontend/src/i18n/locales/zh.ts`
