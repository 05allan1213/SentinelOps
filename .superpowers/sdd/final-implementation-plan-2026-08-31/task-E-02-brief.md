# Task E-02 brief (coordinator)

Task card: read `output/final-implementation-plan-2026-08-31.md` lines 1577-1618 (section `### Task E-02: Connect active Run SSE to cache with visibility and terminal lifecycle`) in full; it is the authoritative scope.

## Environment

- Work in `/home/monody/project/.worktrees/sentinelops-e` (branch `feat/phase-e-20260910`). Do NOT edit `/home/monody/project/SentinelOps`.
- E-01 must already be committed (`web/src/types/runtime.ts`, `web/src/services/runtime.ts`, `web/src/hooks/useRuntimeQueries.ts`). Read them first; treat their frozen names as given.
- Commands from `web/`: `npm run test:unit`, `npm run lint`, `npm run build`, `npx playwright test <spec> --project=chromium`.

## Scope

- Create `web/src/hooks/useRuntimeEventTail.ts`.
- Modify `web/src/hooks/useRuntimeQueries.ts`.
- Test: `web/tests/unit/runtime-event-tail.test.ts`, `web/tests/ui/runtime-sse.spec.ts`.
- Bounded addition authorized: if the browser spec needs a harness before the Runtime pages exist (they arrive in E-03/E-04), add a minimal fixture under `web/tests/ui/fixtures/` in the same style as D-03's `markdown.html`/`markdown.tsx` fixture. A fixture must not register a production route or fake product behavior, and must be labelled as controlled-transport evidence.
- Do not modify the E-01 service/types files beyond what the card allows (`useRuntimeQueries.ts` only), and do not create pages (E-03/E-04), Go code, dependencies, a second client/store/event bus, or F work.

## Interfaces (frozen)

- `useRuntimeEventTail({runId, enabled, onError?})` returns `{connected: boolean; afterSeq: number; lastEventAt: string | null; error: Error | null; retry: () => void}`.
- Consumes `GET /api/runtime/v1/runs/{run_id}/events?after_seq=N` (service path `/runtime/v1/...` on the sole `api` instance), updates `runtimeQueryKeys.run/timeline/attempts/effects/operation` with targeted `setQueryData`/invalidation per event type, and never creates a Run.
- Dedupe key is `(run_id, seq)`; the cursor is a ref and only scoped to the current Run/session view.

## Coordinator instructions

1. Reuse D-06 transport: `web/src/hooks/useSSECursor.ts` (`streamFetch`, `SSECursor`, `bindSSEVisibility`) already provides finite retry, monotonic cursor, transport dedupe, visibility pause/resume, abort and listener cleanup. Read it and the D-06 tests before writing anything; do not build a second parser/retry loop. If something is missing for the cache layer, extend the consumer side, not the transport.
2. Cache mapping rules: append accepted events into the Timeline cache (sorted by `seq`, deduped by `(run_id, seq)`, no duplicates or reordering), update the run cache only from canonical server facts, and invalidate the child resource whose facts changed (`run.*`, `agent.*`, `approval.*`, `effect.*`, `budget.*`, `operation.*`). A transport close or `[DONE]` is never Run success; `parked`/`reconciling`/`retryable_failed`/`canceled` must not be mapped to success.
3. Lifecycle: start only when `enabled` and the server status is non-terminal and the Run ID is valid. Stop/abort on route change/unmount, keep exactly one reader per Run, and on `visibilitychange` hidden stop nonessential reconnect timers while preserving the cursor; on visible refetch Run once and resume after the last seq. Use `document.visibilityState`.
4. Terminal facts (`succeeded`, `failed`, `canceled`, `parked`) close the reader and leave `lastEventAt` set. Expose connection/error state separately from Run status.
5. TDD: write the failing unit spec first and record the RED output verbatim. Required coverage: event→cache mapping, duplicate and out-of-order frames, hidden→visible transition, terminal close, unmount abort, retry, operation correlation. Then implement and rerun to GREEN.
6. Browser evidence: `runtime-sse.spec.ts` runs against a controlled SSE transport at 1280 and 1440, proving single reader/no duplicate timeline rows/terminal close/visibility behavior. Record real host API/Worker execution as `NOT RUN` unless the environment actually provides it.
7. Verify: focused unit spec, full `npm run test:unit`, `npm run lint`, `npm run build` (68 pre-existing warnings and the large-chunk warning are baseline), plus the Playwright spec. Fix failures before committing.
8. Commit only your files with the exact message `feat(web-runtime): stream runtime events into query cache`. No push. If `index.lock` exists, wait and retry.
9. Write `.superpowers/sdd/final-implementation-plan-2026-08-31/task-E-02-report.md` with status, files, RED/GREEN evidence, cache-mapping table, lifecycle table, and command/PASS-FAIL-NOT RUN evidence.

## Stop conditions

Stop and report if the E-01 interfaces are insufficient (e.g. missing a query key needed by the card), if the transport hook cannot express the required lifecycle without changing D-06 semantics, or if evidence would require presenting controlled fixtures as real Runtime execution.
