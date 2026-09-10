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
- H-03: implementation commit `ccb142a093f20dd2e2c2b8dda17fbf457d0325c7`; `report-H-03.md` records the source audit, `agent_eval` absence/not-run behavior, explicit `rag_eval` reuse, and persisted Release facts.
- H-03 scoped review: Needs fixes. Empty/invalid RAG aggregates could look available without `not_run`; explicit RAG authorization errors were swallowed as unavailable; Worker-version service-path test wrote to the shared phase03 database; report referenced a nonexistent Trace test name.
- H-03 fix round 1: `46e86d6`; malformed/empty RAG source is unavailable or partial with `not_run=true`, authorization errors map to Runtime forbidden, Worker-version evidence uses an isolated disposable database, and the report command is corrected. Scoped re-review PASS; D/E/F remain untouched.
- H-03: complete (commits `ccb142a093f20dd2e2c2b8dda17fbf457d0325c7`..`0206a592d4c1680ef2fbf48d67ef84d812a3fe62`, review clean after 1 fix round).

## H whole-branch review and closeout

- Reviewed the complete H branch from B1-06 baseline `3e27b9bc6027ad08c6498bfb0c6ab8f9ef96f9b3` through `0206a592d4c1680ef2fbf48d67ef84d812a3fe62`. Changes are limited to H Runtime service/controller/DAO/bootstrap/policy catalog code, focused tests, and H reports/ledger; no D/E/F product code or tests were touched.
- H-01 deferred minors: positive persisted MCP/Skill observation, source-collision, and exhaustive serialized-secret assertions remain lower-priority evidence gaps; scoped review found no Critical/Important defect.
- H-02 deferred minor: one disposable-DSN fixture historically assumed an empty/shared phase03 snapshot table; service-path behavior and malformed identity are now covered, and no Critical/Important defect remains.
- Final verification: DSN-backed `go test -p 1 ./internal/service/runtime ./internal/controller/runtime ./internal/bootstrap ./internal/dao/mysql ./internal/ai/policy ./internal/ai/runtime ./internal/service/rageval` PASS (the long `internal/ai/runtime` package completed in 150.887s); `go test -p 1 ./internal/service/rageval -count=1` PASS; `go test -race ./internal/service/runtime ./internal/controller/runtime` PASS; `go vet ./internal/service/runtime ./internal/controller/runtime ./internal/bootstrap ./internal/dao/mysql ./internal/ai/policy ./internal/ai/runtime ./internal/service/rageval` PASS; `git diff --check` PASS.
- H-03 optional Release persisted-worker integration test PASS with isolated disposable MySQL database. Real provider, versioned Agent Eval, rollout/rollback, hosted CI, images, P43, and all later D/E/F phases remain NOT RUN.
- Post-closeout documentation cleanup: `29ffb26` replaced literal disposable DSN credentials in H-01/H-02 reports with non-sensitive placeholders; `19459ce` synchronized this ledger. No product behavior changed; final HEAD is `19459ce`.

## B1-02 contract fix (post-H review)

- Scope: only the review-identified B1-01/B1-02 contract gaps; no Route/IRuntimeV1 growth, no migration, no frontend/D/E/F changes. `grill-truth.md` remains untracked and is excluded from commits.
- Commit 1 (fingerprint): `f1ce886` separates deterministic AttemptFingerprint from the persisted executing-worker fingerprint, validates Worker snapshot hashes at Claim/Recovery first claim inside the row lock, and persists the fingerprint on new Attempt rows.
- Commit 2 (context): `8542022` adds bounded redacted `include=history` Context expansion and routes Context reads through `RuntimeService.GetContext`.
- Commit 3 (budget/agent): `cd66487` keeps invalid Budget counters nil and degrades missing/malformed agent projections on `/runs` rows.
- Commit 4 (tools/docs): `34e0485` removes staticcheck dead helpers, corrects the controller-test assertion, refreshes the frozen-plan/ContextDTO contract, and adds the B1 fix review file. No push.
- Focused verification: new deterministic fingerprint tests PASS; Claim and Recovery fingerprint persistence/mismatch DB tests PASS; real durable Run Context `HistoryCount=2` DB test PASS; `go test ./api/runtime/... ./internal/service/runtime ./internal/controller/runtime ./internal/dao/mysql` PASS.
- Final tooling: `SENTINELOPS_TEST_DSN=... go test ./internal/ai/workflow -run 'Test(Attempt|C06)' -count=1 -timeout 30m` PASS (53.407s); `internal/ai/runtime` PASS (238.738s); `go test -race ./internal/service/runtime ./internal/controller/runtime`, `go vet ./...`, target `staticcheck`, and `git diff --check` PASS.
- Final full run with `-timeout 30m`: `SENTINELOPS_TEST_DSN=... go test -p 1 ./api/runtime/... ./internal/service/runtime ./internal/controller/runtime ./internal/dao/mysql ./internal/ai/workflow ./internal/ai/runtime -count=1 -timeout 30m` PASS; workflow 787.037s, ai/runtime 61.064s, dao/mysql 41.276s.
- Final review verdict: PASS, no Critical/Important finding. Earlier 10-minute default-timeout run is superseded by the successful 30-minute serial run and is recorded only as an environment note.
- Remaining gates: D/E/F, Hosted CI, real providers/API/Worker execution, rollout/rollback, image contracts, and P43 remain NOT RUN.


