# Task D-01 Implementation Report

## Status

DONE. D-01 establishes a focused Vitest/jsdom component-test cycle while preserving the existing frontend scripts and runtime architecture. The required unit, lint, and build gates pass.

## What I implemented

- Added `npm run test:unit`, backed by `vitest run`, without changing the existing `dev`, `build`, `lint`, `preview`, or `test:smoke` scripts.
- Added the exact pinned dev dependencies required by the brief:
  - `vitest@4.1.11`
  - `@testing-library/react@16.3.3`
  - `@testing-library/jest-dom@7.0.1`
  - `jsdom@30.0.1`
- Added `web/vitest.config.ts` with:
  - `environment: 'jsdom'`
  - one shared setup file
  - Vitest globals
  - the existing `@` source alias resolved from `web/src`
  - an explicit unit-test include pattern so Vitest does not collect Playwright suites
- Added one shared setup file that registers the Vitest-specific jest-dom matchers.
- Added one provider-free render smoke test. It imports the existing `ConfidenceBadge` source component through `@/*`, renders it with Testing Library, and verifies the jsdom output with `toBeInTheDocument()`.
- Added no HTTP client, QueryClient, state store, browser runner, network fixture, or mocked Runtime success.

## TDD evidence

### RED — expected FAIL

Command:

```text
npm run test:unit
```

Before the final render smoke assertion existed, `tests/unit/smoke.test.ts` contained one intentional temporary mismatch. Vitest found the focused unit file and failed exactly on that assertion:

```text
RUN  v4.1.11 /home/monody/project/.worktrees/sentinelops-d/web
FAIL  tests/unit/smoke.test.ts > focused component test cycle > reports the temporary assertion before the render smoke test exists
AssertionError: expected 'pending smoke test' to be 'mounted smoke test'
Test Files  1 failed (1)
Tests       1 failed (1)
```

Exit status: `1` (expected). This proves the new test cycle detects a failing component-contract assertion.

### GREEN — PASS

Command:

```text
npm run test:unit
```

After replacing the temporary mismatch with the provider-free source-component render:

```text
RUN  v4.1.11 /home/monody/project/.worktrees/sentinelops-d/web
Test Files  1 passed (1)
Tests       1 passed (1)
Duration    636ms
```

Exit status: `0`.

After a clean lockfile installation with `npm ci`, the same command passed again:

```text
Test Files  1 passed (1)
Tests       1 passed (1)
Duration    614ms
```

Exit status: `0`.

## Other verification

### Dependency installation and lockfile — PASS

Command:

```text
npm install --save-dev --save-exact vitest@4.1.11 @testing-library/react@16.3.3 @testing-library/jest-dom@7.0.1 jsdom@30.0.1
```

Result: PASS; 413 packages installed. `npm ls --depth=0` and direct lockfile inspection both report the four exact requested versions. A JSON comparison against `HEAD:web/package-lock.json` reports `no existing lockfile package entries changed`; the lockfile adds only the requested top-level entries and their dependency graph.

Command:

```text
npm ci
```

Result: PASS; a clean install from `package-lock.json` installed 413 packages, and the subsequent unit test passed.

Environment used: Node `v24.19.0`, npm `12.0.2`, lockfile version `3`.

### Lint — PASS with baseline warnings

Command:

```text
npm run lint
```

Result: PASS (exit `0`), with `0 errors` and `68 warnings`. Every warning is in pre-existing `web/src/**` files; none points to a D-01 file. Examples include existing React hook/compiler warnings in `ReportGenerateModal.tsx` and `pages/chat/index.tsx`. Per scope, those warnings were recorded and not changed.

### Build — PASS with baseline warning

Command:

```text
npm run build
```

Result: PASS (exit `0`). TypeScript checks passed, Vite `8.2.2` transformed 2,698 modules, and the production bundle completed in 835ms. Vite emitted the pre-existing warning that one minified chunk exceeds 500 kB; D-01 test/config files are not part of the production bundle.

### Existing Playwright command — available; full run reproduces baseline FAIL

Command:

```text
npm run test:smoke -- --list
```

Result: PASS; Playwright listed the existing 3 Chromium tests from `tests/smoke/current-ui.spec.ts`, proving the script remains available and Vitest has not replaced the browser runner.

Command:

```text
npm run test:smoke
```

Result: FAIL, `2 passed / 1 failed`. The existing dark-variant case could not find the pre-existing `思考链路` element at `tests/smoke/current-ui.spec.ts:68`. The coordinator confirmed this is the already documented F-02 baseline failure. D-01 changes neither product UI nor Playwright assertions, so this unrelated failure was not fixed.

