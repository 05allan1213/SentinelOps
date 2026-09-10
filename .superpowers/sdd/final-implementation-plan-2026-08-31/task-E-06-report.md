# Task E-06 implementation report

Status: **PASS**. Effects, Evidence, Context and Trace tabs are implemented with explicit expansion,
redaction-safe rendering and panel-level states; browser-verified at 1280/1440.

Base: `6d8050a` (E-05).

## Files

- `web/src/pages/runtime/components/EffectsPanel.tsx`
- `web/src/pages/runtime/components/EvidenceInspector.tsx`
- `web/src/pages/runtime/components/ContextPanel.tsx`
- `web/src/pages/runtime/components/TracePanel.tsx`
- `web/src/pages/runtime/detail.tsx` (the four tabs now mount the real panels; no placeholders remain)
- `web/tests/ui/runtime-detail-resources.spec.ts` (new)
- `web/tests/ui/runtime-detail-overview.spec.ts` (Trace assertion updated from placeholder to real panel;
  sub-resources mocked so the spec stays deterministic)
- `web/tests/ui/runtime-recovery.spec.ts` (operator scenario now waits for the server-driven buttons first)

## Behaviour

| Tab | Implementation |
| --- | --- |
| Effects | Primary/Derived grouping, status tone (unknown/reconciling never success), tool/revision, proposal/target/schema hashes, idempotency digest, generation/attempt, external reference, reconciliation attempts/resolution/actor and the full history list. Unknown/reconciling Effects expose the canonical controlled reconciliation for admin only (`opsService.resolveEffect` / `acceptUnknownEffect` on `/ops/v1/effects/...`), invalidating the Runtime effects query after the server response with no optimistic status change. |
| Evidence | Default rows show ID/source/version/hash/scope/vector/rerank/answer relation and whether a quote exists; `quote` is fetched only after the explicit expand action (`?include=quote`), rendered through `MarkdownRenderer`, with an unavailable/forbidden note when the server refuses. |
| Context | Metadata-only by default (identity/role/scope/revisions/hashes/deadline/gate keys/counts); `include=history` is a separate permission-gated query and renders redacted history with truncation/redaction flags. |
| Trace | Run→Attempt→trace rows with status, trace quality, duration/tokens/cost/node count; the existing Trace detail link is rendered only when `raw_available` and `detail_url` are server-provided, otherwise an explicit unavailable reason is shown. |
| States | Each panel has its own loading/empty/partial/unavailable/failed presentation with `availability`/`data_quality`/`reason_code`/`not_run`; local scrolling only. |

## TDD evidence

1. RED: the spec ran before the panels existed (missing tab content).
2. Own defects fixed during GREEN: the last state assertion matched the page header's quality chip instead of the
   Trace panel's (now scoped); the E-04 spec's Trace placeholder assertion was updated because E-06 replaced it.
3. GREEN: `runtime-detail-resources.spec.ts` 4/4; full Runtime UI set 23/23 in 29.7s.

## Final verification

| Command | Result | Evidence |
| --- | --- | --- |
| Runtime UI suites (`runtime-runs`, `runtime-sse`, `runtime-detail-overview`, `runtime-recovery`, `runtime-detail-resources`) | PASS | 23/23 at 1280/1440 |
| `npm run test:unit` | PASS | 13 files / 269 tests |
| `npm run lint` | PASS | 0 errors, 68 pre-existing warnings |
| `npm run build` | PASS | existing >500 kB chunk warning only |
| Backend Runtime APIs with disposable MySQL DSN | PASS | `go test -p 1 ./api/runtime/... ./internal/service/runtime ./internal/controller/runtime -count=1 -timeout=15m` (Scope/Redactor/Evidence/Trace service tests included) |
| `git diff --check` | PASS | no whitespace errors |

## Boundaries

- Reconciliation calls the existing canonical `/ops/v1` admin paths only; the browser never executes an Agent,
  Tool or external Effect, and no optimistic success is rendered.
- Controlled fixtures prove the UI contract; real Worker/provider execution remains `NOT RUN`.