## Phase D session 2026-09-08
Scope: D-01 through D-07 only; stop before E. No push/implicit merge.
Base c0b819158fad3fbe02132050d47c9261b65d1a4a. B1 six tasks PASS; H three tasks complete per original ledger and source; final commits verified HEAD ancestors. Latest B1 fixes and serial 30m PASS present.
Original untracked grill-truth.md preserved. Ignored plan copied into isolated worktree.
Ruling: reversible worktree creation authorized by task/developer autonomy; use existing sibling .worktrees.
Ruling: scripts are not executable, task-brief recognizes numeric headings only; use bash and exact Python extraction for D headings.
Ruling: planning-only constraint applies to authoring, not implementation.
| Tasks | Interface | Audit/ruling |
|---|---|---|
| D-01 / all D | runner/setup | reviewed first |
| D-01 / D-03 | package/lock | sequential exact versions |
| D-02 / D-03 | helpers/CodeBlock | renderer owns restricted highlighting |
| D-02 / D-05 | fences | preserve interface, matching markers |
| D-03 / D-04 | variants | one parser import |
| D-03 / D-05 | renderer lifecycle | pure display, caller stream state |
| D-04 / D-05 / D-07 | Chat page | sequential ID order |
| D-06 / D-07 | parser/cursor | GET retry cannot create Run |
| D-01 own | red-green smoke | remove temporary failure |
| D-02 own | names vs aliases | test all canonical names/common aliases |
| D-03 own | lowlight/rehype | verify restricted registration |
| D-04 own | 8 calls/16 tags | migrate actual calls |
| D-05 own | throttle/terminal | message-ID scoped, immediate completion |
| D-06 own | retry/visibility | no command retry, monotonic GET |
| D-07 own | new turn vs resume | preserve explicit new turns, no silent replacement |
Task D-01: in progress; BASE c0b819158fad3fbe02132050d47c9261b65d1a4a
Tasks D-02 through D-07: pending

- Prerequisite live regression PASS: disposable MySQL started from existing stopped container; DSN injected only into subprocess. go test -p 1 ./api/runtime/... ./internal/service/runtime ./internal/controller/runtime ./internal/controller/chat -count=1 -timeout=30m; all packages pass. Log prerequisite-tests.log.
- Node 24.19.0, npx, Chromium caches verified available.

Task D-01: implementation 968d2c542ecddda7af69bae68d45781a6dbcffb3; review pending. Unit 1 PASS; clean npm ci PASS; lint 68 baseline warnings/0 errors; build PASS baseline chunk warning. Old Playwright smoke FAIL 2/3, existing missing thinking header (F-02), no D-01 product changes.

Task D-01: fix round 1/5 started. Important: new lock entries coupled fresh checkout to undeclared npmmirror registry. Repair registry-neutral resolved metadata preserving exact versions/integrities; npm ci + unit gate. Reviewer cannot verify no-push from diff; coordinator confirms no push tool/command issued. Baseline lint/build/smoke observations deferred to respective scope, no D-01 regression.

Task D-01: fix round 1/5 (1 addressed, 0 open; 968d2c5..14e2022).
Task D-01: complete (commits c0b8191..14e2022, review clean).
Task D-02: in progress; BASE 14e2022b661984024344e86ca160310e8050a14a.

## Resume 2026-09-09
- User requested continue. Verified HEAD 14e2022, no partial D-02 product edits. D-02 original implementer resumed after usage-limit failure; D-01 not repeated. Main remains c0b8191 with original untracked grill-truth.md only.