### Repository checks — PASS

Command:

```text
git diff --check
```

Result: PASS with no whitespace errors.

`npm run` lists all six expected scripts: `dev`, `build`, `lint`, `preview`, `test:smoke`, and `test:unit`.

## Files changed

- `web/package.json`
- `web/package-lock.json`
- `web/vitest.config.ts`
- `web/tests/unit/setup.ts`
- `web/tests/unit/smoke.test.ts`

The ignored report is intentionally not force-added. The coordinator-owned modified `progress.md` and untracked `grill-truth.md` were preserved and excluded from the task commit.

## Self-review

- Completeness: all D-01 files, exact dependency pins, script/config fields, shared setup, alias import, jsdom render, and command-preservation requirements are present.
- Test quality: the smoke test exercises the real source alias, React rendering, Testing Library query, shared matcher setup, Vitest globals, and jsdom. It does not claim network, Runtime, provider, rollout, or production evidence.
- Scope discipline: no product source, Playwright suite, TypeScript configuration, backend file, image, QueryClient, store, or later D/E/F task was changed.
- Lockfile discipline: existing lockfile package records were compared structurally and remained unchanged.
- React review: the test renders an existing module-level component; it does not create a nested component type or introduce state/effect work.
- Fresh-checkout behavior: `npm ci` followed by `npm run test:unit` passes deterministically.

## Issues and concerns

- `npm run lint` passes with 68 pre-existing warnings in unrelated source files.
- `npm run build` passes with the pre-existing chunk-size warning.
- The full existing Playwright smoke run remains FAIL at the documented F-02 dark-variant assertion (`2 passed / 1 failed`); command availability is preserved, and this is outside D-01.
- Hosted CI, real provider/Runtime behavior, rollout/rollback, images, D-02 through D-07, E, and F are NOT RUN and are not implied by the local D-01 PASS.

## Fix round 1 — registry-neutral lock metadata

### Review finding

The D-01 dependency records contained `resolved` tarball URLs for `registry.npmmirror.com`, inherited from the developer-level npm registry setting. The repository has no `.npmrc` establishing that mirror, and all 405 pre-D package records were registry-neutral. Keeping those URLs would make the undeclared mirror part of fresh-checkout dependency resolution.

### Repair

Regenerated lock metadata with npm's registry-neutral mode:

```text
npm install --package-lock-only --omit-lockfile-registry-resolved
```

Relevant output:

```text
up to date in -681ms

148 packages are looking for funding
  run `npm fund` for details
```

The resulting `web/package-lock.json` removes only the `resolved` field from the 81 package records introduced by D-01. A structural comparison against pre-D base `c0b819158fad3fbe02132050d47c9261b65d1a4a` and fix-round base `968d2c542ecddda7af69bae68d45781a6dbcffb3` reports:

```text
added package records: 81
remaining resolved fields: 0
records changed versus fix-round base: 81
pre-existing records changed: 0
total package records: 486
```

Versions, integrity hashes, dependency relationships, exact top-level pins, and every pre-existing package record are preserved.

### Covering verification

Command:

```text
npm ci
```

Output:

```text
added 413 packages in 10s

148 packages are looking for funding
  run `npm fund` for details
```

Result: PASS (exit `0`). The registry-neutral lockfile supports a clean dependency installation.

Command:

```text
npm run test:unit
```

Output:

```text
npm notice run sentinelops-web@1.0.0 test:unit
npm notice run vitest run

RUN  v4.1.11 /home/monody/project/.worktrees/sentinelops-d/web

Test Files  1 passed (1)
Tests       1 passed (1)
Duration    640ms (transform 34ms, setup 59ms, import 48ms, tests 24ms, environment 415ms)
```

Result: PASS (exit `0`).

Lint and build were not rerun because the repair changes only lockfile fetch metadata; their original D-01 PASS results remain recorded above. The deferred baseline lint/build warnings and F-02 Playwright failure are unchanged.

### Fix-round self-review

- Scope: only `web/package-lock.json` changed in the fix commit; the report remains ignored.
- Portability: no package record contains a `resolved` URL, matching the repository's existing convention and avoiding any undeclared registry dependency.
- Integrity: all 486 package records and all D-01 versions/integrities remain present; no pre-existing package record changed.
- Verification: a clean `npm ci` and the focused jsdom component smoke test both pass.
