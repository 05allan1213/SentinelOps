# Task E-05 brief (coordinator)

Task card: read `output/final-implementation-plan-2026-08-31.md` lines 1711-1755 (section `### Task E-05: Add Attempts, Checkpoint metadata and Recovery operation workflow`) in full; it is the authoritative scope.

## Environment

- Work in `/home/monody/project/.worktrees/sentinelops-e` (branch `feat/phase-e-20260910`). Do NOT edit `/home/monody/project/SentinelOps`.
- Prerequisites already committed: E-00 direction, E-01 data layer, E-02 tail, E-03 Runs page, E-04 detail shell. Read them first; treat their frozen names/props as given.
- Commands from `web/`: `npm run test:unit`, `npm run lint`, `npm run build`, `npx playwright test tests/ui/runtime-recovery.spec.ts --project=chromium`. Desktop 1280/1440 only.

## Scope

- Create `web/src/pages/runtime/components/AttemptsPanel.tsx`, `CheckpointPanel.tsx`, `RecoveryDialog.tsx`, `OperationProgress.tsx`.
- Modify `web/src/pages/runtime/detail.tsx` and/or `RunDetailTabs.tsx` only to mount these panels into the existing `attempts` tab (E-04 left an explicit placeholder there) and to render `OperationProgress` for an accepted operation.
- Modify `web/src/components/common/ConfirmDialog.tsx` only if the Recovery wrapper genuinely needs its controlled API; otherwise leave it untouched.
- Test: `web/tests/ui/runtime-recovery.spec.ts`.
- Do not create E-06 panels, E-07 pages, Go code, dependencies, a new Radix/shadcn design system, a second client/store, or F work.

## Interfaces (frozen)

- `RecoveryDialog` props `{run: RunDetailDTO; action: RecoveryAction; open: boolean; onClose: () => void; onAccepted: (operationId: string) => void}`; it requires a non-empty reason and an expected generation, and includes expected compatibility only for `restore`.
- `OperationProgress` consumes `OperationDTO` and renders `accepted|running|succeeded|failed|canceled|rejected`, `terminal`, `idempotent_replay`, reason/error/correlation seq; it polls `GET operation` only while non-terminal and stops at terminal.
- Recovery controls appear only when the server `allowed_recovery_actions` contains the action AND the current role is admin. The client may never add or synthesise an action.
- Reuse `useRuntimeAttempts`, `useRuntimeCheckpoints`, `useRuntimeOperation`, `runtimeService.recoverRun`, existing approval/effect permissions, `ConfirmDialog` until the F-05 primitive decision.

## Coordinator instructions

1. Attempts panel: each Attempt's mode (`fresh|resume|replay`), Worker, generation, trace, retry/failover counts, checkpoint fingerprint, failure and quality. Missing fields render partial/unknown, never guessed values.
2. Checkpoint panel: ID/key/hash/runtime/generation/state/time plus explicit missing/corrupt/expired/incompatible reason. Never fetch or render opaque bytes.
3. Recovery dialog: explain impact, require non-empty reason and expected generation, disable submit while pending, send exactly one idempotency key per user submission, and surface the returned `operation_id` immediately.
4. Operation progress: reconnects by operation ID after route reload and reads final status from the server; invalidates Run/Attempts/Timeline/Effects only after server facts. Never optimistic success, never invoke Agent/Effect from the browser.
5. Resume/Replay/Cancel/Restore labels and disabled reasons mirror the server legality matrix. Legacy, terminal, incompatible or dependency-invalid Runs expose no bypass button.
6. TDD: write `runtime-recovery.spec.ts` first, run it before implementation, and record the expected failure verbatim. Required coverage: attempt modes, opaque-checkpoint exclusion, admin/operator/viewer control visibility, required reason/generation, `202` operation, same-key replay, different-key conflict, operation reload/reconnect, failed/canceled/parked outcomes, reduced-motion behavior.
7. Verify: focused spec at both viewports, full `npm run test:unit`, `npm run lint`, `npm run build` (68 warnings + large chunk are baseline), plus the existing smoke/approval UI suites. Fix failures before committing.
8. Commit only your files with the exact message `feat(web-runtime): add attempts checkpoints and recovery progress`. No push. On `index.lock`, wait and retry.
9. Write `.superpowers/sdd/final-implementation-plan-2026-08-31/task-E-05-report.md` with status, files, RED/GREEN evidence, the recovery legality/disabled-reason table, and the command/PASS-FAIL-NOT RUN evidence.

## Stop conditions

Stop and report if the E-01 operation/recovery contract cannot express legality, idempotency replay or terminal status; if the Recovery wrapper would require a new design-system primitive beyond the allowed `ConfirmDialog` change; or if any test would treat a fixture `202` as real Agent execution evidence.
