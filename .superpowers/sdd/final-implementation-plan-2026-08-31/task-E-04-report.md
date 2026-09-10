# Task E-04 implementation report

Status: **PASS**. Run Detail shell with Overview, Timeline, seven tabs (URL state), per-panel error boundaries,
quality metadata and non-terminal event tail is implemented and browser-verified at 1280/1440.

Base: `56040ac` (E-03).

## Files

- `web/src/pages/runtime/detail.tsx`
- `web/src/pages/runtime/components/RunDetailTabs.tsx`
- `web/src/pages/runtime/components/RunOverview.tsx`
- `web/src/pages/runtime/components/RunTimeline.tsx`
- `web/src/pages/runtime/components/RuntimeQualityState.tsx`
- `web/src/pages/runtime/components/detailTabs.ts` (tab ids/labels split out so component files stay
  component-only for Fast Refresh)
- `web/src/App.tsx` (`runtime/runs/:runId` route; all existing routes preserved)
- `web/tests/ui/runtime-detail-overview.spec.ts`, `web/tests/ui/fixtures/runtime-detail.ts`

## Behaviour

| Concern | Implementation |
| --- | --- |
| Tabs | `overview`/`timeline`/`attempts`/`effects`/`evidence`/`context`/`trace` as semantic `role="tab"` buttons; the active tab is URL search state (`?tab=`), overview is the default; the five later tabs render an anchored placeholder that E-05/E-06 replace. |
| Overview | Status/phase badge, identity/orchestration, worker + lease + generation + heartbeat + park reason, Run/Checkpoint/Attempt/executing-worker fingerprints with match flags and exact-restore verdict, Gate summary (shadow/L1/L2/caps/policy/catalog/audit), Budget (all counters, nulls as “—”, usage/trace quality) and Context summary (identity/scope/revisions/hashes/deadline). |
| Timeline | Every canonical event in `seq` order with seq/type/summary/time/attempt/generation, collapsed payload by default (expand shows trace/operation/reference/time, `MarkdownRenderer variant="runtime"` for the summary and allowlisted attributes). No filtering or dropping. |
| Quality | `RuntimeQualityState` renders availability/data-quality/reason_code and independent `not_run`; partial compatibility and gate metadata are shown with their own reasons. |
| Streaming | E-02 tail starts only when the server status is non-terminal; terminal status (succeeded/failed/canceled/parked) opens no reader. Tail connection state is displayed separately from Run status. |
| Errors | Per-panel class error boundary keeps the Run identity visible; detail loading/unavailable/error states are distinct. |

## TDD evidence

1. RED: the spec was written before the components; the first run failed with every detail assertion (route and
   panels missing).
2. Own defects fixed during GREEN: the fixture returned the raw `RunDetailDTO` without the frozen
   `GetRunRes{item,...}` wrapper (page showed "未找到该 Run"), and three locators needed `.first()` because the
   header and Overview intentionally both render the status badge/quality chips.
3. GREEN: 5/5 browser cases pass.

## Final verification

| Command | Result | Evidence |
| --- | --- | --- |
| `npx playwright test tests/ui/runtime-detail-overview.spec.ts` | PASS | 5/5 (2 widths × overview/tabs/timeline + 2 widths × partial/unavailable/failed + tail gating) |
| `npx playwright test tests/ui/runtime-runs.spec.ts tests/ui/runtime-sse.spec.ts` | PASS | 7/7 (no regression from the new route) |
| `npm run test:unit` | PASS | 13 files / 269 tests |
| `npm run lint` | PASS | 0 errors, 68 pre-existing warnings |
| `npm run build` | PASS | existing >500 kB chunk warning only |
| `git diff --check` | PASS | no whitespace errors |

## Boundaries

- The five later tabs are explicit anchored placeholders owned by E-05/E-06; no fake data is rendered.
- Controlled API fixtures prove UI behaviour only; real host API/Worker execution is `NOT RUN`.
