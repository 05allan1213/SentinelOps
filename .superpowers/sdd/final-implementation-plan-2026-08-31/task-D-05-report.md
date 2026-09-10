# D-05 implementation report

Task: Make streaming Markdown fence handling incremental and stable.
Base: `70c2de71d59cb74c6b341435614aacbb9fa68366`.
Result: PASS.

## Implementation

- Added a caller-only `useStreamRenderScheduler` hook. Its `useRef` map holds message-ID cursors, the last-render timestamp, pending field callbacks, and timer/RAF handles. Each message shares a 100 ms minimum interval followed by an animation frame; completion flushes pending callbacks immediately and removes that cursor. Unmount cancels pending render work. No second message state, store, transport, or event bus was added.
- Replaced the global text accumulator with a cursor captured by each stream's existing callback closure. Main content and thinking updates feed the existing Chat messages state through the scheduler; identical rendered input is memoized, empty content chunks do not trigger parsing, and repeated nonempty deltas retain their actual text.
- Chat and Thinking now each keep one shared MarkdownRenderer call through streaming/completion. Removed Chat normalization from the hot path. Lifecycle flags come from the owning message, independently of its list position or the page loading boolean.
- Open fences retain a stable `pre[data-streaming-markdown]` containing escaped raw text. A bounded update with closed fences enables the existing restricted highlighter. Extended the existing fence helper to recognize up to three leading spaces and quote/list containers while rejecting literal four-space or quote-prefixed closers in a top-level fence.
- Completion forces one final parse without replacing the renderer/code DOM. The parser boundary retains readable raw content on failure and retries on the completion transition even when final content is identical. Highlight exceptions retain the D-03 raw-code fallback.
- Assistant bubble widths are constrained with `min-w-0`, `max-w-full`, and a stable full-width container. Bottom following is conditional on the user's scroll position; layout updates and ResizeObserver retain the bottom anchor, while a user scrolled up keeps control.
- Streaming-only cursor/status/planning/thinking animations stop at terminal message state; active indicators honor reduced motion. Stopped thinking remains readable without being labeled completed. Synchronous request failure also clears stream-only presentation flags.
- Fixed D-04's loose JSX streaming assertion: it now examines individual MarkdownRenderer opening tags. Consumer counts reflect the two legitimate Chat/Thinking calls instead of preserving duplicate branches.

## TDD evidence

Commands run from `web` unless explicitly stated otherwise.

### RED: caller streaming behavior

Command: `npm run test:unit -- tests/unit/chat-streaming-markdown.test.tsx`

Before implementation: FAIL, 3 failed / 1 passed. Relevant output:

```text
batches split fence chunks ...: expected a Node, received null
bounds parses ...: expected 21 to be 1
 does not parse prose for every token ...: expected 30 to be 0
Test Files 1 failed (1)
Tests 3 failed | 1 passed (4)
```

The failures exposed the missing stable raw-pre marker, reparsing on every identical/empty update, and parsing every incoming prose token. The existing readable parser-error fallback already passed.

### RED: fence containers and same-content parser recovery

- `npm run test:unit -- tests/unit/markdown-safety.test.ts`: FAIL, 3 failed / 116 passed after adding indented/quote/list fence cases; all three open-container cases were incorrectly considered closed.
- `npm run test:unit -- tests/unit/chat-streaming-markdown.test.tsx`: FAIL, 1 failed / 6 passed after adding completion recovery with identical final text; the old error boundary stayed in fallback and never retried the parser.

### GREEN

Final focused command: `npm run test:unit -- tests/unit/chat-streaming-markdown.test.tsx`

```text
Test Files 1 passed (1)
Tests 7 passed (7)
```

