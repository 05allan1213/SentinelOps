# Task E-07 implementation report

Status: **PASS**. Capabilities / Safety / Worker Health pages with read-only Eval/Release/Retention sections are
implemented, routed and browser-verified at 1280/1440. All four `Agent Runtime` navigation links now resolve.

Base: `3848db2` (E-06).

## Files

- `web/src/pages/runtime/capabilities.tsx`, `safety.tsx`, `worker-health.tsx`
- `web/src/pages/runtime/components/CapabilityTable.tsx`, `SafetySummary.tsx`, `WorkerHealthTable.tsx`,
  `EvalReleaseSummary.tsx`
- `web/src/App.tsx` (registers `/runtime/capabilities`, `/runtime/safety`, `/runtime/worker-health`; the E-03
  navigation entries therefore no longer point at unresolved routes)
- `web/tests/ui/runtime-capabilities.spec.ts`

## Behaviour

| Surface | Implementation |
| --- | --- |
| Capabilities | Read-only table with `source`, `configured_state`, `observed_worker_state`, risk, revision, schema hash, required gate/role, allowed agents, last observed time and per-item metadata. Configured and observed are separate columns; an unobserved capability renders `not_observed` and never `loaded`/healthy. |
| Safety | Shadow-mode banner (L1/L2 effective writes closed), static/dynamic/current-effective caps, policy hash, catalog revision and gate-audit availability; explicitly states that gate changes stay on the existing admin settings API. |
| Worker Health | Persisted worker rows (status, heartbeat, runtime/compatibility, active Run + generation, observed MCP/Skill counts, last error) and nullable aggregates rendered as `—` rather than a zero-valued healthy count; missing observations show the explicit unavailable/`not_observed` panel. |
| Eval / Release / Retention | Read-only sections on the Worker Health route: Agent Eval (`not_run` when absent, explicitly distinguished from the existing RAG Eval page), Release (persisted version/worker versions; `gray_state`/`rollback_state` stay `—` when no durable fact exists) and Retention (policy + last-cleanup evidence with its own reason code). No release/rollback/cleanup controls exist. |
| Read-only guarantee | The only interactive control on each page is refresh; the specs assert there are no form controls (`input/select/textarea/[role=switch]`) inside `main`. |

## TDD evidence

1. RED: the spec ran before the pages/routes existed (`/runtime/capabilities` etc. unresolved).
2. Own defects fixed during GREEN: the read-only assertions were initially scoped to the whole document and
   counted sidebar buttons/inputs — now scoped to `main`; the Runs landing request is mocked so the navigation
   case stays deterministic.
3. GREEN: `runtime-capabilities.spec.ts` 3/3.

## Final verification

| Command | Result | Evidence |
| --- | --- | --- |
| `npx playwright test tests/ui/runtime-capabilities.spec.ts` | PASS | 3/3 (2 widths × navigation/table/safety/health/eval-release-retention + unavailable state) |
| Full desktop UI suite, `--workers=1` | 55/56 | Only the pre-existing `approval-ui.spec.ts:112` baseline failure (identical at `f461a3c`) |
| `npm run test:unit` | PASS | 13 files / 269 tests |
| `npm run lint` | PASS | 0 errors, 68 pre-existing warnings |
| `npm run build` | PASS | existing >500 kB chunk warning only |
| `git diff --check` | PASS | no whitespace errors |

## Boundaries

- No capability toggle, Skill editor, MCP secret form, release/rollback control, database management action or
  Status center was added; the frozen navigation gains no extra item.
- Controlled fixtures prove the UI contract only; real provider/Worker execution and hosted CI remain `NOT RUN`.
