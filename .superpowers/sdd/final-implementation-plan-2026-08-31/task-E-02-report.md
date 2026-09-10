# Task E-02 implementation report

Status: **PASS**. Runtime SSE tail is wired to the shared D-06 transport and the TanStack cache with a single
reader per Run, monotonic cursor, dedupe, visibility handling and terminal close. No Run is ever created.

Base: `4d79772` (E-01).

## Files

- `web/src/hooks/useRuntimeEventTail.ts` (new)
- `web/tests/unit/runtime-event-tail.test.ts` (new)
- `web/tests/ui/runtime-sse.spec.ts` (new)
- `web/tests/ui/fixtures/runtime-sse.html`, `runtime-sse.tsx`, `runtime-sse-harness.tsx`,
  `runtime-sse-fixture.ts` (controlled transport fixture; no product route, store or service registered)

`web/src/hooks/useRuntimeQueries.ts` was not modified: the frozen `events(runId, afterSeq)` key and the resource
keys the tail invalidates already exist from E-01 and match the card.

## Behaviour

| Concern | Implementation |
| --- | --- |
| Transport | Reuses `useSSECursor`/`streamFetch` (`GET /api/runtime/v1/runs/{run_id}/events?after_seq=N`), finite retry, abort, visibility pause; no parser, retry loop or polling substitution is added. |
| Cursor/dedupe | Transport cursor commits after successful delivery; the hook additionally validates `id === DTO.seq` and `run_id`, and `mergeRuntimeEvent` dedupes by `seq` and re-sorts. |
| Cache mapping | Every accepted event merges into all cached timeline pages for the Run; `run.*`/`budget.*` invalidate the Run, `agent.*` invalidates Run + Attempts, `approval.*` invalidates Approvals, `effect.*` invalidates Effects, any `operation_id` invalidates that Operation. No optimistic status/success writes. |
| Terminal | Terminal canonical statuses (`succeeded|failed|canceled|parked`) and `run.completed|run.parked|run.canceled` close the reader and keep `lastEventAt`; transport close alone changes nothing. |
| Visibility | `visibilitychange` to visible invalidates the Run once; hidden performs no request (transport pauses reconnect timers, cursor preserved). |
| Lifecycle | One reader per Run; abort on unmount/route change; `enabled` gates start, and the detail page will pass non-terminal server status. |

## TDD evidence

1. RED: `npm run test:unit -- runtime-event-tail` -> `Failed to resolve import "@/hooks/useRuntimeEventTail"`.
2. Two self-inflicted test defects were corrected before GREEN: a mocked `visibilityState` spy that left the
   document hidden for later cases, and a `mockResolvedValueOnce` that bypassed the recording `fetch` mock.
   One assertion was also corrected to match real transport semantics (a closed stream retries within the
   finite budget, so "transport close" is asserted as "no terminal fact", not "reader closed").
3. GREEN: `npm run test:unit -- runtime-event-tail` -> 1 file / 9 tests PASS.

## Final verification

| Command | Result | Evidence |
| --- | --- | --- |
| `npm run test:unit` | PASS | 13 files / 269 tests (260 + 9) |
| `npx playwright test tests/ui/runtime-sse.spec.ts` | PASS | 2/2 at 1280 and 1440 |
| `npx playwright test tests/ui` | 31/32 | The only failure is `approval-ui.spec.ts:112` ("preview surfaces never claim an effect was applied"), reproduced identically in the untouched D worktree at `f461a3c` -> pre-existing baseline failure, not an E-02 regression. |
| `npm run lint` | PASS | 0 errors, 68 pre-existing warnings (an earlier new fixture warning was removed by splitting the fixture module) |
| `npm run build` | PASS | existing >500 kB chunk warning only |
| `git diff --check` | PASS | no whitespace errors |

## Boundaries

- Controlled SSE transport proves protocol/cache/lifecycle behaviour only; real host API + Worker execution is
  `NOT RUN`.
- The fixture registers no application route and exposes no fake product state.
