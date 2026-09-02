# H-01 Implementation Report

Status: DONE_WITH_CONCERNS

## Scope

Implemented only H-01 Capabilities/Safety read-only service and controller wiring. B1 was confirmed complete at baseline `3e27b9bc6027ad08c6498bfb0c6ab8f9ef96f9b3` (parent reported focused B1 verification PASS).

## Changes

- Added `RuntimeService.GetCapabilities` and `GetSafety`.
- Exposed strict policy Catalog local and AgentTool entries with risk, revision, schema hash, effect and gate metadata.
- Added configured MCP server and validated Skill snapshot entries without serializing URLs, headers, SecretRefs, sessions or handles.
- Joined MCP/Skill observed state only from persisted `runtime_worker_snapshots`; missing observations are `availability=unavailable`, `reason_code=not_observed` and `observed_worker_state=unknown`.
- Added one-shot GateEvaluator static/dynamic/effective safety projection; shadow mode closes effective L1/L2 writes and policy/catalog identity is exposed.
- Wired controller methods while retaining nil-service unavailable behavior and existing `{message,data}` routes; bootstrap injects validated app config.
- No write endpoints, toggles, stores, routes or alternate runtime paths added.

## Verification

PASS — `go test ./internal/service/runtime ./internal/controller/runtime ./internal/bootstrap`

PASS — `go test ./internal/service/runtime ./internal/controller/runtime -run 'Test(Capabilit|Safety)'`

PASS — `go test ./internal/ai/policy ./internal/ai/tools/mcp`

PASS — `SENTINELOPS_TEST_DSN='root:sentinel_test_pw@tcp(127.0.0.1:13307)/sentinelops_phase03?parseTime=true&multiStatements=true' go test ./internal/ai/runtime -run 'TestWorkerSnapshotPersistsHeartbeatWithDisposableDSN' -count=1`

NOT RUN — full `internal/ai/runtime` suite has additional integration tests requiring the disposable DSN; the targeted worker snapshot test passed with the disposable MySQL configuration above.

## Self-review / concerns

- MCP capability rows represent configured servers (the frozen DTO has no dedicated server/transport fields); allowed/discovered tool details remain intentionally absent from the public DTO.
- When a frozen runtime snapshot is injected, local/AgentTool revision and schema values are read from its immutable ToolSnapshot projection; otherwise the strict Catalog is used.
- Skill filesystem validation errors are fail-closed by omission of invalid entries; no unvalidated Skill is reported loaded.
- Safety policy hash is a deterministic hash of the strict server Catalog; no mutable configuration or secret material is included.

Commit: this implementation commit (verify with `git rev-parse HEAD`).

## Fix round 1

- Added authoritative `policy.DurableToolAgents` inventory inversion for `allowed_agents`.
- MCP/Skill configured state now requires current effective Gate (fail-closed when evaluator is absent); frozen Snapshot tool/Skill identities are reused.
- Gate audit availability is injected from the existing persisted audit reader; absent source returns unavailable/not_observed/not_run instead of fabricated `true`.
- Worker observation query errors now downgrade response metadata to unavailable; MCP and Skill observations join through source-qualified keys to prevent same-name cross-wiring.
- Skill validation failures produce explicit `skill_validation_unavailable` metadata. Added focused tests for strict inventory, configured/observed separation, secret redaction, read-only behavior and shadow effective writes.

Fix verification: `go test ./internal/service/runtime ./internal/controller/runtime ./internal/bootstrap ./internal/ai/policy ./internal/ai/tools/mcp` — PASS; disposable snapshot test with `SENTINELOPS_TEST_DSN` on `127.0.0.1:13307` — PASS.

Observed command output:

```text
ok   SentinelOps/internal/service/runtime
ok   SentinelOps/internal/controller/runtime
ok   SentinelOps/internal/bootstrap
ok   SentinelOps/internal/ai/policy
ok   SentinelOps/internal/ai/tools/mcp
ok   SentinelOps/internal/ai/runtime
```

## Fix round 2

- Frozen Skill snapshot fallback now derives configured state from effective/frozen Skill Gate and never reports `enabled` when the evaluator is absent or the gate is closed.
- Capability response metadata uses deterministic precedence: worker snapshot errors (including malformed JSON) then Skill validation errors, preserving reason codes and `not_run`; generic `not_observed` is only used when no stronger error exists.
- Corrupt persisted observation JSON returns an explicit malformed snapshot error instead of being treated as missing evidence.

Verification: focused service/controller/bootstrap/policy/MCP tests PASS; disposable Worker snapshot test with `SENTINELOPS_TEST_DSN` on `127.0.0.1:13307` PASS. Positive persisted observation and secret-redaction tests remain covered by existing runtime snapshot/security suites.

## Fix round 3

Removed the evaluator-missing frozen-gate fallback. MCP and Skill configured states now remain fail-closed (`disabled`) whenever no current GateEvaluator is available, even if a frozen snapshot contains enabled gates. Production bootstrap continues to inject the evaluator.

Verification: focused service/controller/bootstrap/policy/MCP tests PASS; disposable Worker snapshot test with `SENTINELOPS_TEST_DSN` and `127.0.0.1:13307` PASS.