Task D-02: implementation eb232a5b2c194683d6e088086ada9c7768ebb4d6; review pending. Focused 103/full 104 unit PASS, touched ESLint clean, lint/build PASS baseline warnings unchanged. Browser gate scheduled with D-03.

Task D-02: fix round 1/5 started (backslash URL classification and CR-only fence detection). Downstream review checks: D-03 must skip highlighting before processing oversized/streaming code; D-03 owns inline/fenced/browser integration, D-05 owns caller streaming. Baseline warnings remain same as D-01 report.

Task D-02: fix round 1/5 (2 addressed, 0 open; eb232a5..a6b5ddd).
Task D-02: complete (commits 14e2022..a6b5ddd, review clean). Re-review used exact base/head fallback after reporting package unavailable; coordinator package exists, same range inspected.
Task D-03: in progress; BASE a6b5ddd19bf1d69b0575dcb5149b3ad2190bbf0e.

Ruling: D-03 may add exact lowlight alias + core adapter/config changes — rehype-highlight imports lowlight.common and public lowlight barrel reexports common/all, so runtime language options alone do not satisfy restricted bundle requirement — cost if wrong: package-internal pinned core path requires revalidation on upgrade; keep Vite/Vitest resolution identical and test actual bundle.

- User resumed D-03 after second usage-limit interruption; partial edits preserved and implementer resumed without repeating D-01/D-02.
Task D-03: implementation f8f61ac; review pending. Unit focused 32/full 144 PASS; browser 3 PASS at 1280/1440; exact 12 grammar implementations/no common-all bundle verified; lint/build PASS baseline warnings.

Task D-03: fix round 1/5 started. Important exact-copy edge: indented/quoted literal fence-like EOF line incorrectly treated as closer, adding absent newline. Reviewer reproduced via real remark pipeline. Implementer owns scoped fix/tests.

Task D-03: fix round 1/5 (1 addressed, 0 open; f8f61ac..ba71dea).
Task D-03: complete (commits a6b5ddd..ba71dea, review clean). Focused 37 unit + 1 new clipboard browser test PASS after fix; earlier 144 full unit + 3 browser PASS recorded.
Task D-04: in progress; BASE ba71dea.

Task D-04: implementation 70c2de7; review pending. Full unit 166 PASS; Markdown 165 PASS; all 8 calls migrated; parser imports only shared renderer; normalizeMarkdown identity preserves CR/LF bytes; lint/build PASS 68 baseline warnings. Automatic design hook surfaced 2 pre-existing gray-on-color Chat cases, unchanged/out of D-04.

Task D-04: minor (deferred to D-05 touched test): streaming-prop source regex spans multiple JSX tags, allowing false positive; scope to individual tag.
Task D-04: complete (commits ba71dea..70c2de7, review approved, one test-precision minor).
Task D-05: in progress; BASE 70c2de7.

- D-05 resumed after usage-limit interruption; verified no partial edits before resuming original implementer.
Task D-05: implementation 3c9db3e; review pending. Full unit 181 PASS; 6 browser cases PASS including delayed actual Chat at 1280/1440; lint/build PASS with unchanged baseline warnings. D-04 cross-tag assertion minor fixed in this task; transport untouched.

Task D-05: fix round 1/5 started (nested list/quote open fences prematurely highlighted; nonterminal planning finish() bypasses render budget).
Ruling: ambiguous container-fence syntax may conservatively remain escaped raw text until completion — truth requires stable unclosed fences and no per-token full parse — cost if wrong: temporary highlighting may be delayed for ambiguous valid Markdown, final rendering remains complete.

Task D-05: fix round 1/5 (2 addressed, 0 open; 3c9db3e..cbc2f5e). Fix resumed after usage-limit interruption with existing tests preserved.
Task D-05: complete (commits 70c2de7..cbc2f5e, review clean). Full 187 unit + 6 browser PASS; D-04 assertion minor resolved.
Task D-06: in progress; BASE cbc2f5e.

Task D-06: implementation 29d2011; review pending. SSE focused 37/full frontend 224 unit PASS, touched lint clean, full lint/build PASS baseline warnings, Go Chat controller SSE tests PASS. Adds compatible control return and shared visibility binder for D-07; transport completion never creates server success.

- D-06 review resumed after usage-limit interruption; implementation unchanged.
Task D-06: fix round 1/5 started. Important: cursor advanced before successful consumer callback, causing explicit retry to skip undelivered event. Fix commits cursor after successful callback, keeps duplicate check before dispatch and success-then-abort cursor semantics.

