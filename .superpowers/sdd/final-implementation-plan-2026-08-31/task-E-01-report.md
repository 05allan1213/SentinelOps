# Task E-01 implementation report

Status: **PASS**. Typed Runtime service, DTO mirror and the frozen query-key surface are implemented with the
sole Axios client and the sole QueryClient. No page, route, Go, dependency or F work.

Base: `c65271e` (E-00). Files: `web/src/types/runtime.ts`, `web/src/services/runtime.ts`,
`web/src/hooks/useRuntimeQueries.ts`, `web/tests/unit/runtime-service.test.ts`,
`web/tests/unit/runtime-query-keys.test.ts`.

## Implementation

- `types/runtime.ts` mirrors `api/runtime/v1/runtime.go` exactly (JSON tags, snake_case, enum unions,
  `omitempty`/pointer fields as `?: T | null`). All wrappers extend `ResourceMeta`, and collection `items`
  / `page` are optional so an absent source stays absent instead of becoming an empty success.
- `services/runtime.ts` exposes the frozen method names on `/runtime/v1/...` through the existing `api`
  instance, unwraps `{message,data}` like sibling services, forwards `AbortSignal`, and performs exactly one
  mutation (`recoverRun`) with no optimistic update. `normalizeRuntimeParams` is deterministic (sorted keys,
  drops `undefined`/`null`/`''`/empty arrays, keeps explicit `false` and `0`).
- `hooks/useRuntimeQueries.ts` defines `runtimeQueryKeys` with all 18 frozen keys, every key embedding the full
  normalized parameter set or resource id, plus the 18 frozen hooks. Hooks only add `enabled`/`refetchInterval`;
  no server-state copy, no second client, no Zustand slice.

## TDD evidence

1. RED (missing modules): `npm run test:unit -- runtime-service runtime-query-keys` exited 1 with
   `Failed to resolve import "@/services/runtime"` and `"@/hooks/useRuntimeQueries"`, 2 files failed, no tests.
2. RED (own test defects, fixed before GREEN): the query-key spec was authored as `.ts` with JSX, which the
   frozen filename forbids; rewritten with `createElement`. The empty-id case also needed an explicit
   `beforeEach` mock reset so call counts did not leak across tests.
3. GREEN: `npm run test:unit -- runtime-service runtime-query-keys` -> 2 files / 13 tests PASS.

## Final verification

| Command | Result | Evidence |
| --- | --- | --- |
| `npm run test:unit` | PASS | 12 files / 260 tests (247 baseline + 13 new) |
| `npm run lint` | PASS | exit 0; 0 errors, 68 pre-existing warnings; new files add none |
| `npx eslint` on the five new files | PASS | no output |
| `npm run build` | PASS | TypeScript + Vite build; only the existing >500 kB chunk warning |
| `git diff --check` | PASS | no whitespace errors |

## Boundaries and deferred items

- Availability/quality/reason_code/not_run and nullable budget counters survive parsing unchanged (asserted).
- Recovery mutation sends the exact frozen body and returns the server envelope's `operation`; nothing is
  written to cache optimistically.
- Real Runtime/Worker/provider execution is `NOT RUN`; no fixture is presented as real execution evidence.
- Deferred: E-02 consumes `runtimeQueryKeys.events` for the SSE tail; E-03+ pages are not implemented here.
