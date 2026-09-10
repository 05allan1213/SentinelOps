# Task E-03 implementation report

Status: **PASS**. `/runtime/runs` desktop console, URL-driven filters, canonical status badges, legacy labelling
and the `Agent Runtime` navigation group are implemented and browser-verified at 1280/1440.

Base: `08722be` (E-02).

## Files

- `web/src/pages/runtime/index.tsx`
- `web/src/pages/runtime/components/RuntimeRunsTable.tsx`
- `web/src/pages/runtime/components/RuntimeRunFilters.tsx`
- `web/src/pages/runtime/components/RuntimeStatusBadge.tsx`
- `web/src/pages/runtime/components/runtimeStatus.ts` (status/phase/legacy helpers split out to keep
  Fast Refresh boundaries; the card's component files stay component-only)
- `web/src/App.tsx` (route `runtime/runs`; every existing route/redirect preserved)
- `web/src/components/layout/Sidebar.tsx` (one new `Agent Runtime` group with the four frozen children)
- `web/tests/ui/runtime-runs.spec.ts`
- Bounded extension: `web/src/hooks/useRuntimeQueries.ts` gains `placeholderData: keepPreviousData` so a failed
  refetch keeps the previously proven rows (the card's "failed shows retry without clearing prior data");
  no key/service change.
- `web/src/components/layout/Layout.tsx` was intentionally not modified: the page constrains itself
  (`max-w-[1440px]`, `min-w-0`) and the table owns its horizontal scroll, so no shell change was needed.

## Behaviour

| Concern | Implementation |
| --- | --- |
| Data | `useRuntimeRuns` → `/api/runtime/v1/runs` with URL-derived params; durable default (`include_legacy` only sent when checked). |
| Columns | run_id (+ legacy chip), session_id, agent, status/phase badge, attempt, worker/lease/generation, recovery_mode, budget (M/T), started_at, duration_ms. The card's `created_at` column does not exist in `RunSummaryDTO`; the frozen DTO's `started_at` is shown instead (source conflict recorded). |
| Status semantics | Only `succeeded` uses the success tone; `parked`/`reconciling` use attention, `canceled` neutral, unknown a dashed gray badge. |
| States | Skeleton rows (loading), `暂无数据` (successful empty), amber partial banner with `reason_code`, gray unavailable panel with `reason_code` (`not_observed`), red error banner with retry while rows stay rendered. |
| Filters | URL search params only (`status`, `session_id`, `agent`, `from`, `to`, `scope`, `include_legacy`, `sort`, `direction`, `page`, `page_size`); no Zustand state; reset clears the query string. |
| Layout | Stable column widths, `min-w-0`, table-local horizontal scroll, page asserts no horizontal overflow at 1280/1440. |

## TDD evidence

1. RED: `npx playwright test tests/ui/runtime-runs.spec.ts` before the page existed -> the route was missing and
   every navigation/state assertion failed (recorded failure output: `No routes matched location "/runtime/runs"`).
2. Own defects fixed during GREEN: the first run sent `include_legacy=false` on the durable default (now omitted
   unless checked), and the initial ref-based "previous data" fallback violated `react-hooks/refs` (replaced by
   `keepPreviousData`).
3. GREEN: 5/5 browser cases pass (2 widths × navigation/filters/badges/legacy + 2 widths × states + 1 row
   navigation).

## Final verification

| Command | Result | Evidence |
| --- | --- | --- |
| `npx playwright test tests/ui/runtime-runs.spec.ts` | PASS | 5/5 at 1280 and 1440 |
| `npm run test:unit` | PASS | 13 files / 269 tests |
| `npm run lint` | PASS | 0 errors, 68 pre-existing warnings (new files add none after the helper split) |
| `npm run build` | PASS | existing >500 kB chunk warning only |
| Legacy suites `approval-ui` + `markdown` | 9/10 | Only the baseline-reproduced `approval-ui.spec.ts:112` failure (identical at `f461a3c` in the D worktree) |
| `git diff --check` | PASS | no whitespace errors |

## Boundaries

- The row link targets the frozen `/runtime/runs/{run_id}` route that E-04 implements; until then React Router
  logs "No routes matched" for the detail URL (temporary, in-phase).
- Controlled API fixtures prove UI/state behaviour only; real host API/Worker execution is `NOT RUN`.