Task D-06: fix round 1/5 (1 addressed, 0 open; 29d2011..b63d390).
Task D-06: complete (commits cbc2f5e..b63d390, review clean). SSE 38 PASS after fix; prior full frontend 224/Go controller PASS recorded.
Task D-07: in progress; BASE b63d390.

Ruling: D-07 may add a per-request 429 retry opt-out to existing api.ts for v2 creation — current global interceptor automatically repeats POST, conflicting with one-create semantics — cost if wrong: a rate-limited create needs explicit user retry; unrelated request behavior remains unchanged.
Ruling: optional trailing multiAgentChat options may distinguish deliberate newTurn from stored-Run resume while retaining existing positional callback signatures — otherwise subsequent user messages remain trapped on prior terminal Run — cost if wrong: extra compatibility option needs coverage; failed create must retain prior recoverable binding.

- D-07 resumed after usage-limit interruption; partial service/page/tests preserved, legacy identity-to-tail and final checks completed.
Task D-07: implementation 1f7acf1; review pending. Full frontend 242 unit PASS; 14 controlled browser PASS; lint/build PASS baseline warnings; Go Chat controller PASS. Explicit missing-identity resume cannot create; lost create acknowledgment remains unconfirmed. Real Worker/provider E2E NOT RUN.

Task D-07: fix round 1/5 started, 4 Important findings: real run.claimed/approval.requested envelopes not mapped; switch-back before pending create reply strands accepted Run; unconfirmed creation lost on switch/prior-turn binding; upload notice can overwrite accepted snapshot with stale rendered state while cursor remains advanced. Fixes require real-envelope and lifecycle/persistence ordering regressions.

Task D-07: fix round 1 implementation 562a6c9; re-review pending. 245 unit and 22 desktop browser tests PASS; lint/build PASS baseline warnings. Fix resumed from preserved partial edits after usage limit. Transient pending-create promises and per-message accepted binding/uncertainty close the reported lifecycle gaps; upload uses accepted snapshot.

Task D-07: fix round 1/5 (4 addressed, 0 Important open; 1f7acf1..562a6c9).
Task D-07: minor (deferred to final review): legacy saved pending assistant with isStreaming=true and no Run identity/createUnconfirmed field loses uncertainty on restoration; convert old signal to new message marker once.
Task D-07: complete (commits b63d390..562a6c9, review approved with one legacy-compatibility minor).

## D whole-branch closeout
- All D-01 through D-07 task gates complete. E/F NOT RUN. Final whole-branch review and final verification pending.
- Review deferred items: D-04 source-regex minor resolved by D-05; D-07 legacy pending-snapshot migration minor remains for final triage. Lint 68 warnings/large bundle warning are baseline, old smoke 2/3 baseline failure belongs F-02. H historical minors are prior scope, not D work.
- Final serial frontend verification: `npm run test:unit` 9 files / 245 PASS; `npm run lint` exit 0, 0 errors / 68 baseline warnings; `npm run build` PASS with existing chunk warning; controlled desktop Playwright 22/22 PASS after an earlier parallel-build timeout and exact-case rerun.
- Final bundle/dependency verification: exact D pins and registry-neutral lockfile PASS; actual production bundle contains only 12 allowlisted highlight.js grammar implementations (HTML/XML shared), core + rehype-highlight present, no lowlight common/all registries.
- Backend compatibility: `go test ./internal/controller/chat -count=1` PASS. DSN-backed prerequisite Runtime/Controller/Chat tests PASS. No api/internal/manifest migration or Runtime page file changed by D.
- First parallel browser run had 1 timeout at 1280 `accepted create after session switch...` while a Vite bundle build ran concurrently; exact failing case rerun alone PASS (6.6s), then the full 22-case suite rerun serially PASS (55.7s). Classified as test-environment contention, not product regression.
- Final cross-task scope checks: no `api/`, `internal/`, migration, manifest, or Runtime page file changed by D; no rehype-raw, second EventSource/client, debug logging or secret literals in D frontend paths. D-07 429 opt-out affects only the durable create request.
- Final review adjudication: repeated reviewer-subagent dispatch attempts returned without executing; coordinator completed the whole-range diff/scope/test review directly. No Critical/Important finding remains. D-07 legacy pending-snapshot minor is real but not load-bearing for new flows or E; parked for optional F cleanup.
- D phase complete. Stop before E-00/E/F as instructed. Branch remains local, unpushed; no merge performed.