The seven Chat tests exercise split fence tokens and stable raw-pre identity, pre-completion highlighting, empty updates and repeated actual deltas, bounded parsing, final/duplicate completion parse counts, renderer/code DOM identity and width classes, parser fallback and same-content completion recovery, per-message accumulator isolation, terminal animation removal, unmount cancellation, and independent 100 ms plus RAF gates. Tests use the real Chat/MarkdownRenderer and a mocked service boundary; they do not claim transport or provider verification.

## Verification

| Command / check | Result | Output / evidence |
| --- | --- | --- |
| `npm run test:unit` | PASS | 5 files, 181 tests passed; no warnings |
| Final focused Chat unit command above | PASS | 1 file, 7 tests passed after additionally asserting duplicate completion does not reparse |
| `npm run lint` | PASS | TypeScript and ESLint exit 0; 0 errors, 68 existing warnings, same count as D-04 |
| `npm run build` | PASS | TypeScript and Vite production build; JS 2149.74 kB / gzip 671.87 kB; existing >500 kB chunk warning remains |
| `npx playwright test tests/ui/chat-streaming-markdown.spec.ts --project=chromium` | PASS | 2 passed (9.0s), actual `/chat` at 1280 and 1440 |
| `npx playwright test tests/ui/chat-streaming-markdown.spec.ts tests/ui/markdown.spec.ts --project=chromium` | PASS | 6 passed (8.9s); delayed actual Chat plus shared Markdown/table/code/copy regressions |
| `git diff --check` | PASS | No whitespace errors |
| Real provider / runtime execution / D-06 transport / D-07 v2 migration / mobile / E / F / hosted CI / rollout | NOT RUN | Outside D-05 scope; fixture evidence is frontend behavior only |

The actual Chat Playwright fixture installs an asynchronous ReadableStream at the existing fetch boundary; each chunk arrives after 40 ms and goes through the unchanged service and SSE reader. It adds no application route. The legacy URL is isolated in `installDelayedStream` so D-07 can adapt that fixture boundary without replacing the behavioral assertions. The tests verify raw-pre node continuity, readable retained content, highlighting after closure, renderer and code node continuity at completion, invariant bubble width, no page overflow, bottom following, stable user-controlled scroll position, no running message animations under reduced motion, no terminal animation classes, and no page errors. Shared renderer cases continue to cover wide tables and clipboard source fidelity.

An initial browser run failed because the fixture appended ordinary text on the same line as its closing fence, correctly reopening raw mode. The fixture now emits a newline after the closing delimiter; the product was not changed to accept an invalid closer. An additional test-only assertion was corrected to account for Markdown trimming trailing paragraph whitespace. Two shell invocations used an incorrect working-directory/path combination and were rerun from `web`; those invocation errors provide no validation evidence.

## Files changed

- `web/src/pages/chat/index.tsx`
- `web/src/pages/chat/useStreamRenderScheduler.ts`
- `web/src/components/markdown/MarkdownRenderer.tsx`
- `web/src/components/markdown/markdown.ts`
- `web/tests/unit/chat-streaming-markdown.test.tsx`
- `web/tests/unit/markdown-consumers.test.tsx`
- `web/tests/unit/MarkdownRenderer.test.tsx`
- `web/tests/unit/markdown-safety.test.ts`
- `web/tests/ui/chat-streaming-markdown.spec.ts`
- `web/tests/ui/markdown.spec.ts`
- `.superpowers/sdd/final-implementation-plan-2026-08-31/task-D-05-report.md`

## Self-review and concerns

- Read the complete scoped diff. Self-review fixed an initially duplicated scroll-content ref and the same-content parser-recovery edge case, then reran covering checks.
- The existing Chat file remains large. The small scheduler hook isolates only transient render scheduling; no general store/state abstraction was introduced.
- The existing transport/reconnect behavior remains owned by D-06/D-07; this task does not establish replay correctness, provider success, or runtime readiness.
- No new lint errors or warnings. Existing lint/build warnings and Playwright's environment `NO_COLOR`/`FORCE_COLOR` notices remain.
- Shared `progress.md` edits and untracked `grill-truth.md` were preserved and excluded from the task commit. No ledger, truth file, dependency, service transport, or application route was changed.

