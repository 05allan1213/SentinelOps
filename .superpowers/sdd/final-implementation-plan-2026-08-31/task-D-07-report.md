# Task D-07 implementation report

Status: PASS — implementation complete; independent review belongs to the coordinator.

Base: `b63d390`. Scope: final plan D-07 only. D-01 through D-06 interfaces are reused. No E/F work, application images, new Runtime routes, client, store, event bus, or backend execution path.

## Implemented behavior

- `chatService.createDurableRun({sessionId,query,agent?})` uses the existing Axios `api.post('/chat/v2/runs')` and verifies the returned session identity. It returns the existing response envelope's `{run_id,session_id,status}` data.
- `tailDurableRun({runId,afterSeq,onEvent,onState,signal,...})` uses only D-06 `streamFetch` GET and `bindSSEVisibility`. D-06 owns parsing, Run/seq dedupe, finite retry budget, visibility pause/resume, reader cancellation and listener cleanup. There is no service or page manual parser or second retry loop.
- Existing `multiAgentChat` positional arguments/callbacks remain. Its optional tenth argument distinguishes intentional `{newTurn:true}` from explicit `{newTurn:false}` resume. Default callers create only when no stored identity exists; stored identity always tails. Explicit resume with no stored identity errors without creating. A deliberate next user turn can create after a previous terminal Run.
- The existing per-session `chat_run_id_*` and `chat_last_seq_*` keys remain. Only a confirmed create response replaces a Run binding/reset cursor. Failed/409 creates leave the old binding/cursor intact. The per-session `chat_run_state_*` snapshot preserves previously observed server facts across DONE-only reloads. An in-flight create is allowed to return after switch/unmount so its accepted identity can be retained, while its aborted consumer never receives callbacks or starts a tail.
- Explicit `multiAgentChatV1` preserves the legacy POST fields and handles C-07 identity metadata before using the same v2 GET tail. It makes zero v2 creates. Its synthetic v1 SSE identity ID is never used as a durable cursor; server `after_seq` is used. Plain-text legacy streams still deliver through their existing callbacks. Server v1 code/envelope is unchanged.
- Run state mapping reads `data.to_status`, including `canceled` and `retryable_failed` on `run.failed`. Retryable failure remains a nonterminal tail. Parked/reconciling stay distinct. `operation.*` has an independent UI field and cannot set Run success. Unsupported explicit statuses remain unknown. Local `onDone`, retry buttons and DONE-only frames do not create a success fact.
- Chat retains its message model/user bubble and D-05 message-ID render scheduler. Narrow optional message fields bind Run identity, Run/operation status and actionable connection errors. Accepted body/planning updates are persisted before seq advancement, independently of the buffered render schedule. Thus a reload does not skip text that was accepted but not yet rendered. Session switches use this accepted snapshot rather than a potentially older React render.
- Reload and session selection resume the matching assistant message through GET. Switch/unmount aborts the browser tail; it does not cancel the backend Run. Explicit retry preserves accumulated body and uses the original Run/cursor. Messages with no recoverable identity show an unconfirmed creation result rather than continuing a phantom spinner.
- The assistant retains its bounded 760 px outer column and block/full-width wrapper; the Markdown renderer now explicitly has `min-w-0 max-w-none`. Existing user bubble structure is unchanged.

## Scope ruling and extra files

The coordinator explicitly approved a narrow `api.ts` opt-out after inspection showed the sole Axios interceptor automatically retried every HTTP 429, including create POSTs. `skipRateLimitRetry` is used only by the durable create request; unrelated Axios retry policy is unchanged. A test runs the actual Axios interceptor and proves one adapter call for HTTP 429.

Additional files beyond the brief's primary list:

- `web/src/services/api.ts`: per-request retry opt-out described above.
- `web/tests/unit/chat-service-api.test.ts`: actual Axios interceptor coverage, separate from the service suite's mocked `api` boundary.
- `web/tests/ui/chat-streaming-markdown.spec.ts`: only the delayed browser fixture boundary changed from legacy v1 to v2 create/tail, with durable IDs/JSON and a canonical terminal event. Existing behavioral assertions were preserved.

