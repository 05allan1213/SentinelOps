# Task E-06 brief (coordinator)

Task card: read `output/final-implementation-plan-2026-08-31.md` lines 1756-1800 (section `### Task E-06: Add Effects, Evidence, Context and Trace detail tabs`) in full; it is the authoritative scope.

## Environment

- Work in `/home/monody/project/.worktrees/sentinelops-e` (branch `feat/phase-e-20260910`). Do NOT edit `/home/monody/project/SentinelOps`.
- Prerequisites already committed: E-00 direction, E-01 data layer, E-02 tail, E-03 Runs page, E-04 detail shell, E-05 attempts/checkpoints/recovery. Read them first.
- Commands from `web/`: `npm run test:unit`, `npm run lint`, `npm run build`, `npx playwright test tests/ui/runtime-detail-resources.spec.ts --project=chromium`. Desktop 1280/1440 only.

## Scope

- Create `web/src/pages/runtime/components/EffectsPanel.tsx`, `EvidenceInspector.tsx`, `ContextPanel.tsx`, `TracePanel.tsx`.
- Modify `web/src/pages/runtime/detail.tsx` and/or `RunDetailTabs.tsx` only to mount these panels into the existing `effects|evidence|context|trace` tabs (E-04 left explicit placeholders there).
- Test: `web/tests/ui/runtime-detail-resources.spec.ts`.
- Do not create E-07 pages, Go code, dependencies, a second client/store, a database editor, or F work.

## Interfaces (frozen)

- `EffectsPanel` consumes `EffectsRes` and renders the Primary/Derived DAG and history with status, parent, type, hashes, idempotency digest, generation, Attempt, external reference and reconciliation evidence.
- `EvidenceInspector` consumes `EvidenceRes`; the default row shows ID/source/version/hash/scope/vector/rerank/answer relation and requests `/evidence/{id}?include=quote` only on explicit expand.
- `ContextPanel` consumes `ContextDTO`; default metadata shows Identity/Role/Scope/revisions/hashes/deadline/gates/counts. `include=history` is a separate permission-gated query.
- `TracePanel` consumes `TraceAggregateDTO`; it links to the existing Trace detail only when `raw_available=true`, otherwise shows reason/quality.
- Reuse `useRuntimeEffects`, `useRuntimeEvidence`, `useRuntimeContext`, `useRuntimeTraces`, `DataBlock`, `MarkdownRenderer`, and the existing Ops resolve/accept-unknown service only for controlled actions.

## Coordinator instructions

1. Render Effect state history without turning unknown/reconciling into success. Expose an existing controlled resolve/accept-unknown action only to an authorized admin and invalidate after the server response, with no optimistic state.
2. Keep Evidence quote/content preview hidden until explicit user action. A forbidden result must not leak whether another Run owns the ID. Render returned quotes through `MarkdownRenderer` and keep safe link/image rules.
3. Display Context hashes, revision numbers and metadata; raw history stays a separate explicit request with a redacted response. Never expose RuntimeHandler handles or provider credentials.
4. Group Trace rows by Run→Attempt→trace ID, show retry/failover physical calls and trace quality, and keep incomplete traces visibly diagnostic rather than release evidence.
5. Each panel has its own loading/empty/partial/unavailable/failed state; long DAG/table/code values scroll locally inside the panel. No page-level horizontal overflow at 1280/1440.
6. TDD: write `runtime-detail-resources.spec.ts` first, run it before implementation, and record the expected failure verbatim. Required coverage: Primary/Derived effects, unknown/reconciling semantics, controlled reconciliation, Evidence metadata vs explicit quote permission, Context revisions, Trace attempt grouping and raw-unavailable, all panel states.
7. Verify: focused spec at both viewports, full `npm run test:unit`, `npm run lint`, `npm run build` (68 warnings + large chunk are baseline), plus the existing smoke/markdown UI suites. Fix failures before committing.
8. Commit only your files with the exact message `feat(web-runtime): add effect evidence context and trace tabs`. No push. On `index.lock`, wait and retry.
9. Write `.superpowers/sdd/final-implementation-plan-2026-08-31/task-E-06-report.md` with status, files, RED/GREEN evidence, the per-panel state/content table, and the command/PASS-FAIL-NOT RUN evidence.

## Stop conditions

Stop and report if a default-mount panel would render raw provider/prompt/secret/opaque content, if `include=quote|history` cannot be expressed as an explicit user action, or if evidence would require presenting fixtures as real Runtime content.
