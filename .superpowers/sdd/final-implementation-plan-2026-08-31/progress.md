# SDD ledger — plan: output/final-implementation-plan-2026-08-31.md

## Scope and preflight

- Active user scope: execute H-01, H-02, H-03 only; stop before D.
- `grill-truth.md` and `output/final-implementation-plan-2026-08-31.md` were read from the current checkout.
- B1 prerequisite check: progress artifact records PASS for B1-01..B1-06 and current HEAD is the B1-06 commit; independent source/test verification is pending before H implementation.
- H-03 rule: if no factual Agent Eval/Release source exists, implement explicit `unavailable`/`not_run` read models and continue; this is not a blocker.
- Product branch is `main`; user explicitly authorized implementation in `/home/monody/project/SentinelOps`. No push and no application image work.

## Preflight conflict/consistency scan

| Pair / task | Shared output or interface | Finding and ruling |
| --- | --- | --- |
| H-01 ↔ H-02 | `internal/controller/runtime/runtime.go`, `IRuntimeV1`, Runtime DTO metadata | H-01 adds capabilities/safety methods; H-02 adds worker-health/retention methods. They share one controller but have disjoint methods. Sequential execution prevents edit conflicts. **Ruling: implement H-01 first, preserve one controller/service and frozen DTOs.** |
| H-02 ↔ H-03 | `internal/controller/runtime/runtime.go`, `RuntimeService`, Worker snapshots | H-03 consumes H-02 health/snapshot facts and owns Eval/Release only. **Ruling: H-03 follows H-02 and must degrade missing facts to `unavailable` + `not_run`.** |
| H-01 ↔ B1-06 | frozen routes and controller methods | B1-06 currently exposes placeholder H methods. H-01 replaces only those implementations and keeps route paths/envelope unchanged. **Ruling: no new route namespace or client.** |
| H-02 ↔ C-05 | `runtime_worker_snapshots` projection | C-05 is an existing prerequisite; health queries must use persisted rows only. **Ruling: never infer health from API process state.** |
| H-03 ↔ B1-05 | Trace/Eval quality | H-03 may reuse factual RAG/Trace data but cannot label it Agent Eval or release proof. **Ruling: distinguish `rag_eval`; absent versioned Agent Eval remains `not_run`.** |
| H-01 own task | capabilities/safety tests vs service/controller files | Tests must exercise strict catalog, configured/observed split, secret redaction, shadow effective gates, and read-only boundary. **Ruling: no mutable endpoint or raw credential/handle field.** |
| H-02 own task | health/retention DAO/service files | Missing/malformed observations are partial/unavailable, not zero-valued healthy success; retention uses validated existing policy. **Ruling: preserve nullable aggregate metadata.** |
| H-03 own task | eval/release read model | Gray/rollback require durable fact; otherwise nullable unavailable/not_observed and independent not_run. **Ruling: do not invent a new truth table or dashboard.** |

## Task status

- H-01: complete (review clean; deferred test-evidence minors)
- H-02: complete (depends on H-01; review clean after 2 fix rounds; deferred shared-DB test-isolation minor)
- H-03: pending (depends on H-02; optional source degradation permitted)

## H-01 review loop

- Initial implementation commit: `7d883b47a431b15c6a08c338714b9043302dfb8b`.
- Task reviewer verdict: Needs fixes. Important findings: populate `AllowedAgents`; make MCP/Skill configured state gate/frozen-snapshot aware; do not hardcode Gate audit availability; propagate or explicitly degrade worker-snapshot persistence errors; preserve source identity when joining observations. Minor findings: make Skill validation failure explicit and strengthen positive observation/secret serialization tests.
- Fix round 1 commit: `8df97a02bab5f2c224614a77ea83562625b0ff28`; scoped re-review pending.
- Fix round 1 re-review: 4 prior findings addressed; frozen-Skill gate fallback remained open, and metadata precedence/unmarshal handling introduced an Important diagnostic regression. Fix round 2 required.
- Fix round 2 commit: `42f304b8b3ee463ba75f75a3666c0bde6a9e206b`; scoped re-review pending.
- Fix round 2 re-review: metadata precedence and malformed observation handling addressed; evaluator-missing frozen-Gate fallback remains Important. Test-evidence minors remain noted.
- Fix round 3 commit: `359839ce20f9a8edc760be297b117aeb3332430f`; evaluator-missing fallback removed; scoped re-review pending.
- Fix round 3 re-review: PASS; evaluator-missing MCP/Skill states are fail-closed and no new Critical/Important issue. Minor test-evidence gaps deferred for final review.
- H-01: complete (commits `3e27b9bc6027ad08c6498bfb0c6ab8f9ef96f9b3`..`359839ce20f9a8edc760be297b117aeb3332430f`, review clean with deferred minors).
- H-02: implementation commit `d353405f08844c0f659490d6311b2ae060286a33`; task review pending.
- H-02 fix round 1: `bbcbfe53ab1c8bdc9ffaa57fba51a4c4632e0fbc`; cleanup and missing-component metadata corrected, behavior tests added; scoped review found service-path and malformed-identity evidence gaps.
- H-02 fix round 2: `38883d97be6108a6bad97a0a177d354deaaaa01e`; malformed empty `worker_id` is unavailable and disposable-MySQL `RuntimeService.GetWorkerHealth` projection test added; scoped review PASS. Deferred minor: fixture shares phase03 DB and assumes exactly three rows.
- H-02: complete (commits `d353405f08844c0f659490d6311b2ae060286a33`..`38883d97be6108a6bad97a0a177d354deaaaa01e`, review clean with deferred minor).
- H-03: implementation complete locally; `report-H-03.md` records the source audit, `agent_eval` absence/not-run behavior, explicit `rag_eval` reuse, and persisted Release facts. Task review is pending; D/E/F remain untouched.