No helper module extraction was needed. The already large Chat page remains large; the existing send body was minimally separated into an in-file `startAssistantStream` function so reload/retry share the same callbacks and scheduler. The edit-send snapshot was aligned with the already truncated history while making message persistence explicit.

## TDD evidence

RED: `cd web && npm run test:unit -- chat-service-reconnect` exited 1 with `10 tests | 10 failed` before product implementation. Representative failures:

- Expected one Axios v2 POST, received zero.
- Expected GET `/api/chat/v2/runs/stored/events?after_seq=12`, received POST `/api/chat/v1/chat` with the legacy body.
- Expected canonical Run state callbacks such as `pending/succeeded`, received none.

These failures established the missing v2-first, tail-only continuation and state-mapping behavior. An earlier test-file write used the wrong relative directory, created no file, and produced “No test files found”; it was corrected before the recorded RED run and is not counted as TDD evidence.

GREEN: the first service implementation passed 9/10; the remaining timeout came from a fixture reusing the same already-consumed Response for two requests. Replacing it with a fresh Response per request produced PASS. Service plus existing Chat scheduler/Markdown unit coverage then passed 19/19. Self-review added legacy identity-tail, direct tail state, pending-create abort, failed create binding retention, explicit missing-identity resume, and actual Axios 429 cases. Final full unit result is 242/242 PASS.

## Final verification

| Command / check | Result | Evidence |
| --- | --- | --- |
| `cd web && npm run test:unit` | PASS | 9 files, 242 tests; final output has no test warnings/errors |
| `cd web && npm run lint` | PASS | Exit 0; 0 errors, 68 warnings in existing frontend code; baseline D-06 report also recorded 68 warnings |
| `cd web && npm run build` | PASS | Both TypeScript checks and Vite production build; existing >500 kB bundle warning |
| `cd web && npx playwright test tests/ui/chat-reconnect.spec.ts tests/ui/chat-streaming-markdown.spec.ts tests/ui/markdown.spec.ts --project=chromium` | PASS | 14 controlled Chromium tests: 8 new actual-Chat reconnect tests plus 6 existing streaming/Markdown tests, desktop 1280 and 1440 |
| `go test ./internal/controller/chat -count=1` | PASS | `ok SentinelOps/internal/controller/chat 0.349s` |
| `git diff --check` | PASS | No whitespace errors |
| Real Worker/provider Runtime E2E | NOT RUN | No real Worker/provider was launched or exercised; controlled browser events are transport/UI evidence only |
| Full Go suite / hosted CI / rollout / rollback / production readiness | NOT RUN | No backend product change or readiness claim in this task; only focused chat-controller compatibility tests were run |
| E/F stages | NOT RUN | Outside D-07 boundary |

The controlled browser cases exercise the actual Chat page, actual service adapter and actual shared D-06 reader. Axios create responses are route fixtures; delayed SSE bytes are supplied by controlled browser ReadableStreams. They verify duplicate/out-of-order suppression, one create, GET after-seq retry, retained body through disconnect/reload, next-turn new Run, operation-success separation, canceled/failed/retryable_failed/reconciling/parked states, DONE-only state retention, actionable 403 retry, pending create during session switch, and reader cancellation on switch/unmount. Existing delayed Markdown specs additionally retain DOM stability, fence behavior, scroll-follow control and page overflow assertions at both widths.

## Self-review and limitations

- Reviewed the final service/page diff, callback ordering and server C-07 identity/terminal semantics. Accepted message persistence precedes cursor storage; aborted tail callbacks cannot overwrite a different session. Automatic reconnect and explicit retry never POST.
- A browser reload that destroys an in-flight create before its response returns cannot recover an identity that was never received. The UI reports the creation result as unconfirmed and does not automatically create a replacement. Server-side create idempotency/identity discovery is outside this task.
- Recovery uses the existing tab-scoped sessionStorage keys and local message snapshots; this is not a new cross-tab synchronization protocol.
- Durable v2 accepts the existing server agent configuration. Legacy `messageIndex/deepThinking/webSearch` positional arguments remain compatible; v2 does not introduce undocumented request fields.
- Browser fixture success proves frontend integration and bounded transport behavior, not Worker/provider execution, Effect safety in a real deployment, or operational readiness.
- Coordinator-owned `progress.md` and user-owned `grill-truth.md` remain outside staging. No push.
