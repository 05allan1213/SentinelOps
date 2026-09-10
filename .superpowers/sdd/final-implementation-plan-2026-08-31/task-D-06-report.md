# D-06 implementation report

Status: DONE
Base: `cbc2f5e6f7fc80819e0afec9796ede85ede2e7ba`
Commit subject: `feat(web-sse): add bounded cursor-aware reconnect`

## Implemented

- Extended the existing shared fetch SSE parser; no second HTTP client, EventSource, event bus, QueryClient, or state store.
- Added per-Run monotonic high-water cursors, GET `after_seq` resume, duplicate/regressed frame suppression, finite reconnect delays of 250/500/1000/2000/4000 ms, and explicit cancellation.
- Durable GET validates a positive decimal safe-integer event ID and a JSON object envelope before accepting the cursor. Initial cursor accepts nonnegative safe integers. Malformed frames stop with a typed protocol error; previously delivered content and accepted cursor remain intact.
- Handles byte-split UTF-8, CRLF/CR/LF, multiline data, comments, optional space after field colon, and final unterminated frames. On network reader failure, only the incomplete unaccepted frame is discarded before cursor-based recovery.
- GET network/5xx/unterminated nonterminal closure reconnects up to the configured limit, capped at five. HTTP 4xx/auth, protocol error, consumer callback failure, and abort do not reconnect. POST commands are never replayed.
- Reader cancellation and lock release run on completion, terminal, protocol failure, and abort. Retry timers and external abort listeners are cleaned up.
- `run.completed`, `run.parked`, and nonretryable `run.failed` close the tail after delivering the event. Retryable `run.failed` (`payload.data.retryable === true`), `run.reconciling`, and `operation.*` continue. `[DONE]`/`done` signal transport completion only, including a DONE-only reconnect at a previously terminal boundary. No Run status is invented.
- Added React transport hook with one visibility binding, latest event callbacks without reconnect-on-render, explicit manual retry, separate transport error/done state, and cleanup on dependency changes/unmount. Event bodies remain owned by the consumer.

## Interfaces for D-07

Existing positional form remains available:

```ts
streamFetch(url, body, onChunk, onDone, onError?, signal?)
```

New options form (use an explicit `method` for new callers):

```ts
streamFetch(url, {
  method?: 'GET' | 'POST', // POST by default
  body?: unknown,
  signal?: AbortSignal,
  runId?: string, // defaults to URL; durable callers should supply the Run ID
  initialAfterSeq?: number, // default 0
  maxRetries?: number, // default 5, bounded to 0..5
  onRetry?: (attempt: number, delayMs: number) => void,
  cursor?: SSECursor,
  paused?: boolean,
}, onChunk, onDone, onError?)
```

`onChunk(type, content, id?)` retains the string payload callback. GET validates JSON but returns its complete original string to the consumer. Legacy POST permits plain text and large opaque ID strings, retains silent positional-signal abort behavior, and never reconnects. The options overload is distinguished by the documented option keys; existing Chat positional request bodies use `query`, `session_id`, `agent`, and `last_seq` and remain compatible. An explicit sixth positional signal selects the legacy form.

Both forms return:

```ts
{
  finished: Promise<void>, // settles after cleanup; failures go to onError
  cursor: SSECursor,
  abort(): void,
  setPaused(paused: boolean): void,
}
```

`SSEError` extends `Error`, exposing `code` (`aborted|http|network|protocol|retries_exhausted|consumer`) and optional HTTP `status`. Consumer callbacks should not throw from `onError`.

`SSECursor` exposes `lastSeq`, `accept(runId,seq)`, `reset(runId,seq=0)`, and `seen(runId,seq)`. `lastSeq` refers to the most recently selected Run. `accept` never decreases that Run's stored cursor; `reset` is an explicit consumer reset boundary. `seen` covers IDs at/below the high-water mark, including recovered history. Sharing a cursor across successive tails retains dedupe history; do not attach multiple simultaneous tails to one consumer.

