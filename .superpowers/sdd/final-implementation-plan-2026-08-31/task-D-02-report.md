# Task D-02 implementation report

**Status:** DONE
**Base:** `14e2022b661984024344e86ca160310e8050a14a`

## Implemented

- `markdown.ts`: URL API-backed link/image policy; 13 canonical languages plus 24 tested common aliases; marker-aware backtick/tilde fence state; `{content, mode: 'markdown'|'plain'}` streaming decision; documented 32 KiB/1,000-line highlight bound and skip reasons.
- `SafeLink.tsx`: unsafe/malformed destinations become text; absolute/protocol-relative HTTP(S) links receive exact `_blank` and `noopener noreferrer`; relative/in-app links stay in-window.
- `SafeImage.tsx`: HTTPS remote and relative/in-app sources only, lazy/no-referrer image, fixed `640x360` intrinsic dimensions, bounded overflow wrapper, local error fallback.
- `CodeBlock.tsx`: raw copyable source, allowlisted language label, and explicit `streaming`/`unsupported-language`/`size-limit` skip state. No D-03 renderer or highlighter package was added.
- `markdown-safety.test.ts`: table coverage for protocols, attributes, image fallback, inline/block behavior, exact copy, canonical names/aliases, limits, and safely paired/escaped/open/closed fences.

## TDD evidence

The first test draft used JSX in the brief-required `.ts` file and failed to transform (`Expected '>' but found 'Identifier'`). I corrected it to `createElement` before accepting RED evidence.

**RED** — from `web`:

```text
$ npm run test:unit -- markdown-safety
FAIL  tests/unit/markdown-safety.test.ts [ tests/unit/markdown-safety.test.ts ]
Error: Failed to resolve import "@/components/markdown/CodeBlock" from "tests/unit/markdown-safety.test.ts". Does the file exist?
Test Files  1 failed (1)
Tests  no tests
```

Expected because the D-02 modules did not exist.

**GREEN** — from `web`:

```text
$ npm run test:unit -- markdown-safety
RUN  v4.1.11 /home/monody/project/.worktrees/sentinelops-d/web
Test Files  1 passed (1)
Tests  103 passed (103)
Duration  847ms (transform 69ms, setup 64ms, import 140ms, tests 156ms, environment 391ms)
```

Result: PASS. No test executes unsafe markup.

## Verification

```text
$ npm run test:unit
Test Files  2 passed (2)
Tests  104 passed (104)
Duration  934ms (transform 102ms, setup 146ms, import 198ms, tests 177ms, environment 907ms)
```

Result: PASS.

```text
$ npx eslint src/components/markdown tests/unit/markdown-safety.test.ts
```

Result: PASS, no output.

```text
$ npm run lint
tsc --noEmit --pretty false && tsc --project tsconfig.node.json --noEmit --pretty false && eslint .
✖ 68 problems (0 errors, 68 warnings)
```

Result: PASS. TypeScript passed; the 68 warnings are the D-01 baseline and none is in a D-02 file.

```text
$ npm run build
✓ 2698 modules transformed.
(!) Some chunks are larger than 500 kB after minification.
✓ built in 659ms
```

Result: PASS with the existing chunk-size warning.

Browser smoke: NOT RUN. D-03 owns the renderer/highlighter integration required to expose these primitives through a product rendering path.

## Files

- `web/src/components/markdown/markdown.ts`
- `web/src/components/markdown/SafeLink.tsx`
- `web/src/components/markdown/SafeImage.tsx`
- `web/src/components/markdown/CodeBlock.tsx`
- `web/tests/unit/markdown-safety.test.ts`
- `.superpowers/sdd/final-implementation-plan-2026-08-31/task-D-02-report.md` (ignored evidence file)

## Self-review / concerns

- Fence closers require the same marker and at least the opener length; escaped, indented, mixed, short, or trailing-text markers do not close the fence.
- UTF-8 and LF/CRLF/CR limits return early and preserve raw copyable text beyond the bound.
- D-02 files are lint-clean; no renderer, package, global CSS, route, store, or API changes entered scope.
- No D-02 correctness concerns. Repository baseline lint and bundle warnings remain; browser integration is intentionally NOT RUN until D-03.

## Review fix round 1

Fixed both Important findings from review against base `eb232a5b2c194683d6e088086ada9c7768ebb4d6`:

- Link validation now rejects every backslash before URL resolution, preventing `\\\\host/path` and `/\\host/path` from becoming clickable current-window links after special-scheme normalization. Component regressions require both forms to render as non-clickable text.
- Fence scanning now splits LF, CRLF, and CR-only Markdown lines. Open and closed CR-only backtick and tilde fences are covered.

**RED** — from `web`:

```text
$ npm run test:unit -- markdown-safety
Test Files  1 failed (1)
Tests  6 failed | 105 passed (111)
```

The six expected failures were the two rejected-link helper cases, the same two `SafeLink` non-clickable cases, and open CR-only backtick/tilde fence cases.

**GREEN** — from `web`:

```text
$ npm run test:unit -- markdown-safety
Test Files  1 passed (1)
Tests  111 passed (111)
Duration  870ms (transform 65ms, setup 61ms, import 136ms, tests 152ms, environment 423ms)
```

Result: PASS.

```text
$ npx eslint src/components/markdown/markdown.ts tests/unit/markdown-safety.test.ts
```

Result: PASS, no output. Browser renderer/highlighter verification remains owned by D-03; no D-03 or D-05 work was added.
