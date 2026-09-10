# Task E-05 implementation report

Status: **PASS**. Attempts/Checkpoint panels and the admin Recovery workflow (dialog → 202 operation →
polling → terminal facts) are implemented and browser-verified at 1280/1440.

Base: `d2dbf24` (E-04).

## Files

- `web/src/pages/runtime/components/AttemptsPanel.tsx`
- `web/src/pages/runtime/components/CheckpointPanel.tsx`
- `web/src/pages/runtime/components/RecoveryDialog.tsx`
- `web/src/pages/runtime/components/OperationProgress.tsx`
- `web/src/pages/runtime/detail.tsx` (Attempts tab mounts the panels, recovery action bar, dialog and
  operation progress; operation id persists in `sessionStorage` per Run for reconnect)
- `web/src/hooks/useRuntimeQueries.ts` (+`useRuntimeOperationPolling`: same key as `useRuntimeOperation`,
  `refetchInterval` stops when the server reports `terminal`)
- `web/tests/ui/runtime-recovery.spec.ts`, `web/tests/ui/fixtures/runtime-detail.ts` (fixture summary-merge fix),
  `web/tests/ui/runtime-detail-overview.spec.ts` (Attempts-tab assertion updated from placeholder to real panel)
- `ConfirmDialog` was deliberately not modified: the recovery form needs generation/compatibility fields, so a
  dedicated dialog reuses the existing visual language. F-05 still owns the shared focus primitive.

## Behaviour

| Concern | Implementation |
| --- | --- |
| Attempts | Mode (`fresh/resume/replay`), status/phase, worker, generation, trace, operation, retry/failover counts, run/checkpoint compatibility hashes, executing-worker fingerprint, failure fields and per-item quality; unavailable projections render as unavailable, never as empty success. |
| Checkpoints | ID/key/payload SHA256/runtime version/compatibility/generation/times with `valid/missing/corrupt/expired/incompatible` states and `reason_code`; opaque bytes are never requested or rendered. |
| Recovery gating | Buttons come only from the server `allowed_recovery_actions`; disabled with an explicit reason for non-admin, legacy rows, terminal Runs (except `restore`) and Restore without exact compatibility. |
| Dialog | Action-specific impact copy, required non-empty reason, required expected generation, required expected compatibility hash for `restore`, submit disabled while invalid/pending, one idempotency key per mounted dialog, immediate display of the returned `operation_id`. |
| Operation | Polls `GET /runtime/v1/operations/{id}` only while non-terminal, renders accepted/running/succeeded/failed/canceled/rejected, terminal, idempotent replay, reason/error/correlation seq; on terminal it invalidates Run/Attempts/Timeline/Effects. No optimistic Run success and no browser-side Agent/Effect call. |
| Reconnect | The accepted operation id is stored per Run in `sessionStorage`, so the progress panel resumes after a reload or tab switch. |

## TDD evidence

1. RED: the spec was authored before the panels/dialog existed; all recovery assertions failed.
2. Own defects fixed during GREEN: the detail fixture's final `...overrides` spread clobbered the built summary
   (Runtime mode undefined → every action read as legacy); the first scenario used a `parked` Run where Resume is
   legitimately illegal, so it now uses a non-terminal `retryable_failed` Run; the role-switch-by-reload case was
   split into separate operator/admin scenarios; the events sub-resource is mocked with a `[DONE]` frame.
3. GREEN: `runtime-recovery.spec.ts` 7/7 in 24.5s; the previously slow run (120s timeouts) is gone.

## Final verification

| Command | Result | Evidence |
| --- | --- | --- |
| `npx playwright test tests/ui/runtime-recovery.spec.ts` | PASS | 7/7 (2 widths × admin flow, operator gating, admin conflict/idempotent replay + reload/failure reconnect) |
| `npx playwright test tests/ui/runtime-detail-overview.spec.ts` | PASS | 5/5 after the Attempts assertion update |
| `npx playwright test tests/ui/runtime-runs.spec.ts tests/ui/runtime-sse.spec.ts` | PASS | 7/7 |
| `npm run test:unit` | PASS | 13 files / 269 tests |
| `npm run lint` | PASS | 0 errors, 68 pre-existing warnings |
| `npm run build` | PASS | existing >500 kB chunk warning only |
| `git diff --check` | PASS | no whitespace errors |

## Boundaries

- Controlled API fixtures prove the UI contract only; no Agent/Effect execution was triggered from the browser
  and real Worker/provider execution remains `NOT RUN`.
- The dialog is a local component; the shared accessible primitive stays F-05's scope.