## Review fix round 1 — 2026-09-10

Base: `3c9db3e`. Both Important review findings addressed. Result: PASS.

### Changes

1. The streaming fence guard now detects ambiguous nested container prefixes and list-continuation indentation. The reported `10. item\n\n    ```js\n    const unfinished = true` and `- > ```js\n  > const unfinished = true` examples stay in raw `pre[data-streaming-markdown]` mode and never invoke highlighting while streaming. Per the coordinator's explicit ruling, ambiguous container fences remain raw even after an apparent closing line until `complete=true`; completion delegates to the existing renderer/parser. This is a conservative display guard, not a second CommonMark parser, and introduces no per-token AST work.
2. Nonterminal planning events no longer call `finish`. Text, thinking and planning transformations are retained in arrival order and applied to the existing messages state in one scheduled message batch. Intermediate planning events are retained, and both the timer/RAF gate and last-render timestamp survive phase changes. `finish` remains only on the existing terminal completion/failure paths.
3. Self-review added an explicit scheduler rejection after unmount so background stream callbacks do not continue accumulating render batches. Existing background stream/transport behavior was not changed.

### RED

Command from repository root (equivalent to running the same test command in `web`):

```sh
npm --prefix web run test:unit -- tests/unit/chat-streaming-markdown.test.tsx tests/unit/markdown-safety.test.ts tests/unit/MarkdownRenderer.test.tsx
```

Before product fixes, recorded on the original fix-round turn:

```text
Test Files 3 failed (3)
Tests 5 failed | 163 passed (168)
```

The two helper regressions returned `true` instead of `false`; the two renderer regressions had no raw streaming pre because they were parsed/highlighted prematurely; the interleaved phase-event regression observed one parser invocation before the 100 ms/RAF gate instead of zero.

### GREEN and final checks

| Command | Result | Exact relevant output |
| --- | --- | --- |
| Focused command above | PASS | `Test Files 3 passed (3)`; `Tests 169 passed (169)` |
| `npm run test:unit` from `web` | PASS | `Test Files 5 passed (5)`; `Tests 187 passed (187)` |
| `npm run lint` from `web` | PASS | Exit 0; `68 problems (0 errors, 68 warnings)`; unchanged existing count |
| `npm run build` from `web` | PASS | TypeScript/Vite exit 0; JS `2149.96 kB`, gzip `671.99 kB`; existing >500 kB chunk warning |
| `npx playwright test tests/ui/chat-streaming-markdown.spec.ts tests/ui/markdown.spec.ts --project=chromium` from `web` | PASS | `6 passed (10.7s)`; actual delayed Chat at 1280/1440 plus shared Markdown/copy/layout cases |
| `git diff --check` | PASS | No output |
| D-06/D-07 transport changes, E/F, real provider/runtime execution | NOT RUN | Remain outside this fix round |

The timing regression spans two consecutive render intervals, verifies no early parse before either the elapsed-time gate or RAF, retains both planning events in persisted existing message state, and checks the accumulated answer/thinking text. Completed thinking starts collapsed under the existing UI contract, so the test explicitly expands it before measuring the second interval. A separate hook regression verifies that an unmounted scheduler refuses subsequent background render batches. The renderer regressions test both unfinished and apparently closed ambiguous containers before completion, then verify normal highlighting after completion.

Files changed in this fix round: `web/src/components/markdown/markdown.ts`, `web/src/pages/chat/index.tsx`, `web/src/pages/chat/useStreamRenderScheduler.ts`, the three focused unit test files, and this report appendix. The browser specs, services, SSE transport, routes, dependencies, shared progress ledger, and user truth file were not changed. Full scoped diff reviewed; the intentional conservative container display behavior is the only presentation tradeoff added by this round.
