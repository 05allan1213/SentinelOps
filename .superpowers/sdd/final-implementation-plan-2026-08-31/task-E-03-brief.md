# Task E-03 brief (coordinator)

Task card: read `output/final-implementation-plan-2026-08-31.md` lines 1619-1665 (section `### Task E-03: Add Agent Runtime Runs page, filters and desktop navigation`) in full; it is the authoritative scope.

## Environment

- Work in `/home/monody/project/.worktrees/sentinelops-e` (branch `feat/phase-e-20260910`). Do NOT edit `/home/monody/project/SentinelOps`.
- Prerequisites already committed: E-00 visual direction (`docs/implementation/runtime-visual-direction-2026-08-31.md`), E-01 data layer, E-02 event tail hook. Read the E-00 direction before styling and follow it.
- Commands from `web/`: `npm run test:unit`, `npm run lint`, `npm run build`, `npx playwright test tests/ui/runtime-runs.spec.ts --project=chromium`.

## Scope

- Create `web/src/pages/runtime/index.tsx`, `web/src/pages/runtime/components/RuntimeRunsTable.tsx`, `RuntimeRunFilters.tsx`, `RuntimeStatusBadge.tsx`.
- Modify `web/src/App.tsx`, `web/src/components/layout/Sidebar.tsx`, and `web/src/components/layout/Layout.tsx` only for constrained Runtime content width/topbar slot.
- Test: `web/tests/ui/runtime-runs.spec.ts`.
- Do not create the Capabilities/Safety/Worker Health pages (E-07), the Run Detail page (E-04), Go code, dependencies, or F work.

## Interfaces (frozen)

- Route `/runtime/runs` under the existing authenticated `Layout`; navigation group `Agent Runtime` with child links `Runs`, `Capabilities`, `Safety`, `Worker Health`.
- `RuntimeRunsTable` props `{data: ListRunsRes; onSelectRun(runId: string): void; loading: boolean}`; columns `run_id/session_id/agent/status/current_phase/attempt/worker_id/recovery_mode/budget/created_at/duration_ms` exactly as returned (no client-derived phase/success).
- Filters are controlled by URL search params only: `status`, `session_id`, `agent`, `from`, `to`, `scope`, `include_legacy`, `sort`, `direction`, `page`, `page_size`. No filter state in Zustand.

## Coordinator instructions

1. Preserve every existing route/redirect and all unrelated navigation labels/behavior. Only add the one Agent Runtime group. Reuse `Pagination`, `CustomSelect` or a native accessible `<select>`, `StatCard`, `cn()`, Lucide and the existing dark Sidebar.
2. Status rendering: only `succeeded` gets success styling. `parked`, `reconciling`, `retryable_failed`, `canceled`, `waiting_approval`, `pending`, `running` and any unknown value stay visually distinct and never inherit success. Legacy rows (`runtime_mode != durable_v1`) are labelled read-only historical and expose no Recovery control.
3. Distinct table states: successful empty page, loading, partial (`availability=partial` with `data_quality`/`reason_code`), unavailable (`reason_code`, e.g. `not_observed`), and failed with retry that keeps previously loaded rows. Use the E-01/E-02 query results; no client-side fallback data.
4. Layout: stable column widths, `min-width: 0`, and horizontal scrolling confined to the table region; no page-level horizontal overflow at 1280/1440. Follow the E-00 density/hierarchy direction.
5. Row selection navigates to `/runtime/runs/{run_id}` (the detail route is added by E-04; the link target is frozen). Do not reuse `/ops/v1/runs` or reinterpret `OpsRun` rows.
6. TDD: write `runtime-runs.spec.ts` first, run it before implementation, and record the expected missing-route/missing-page failure verbatim. Required assertions: navigation presence, URL filter synchronization, durable-default list, legacy labelling, all loading/empty/partial/unavailable/failed states, canonical badges, row navigation, no page overflow. Use controlled API responses for UI states; real Runtime/Worker execution remains `NOT RUN`.
7. Bounded fixture allowance: if the spec needs an API mock harness, add it under `web/tests/ui/fixtures/` or inside the spec in the same style D used; never register production routes or present fixture data as real Runtime success.
8. Also run at least the existing smoke/approval UI suites to prove `/ops`, `/chat`, `/events/analysis` and the dark Sidebar still work.
9. Verify: focused spec at both viewports, full `npm run test:unit`, `npm run lint`, `npm run build` (68 warnings + large chunk are baseline), plus the legacy suites. Fix before committing.
10. Commit only your files with the exact message `feat(web-runtime): add desktop runtime runs console`. No push. On `index.lock`, wait and retry.
11. Write `.superpowers/sdd/final-implementation-plan-2026-08-31/task-E-03-report.md` with status, files, RED/GREEN browser evidence, state-mapping table, and the command/PASS-FAIL-NOT RUN evidence.

## Coordinator rulings

- The three navigation links to `/runtime/capabilities`, `/runtime/safety`, `/runtime/worker-health` are added here, but their routes are owned by E-07. Your spec must not assert those pages render; the phase closeout verifies every added link resolves. No placeholder page may be created.
- Desktop only (1280/1440); mobile navigation explicitly `OUT OF SCOPE`.

## Stop conditions

Stop and report if the E-01 list response cannot express a required table field/state, if preserving an existing route conflicts with the new group, or if any test would require faking real Runtime data.