Visibility contract:

- `setPaused(true)` holds initial requests and pending reconnect timers. An already live connection continues receiving events; hiding/showing a healthy tail does not create a redundant request.
- Showing a tab with pending recovery resumes once immediately. It preserves the existing reconnect budget rather than starting a second loop.
- Service adapters can call `bindSSEVisibility(control)` once. It installs one listener, synchronizes current visibility, returns idempotent cleanup, and automatically removes the listener when `finished` settles. Caller cleanup should invoke the returned unbind and `control.abort()`.
- React callers can instead use `useSSECursor({url, runId, initialAfterSeq?, maxRetries?, enabled?, signal?, onChunk, onDone?, onError?, onRetry?})`, returning `{cursor,error,done,retry}`. The hook already binds visibility; do not bind it again in a wrapping service/page. `retry()` is an explicit user retry with the retained cursor and a new finite retry budget. It never clears previously delivered body content.
- `done` is transport completion, not `succeeded`. D-07 must keep server Run state separately and handle DONE-only boundary without inferring success.

## TDD evidence

RED: Before product implementation, `cd web && npm run test:unit -- sse` exited 1: `tests/unit/sse.test.ts (27 tests | 27 failed)` and the hook suite could not resolve `@/hooks/useSSECursor`. Representative expected failures: `SSECursor is not a constructor`, `Cannot read properties of undefined (reading 'finished')`, and no retry callback. These demonstrate the baseline lacked the new cursor/control/hook interfaces and retry behavior. An earlier test-file write command used an incorrect relative working directory; it created no test files and returned “No test files found.” That setup mistake was corrected before the recorded RED run.

GREEN: Implemented parser/hook, then `npm run test:unit -- sse` passed the initial 30 tests. Expanded self-review coverage for reader network failure, invalid initial cursor, callback failure, legacy abort, active-tail visibility, Run switching, and abort during buffered delivery. Final focused result: **37/37 PASS** in 2 files.

## Final verification

| Command | Result | Evidence |
| --- | --- | --- |
| `cd web && npm run test:unit -- sse` | PASS | 2 files, 37 tests |
| `cd web && npx eslint src/utils/sse.ts src/hooks/useSSECursor.ts tests/unit/sse.test.ts tests/unit/sse-cursor.test.ts` | PASS | No warnings/errors |
| `cd web && npm run test:unit` | PASS | 7 files, 224 tests |
| `cd web && npm run lint` | PASS | Exit 0; 0 errors, 68 warnings in other existing files |
| `cd web && npm run build` | PASS | TypeScript checks and Vite production build; existing >500 kB chunk warning |
| `go test ./internal/controller/chat -count=1` | PASS | `ok SentinelOps/internal/controller/chat 0.356s`; includes server terminal boundary/retryable/reconnect tests |
| `git diff --check` | PASS | No whitespace errors |
| Provider/Worker/hardware/live-browser E2E | NOT RUN | Not evidence supplied by deterministic transport tests |
| D-07 Chat v2 integration, E/F phases | NOT RUN | Outside D-06 boundary |

The coordinator's prior DSN-backed prerequisite PASS is not recharacterized here as new D-06 integration evidence. This task ran the requested controller command and frontend checks.

## Files changed

- `web/src/utils/sse.ts`
- `web/src/hooks/useSSECursor.ts`
- `web/tests/unit/sse.test.ts`
- `web/tests/unit/sse-cursor.test.ts`
- This report.

## Self-review and boundaries

Fixed an abort edge case found during self-review: if an event callback aborts while further frames are already buffered, dispatch now checks cancellation before delivering those frames. The added regression test confirms one delivery and retained cursor 1.

Reviewed the final diff against server `isDurableStreamStopEvent` and its DONE-only reconnect behavior. Existing Chat service/parser migration remains D-07; no Chat service or page changes were made. The coordinator-owned `progress.md` modification and untracked `grill-truth.md` were preserved and excluded from staging. No push, no E/F work, no provider/production readiness claims.
