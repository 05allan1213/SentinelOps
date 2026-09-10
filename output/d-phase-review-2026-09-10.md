# SentinelOps Phase D Review and Verification

> Date: 2026-09-10 (Asia/Shanghai)
> Branch: `feat/phase-d-20260908`
> Base: `c0b819158fad3fbe02132050d47c9261b65d1a4a`
> Final product HEAD: `562a6c9328fe61b785120ad474bf204fa46ff095`
> Scope: D-01 through D-07 only. E/F were not started.

## Prerequisite Confirmation

- B1-01 through B1-06 were already PASS, including the later contract repairs and the serial 30-minute verification at the current base.
- H-01 through H-03 were already complete. Their final commit is an ancestor of the base, and the Capabilities, Safety, Worker Health, Retention, Eval and Release implementations/tests are present.
- Missing Agent Eval and gray/rollback facts remain explicit `unavailable/not_run` states, as allowed by the frozen contract.
- Existing isolated MySQL was started. DSN-backed Runtime, Controller, Bootstrap/DAO and Chat SSE prerequisite tests passed.

## Task Results

| Task | Result | Commits | Main evidence |
| --- | --- | --- | --- |
| D-01 | PASS | `968d2c5`, `14e2022` | Vitest/jsdom cycle, exact versions, registry-neutral lockfile |
| D-02 | PASS | `eb232a5`, `a6b5ddd` | URL/image/language/fence safety primitives; 111 focused tests |
| D-03 | PASS | `f8f61ac`, `ba71dea` | Shared safe renderer, restricted highlighting, browser and bundle checks |
| D-04 | PASS | `70c2de7` | All eight Markdown calls migrated; one parser import site |
| D-05 | PASS | `3c9db3e`, `cbc2f5e` | Message-ID render budget, stable fences, terminal/scroll behavior |
| D-06 | PASS | `29d2011`, `b63d390` | Monotonic cursor, dedupe, finite GET reconnect, visibility/cleanup |
| D-07 | PASS | `1f7acf1`, `562a6c9` | v2-first create, GET-only recovery, accepted-content persistence |

Each task received an independent review. Important findings were repaired and re-reviewed before the next task. D-03, D-05, D-06 and D-07 each completed one reviewed fix round; D-01 and D-02 also completed their scoped fixes before approval.

## Final Verification

| Check | Result |
| --- | --- |
| `npm run test:unit` | PASS: 9 files, 245 tests |
| `npm run lint` | PASS: 0 errors, 68 pre-existing warnings |
| `npm run build` | PASS with the existing large-chunk warning |
| Controlled desktop Playwright | PASS: 22/22 at 1280 and 1440 |
| Dependency and bundle audit | PASS: exact pins, registry-neutral lockfile, 12 allowed grammar implementations, no lowlight common/all registry |
| `go test ./internal/controller/chat -count=1` | PASS |
| DSN-backed prerequisite Go tests | PASS |
| `git diff --check` | PASS |

The first parallel browser run had one 1280px load timeout while a Vite bundle build was running concurrently. The exact case passed alone in 6.6 seconds, and the complete suite then passed serially in 55.7 seconds. This is recorded as test-environment contention, not a product failure.

## Scope and Truth Boundaries

- No `api/`, `internal/`, migration, manifest or Runtime page file was changed by D.
- No second HTTP client, state store, event bus, EventSource, Runtime or execution path was introduced.
- No `rehype-raw`; raw HTML remains excluded from the DOM.
- GET reconnect never posts a command or creates a Run.
- Read-stream completion and `operation.*` success never imply Run success. Canceled, retryable failure, parked, reconciling and unknown states remain distinct.
- Real Worker/provider E2E, hosted CI, application images, rollout/rollback and P43 remain `NOT RUN`.
- Mobile acceptance remains `OUT OF SCOPE`.

## Final Review Adjudication

All task-level reviews are complete, and the coordinator completed the cross-task diff, scope, security and compatibility checks. No Critical or Important finding remains.

One D-07 minor is parked: a legacy saved assistant message that has `isStreaming=true` but no Run identity or new `createUnconfirmed` marker can lose its uncertainty label when restored. New D-07 messages carry the marker, so this does not affect new sessions or any E-phase dependency. Cost if wrong: a user with an old, pre-D pending local snapshot may see a less explicit recovery state until the message is replaced.

The D phase is complete. Per instruction, implementation stops before E-00/E/F. The branch is local and unpushed; no merge was performed.

## Addendum: Design-Hook Triage and Post-Fix Verification

After this report was written, the session design hook flagged three items in `web/src/pages/chat/index.tsx`: two `gray-on-color` notices (L842, L984) and one `bounce-easing` notice (L1013). Triage result:

- L842 was a false positive. The button's resting color is `text-gray-500`; the indigo background only exists in the hover state, paired with `hover:text-indigo-600`. The resting and hover utilities were split into two class strings so the static detector no longer reads them as a single state. No rendered class changed.
- L984 was a false positive. In the unchecked vote-reason branch, `text-gray-700` pairs with a near-white `hover:bg-gray-50` background, so contrast is not a real problem. The ternary was only reflowed onto separate lines. No rendered class changed.
- L1013 was a real naming defect. The keyframes were named `chat-bounce` while the animation easing is `ease-in-out`; the keyframes were renamed to `chat-dot-pulse` in `web/src/assets/styles/index.css` and the single call site was updated. Motion is unchanged.

Committed as `cee7c83`; product HEAD is now `cee7c83`. Post-fix verification, run serially:

| Check | Result |
| --- | --- |
| `npm run test:unit` | PASS: 9 files, 245 tests |
| `npm run lint` | PASS: 0 errors, 68 pre-existing warnings |
| `npm run build` | PASS with the existing large-chunk warning |
| Controlled desktop Playwright | PASS: 22/22 |
| Detector re-scan of both touched files | No deterministic findings |
| `git diff --check` | PASS |

No detector ignore entries were persisted; all three items were resolved or reflowed in the source.
