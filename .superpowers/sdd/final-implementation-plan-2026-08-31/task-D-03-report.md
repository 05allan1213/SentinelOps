# D-03 implementation report

Status: PASS. Shared renderer implemented; consumer migration remains D-04 (NOT RUN here).

## Implementation

- Added the pure `MarkdownRenderer` display component and public type/barrel export with the exact six variants and streaming/complete inputs. Variants change typography/density only. No API, Run/Chat/SSE lifecycle, loading/retry, Query or Zustand state was added.
- All variants share `remark-gfm`, one bounded rehype highlight plugin, safe links/images, headings, lists/task lists, blockquotes, independent table/code scrolling, breakable long URLs, and escaped parser-error fallback with a local status label.
- `rehype-highlight@7.0.2` constructs one restricted lowlight instance with the 13 canonical registrations: TypeScript, JavaScript, JSON, Go, Python, SQL, Shell, Bash, YAML, Markdown, CSS, HTML, XML. HTML/XML share one XML grammar implementation. Auto-detection is disabled; aliases normalize through D-02.
- D-02 `highlightSkipReason` runs before each actual highlighter call. Streaming, unknown/unlabeled language, and oversized blocks bypass highlighter work. Exceptions restore that block's raw AST content while surrounding Markdown remains intact.
- `CodeBlock` accepts safe React highlighted children while its copy action continues receiving original source text. Inline code has no toolbar. Source-position metadata repairs the parser's synthetic terminal LF for fenced-copy text, preserving closed CRLF blocks and unterminated EOF source; it does not reparse the Markdown document or alter the highlighted display AST.
- Raw HTML is skipped; `rehype-raw` and HTML injection are absent. Original URLs pass straight to D-02 primitives, preventing rejected URLs from being transformed into clickable empty local links.

## Scope additions approved by coordinator

Package inspection found `rehype-highlight` imports `common` and `createLowlight` from lowlight even with explicit language options; lowlight's public barrel references both common/all grammar sets. Runtime registration alone therefore does not prove bundle restriction.

- Added `src/components/markdown/lowlight-core.ts`, an isolated adapter exporting lowlight's pinned internal core and empty `common`. Its exact `lowlight` alias is configured in both Vite and Vitest. Vitest inlines rehype-highlight so the same alias applies to dependency imports.
- The internal core path is required because lowlight 3.3.0 only exports its barrel. Future lowlight upgrades MUST revalidate this path and the bundle assertions below. No existing other lowlight callers were present.
- Added two isolated browser fixture files under `tests/ui/fixtures`; they directly import the renderer and existing stylesheet and register no production route or business services.
- Installed exact pins with `npm install --save-exact rehype-highlight@7.0.2 lowlight@3.3.0 --omit-lockfile-registry-resolved`. All six new lock records omit registry-specific `resolved` fields and retain integrity hashes.

## TDD evidence

1. RED: `npm run test:unit -- MarkdownRenderer` before renderer implementation exited 1: `Failed to resolve import "@/components/markdown/MarkdownRenderer"`; one suite failed, no tests ran. The requested behavior tests were already present and failure was expected because the component did not exist.
2. GREEN: initial implementation ran the same command: 1 file passed, 16 tests passed. Additional allowlist/unlabeled tests then passed: 30 tests.
3. Self-review RED: added CRLF and unterminated-fence copy cases, then ran the same focused command. Vitest reported `2 failed | 30 passed (32)`: both `preserves source newlines` cases failed because the parser added/changed the terminal LF.
4. Final focused GREEN after the narrow source-newline fix: `npm run test:unit -- MarkdownRenderer`: 1 file passed, 32 tests passed, exit 0, no test warnings.

The focused suite covers each of the six variants' GFM/security behavior, all 13 canonical language registrations, inline/fenced/unlabeled/indented code, exact clipboard values including blank lines/CRLF/EOF, safe/rejected external/local links, allowed/failed/rejected images, streaming/oversize pre-call guards, unknown language, highlighter exception fallback and parser exception fallback/recovery. Error branches inject faults around the real remark/rehype plugins; normal cases use their actual implementation and the production core alias.

## Final verification

| Check | Result | Evidence |
| --- | --- | --- |
| `npm run test:unit` | PASS | 3 files, 144 tests passed; exit 0; no test warnings |
| `npm run lint` | PASS | TypeScript checks passed; 0 errors, 68 existing warnings in untouched files |
| Focused changed-file ESLint | PASS | `npx eslint src/components/markdown/MarkdownRenderer.tsx src/components/markdown/CodeBlock.tsx src/components/markdown/lowlight-core.ts tests/unit/MarkdownRenderer.test.tsx tests/ui/markdown.spec.ts tests/ui/fixtures/markdown.tsx vite.config.ts vitest.config.ts`; no warnings/errors |
| `npm run build` | PASS | TypeScript plus Vite production build passed; 2698 modules; JS 2066.04 kB / gzip 646.20 kB; existing >500 kB chunk warning remains |
| `npx playwright test tests/ui/markdown.spec.ts --project=chromium` | PASS | 3/3 tests passed; rerun after copy-source amendment; only environment NO_COLOR/FORCE_COLOR notices |
| Fixture bundle module assertions | PASS | 12 exact allowed implementation grammars; core + rehype-highlight present; common/all registries absent |
| `git diff --check` | PASS | No whitespace errors |
| Product-consumer integration / D-04 / D-05 / E / F | NOT RUN | D-03 scope only; fixture is not product Runtime or provider evidence |

Browser checks use the real renderer in a Vite-only fixture. At both 1280 and 1440 widths, a 16-column table and pathological long code line each have `overflow-x:auto`, measurable internal overflow and working horizontal scrolling; the document has no horizontal overflow. Long unbroken URLs wrap. GFM task checkbox, raw-HTML rejection, blocked image placeholder and actual browser clipboard contents are asserted. The third case covers unfinished streaming plain text, closed streaming code without highlighting, and unsupported language fallback. Desktop only. No application/backend fixture is presented as real Runtime success.

## Reproducible bundle verification

The production application has no renderer consumer until D-04, so its ordinary bundle cannot establish the new renderer's grammar set. Built the isolated fixture with the same Vite configuration and inspected final emitted chunk module membership (not only import text). Run from `web`:

```sh
node --input-type=module <<'JS'
import assert from 'node:assert/strict'
import { build } from 'vite'
const expected = ['bash','css','go','javascript','json','markdown','python','shell','sql','typescript','xml','yaml']
await build({
  configFile: 'vite.config.ts',
  build: { write: false, rolldownOptions: { input: 'tests/ui/fixtures/markdown.html' } },
  plugins: [{
    name: 'verify-markdown-grammar-bundle',
    generateBundle(_, bundle) {
      const modules = Object.values(bundle).filter(item => item.type === 'chunk').flatMap(chunk => Object.keys(chunk.modules))
      const grammarModules = modules.filter(id => id.includes('highlight.js/') && id.includes('/languages/'))
      const actual = [...new Set(grammarModules.map(id => id.match(/\/languages\/([^/.]+)\./)?.[1]))].sort()
      assert.deepEqual(actual, expected)
      assert.equal(modules.some(id => /lowlight\/lib\/(common|all)\.js/.test(id)), false)
      assert.equal(modules.some(id => /lowlight\/lib\/index\.js/.test(id)), true)
      assert.equal(modules.some(id => /rehype-highlight\/lib\/index\.js/.test(id)), true)
      console.log('PASS restricted grammar modules:', actual.join(', '))
      console.log('PASS no lowlight common/all registries; core and rehype-highlight present')
    },
  }],
})
JS
```

Observed: 1938 transformed modules, `PASS restricted grammar modules: bash, css, go, javascript, json, markdown, python, shell, sql, typescript, xml, yaml`, `PASS no lowlight common/all registries; core and rehype-highlight present`, exit 0. HTML and XML share XML, hence 12 implementations for 13 canonical names. Subsequent newline amendment changes copy-source metadata only; imports/configuration remain identical.

## Files changed and self-review

Created `web/src/components/markdown/MarkdownRenderer.tsx`, `index.ts`, `lowlight-core.ts`, `web/tests/unit/MarkdownRenderer.test.tsx`, `web/tests/ui/markdown.spec.ts`, `web/tests/ui/fixtures/markdown.html`, `markdown.tsx`, and this report. Modified `web/package.json`, `web/package-lock.json`, `web/src/components/markdown/CodeBlock.tsx`, `web/vite.config.ts`, `web/vitest.config.ts`.

Self-review corrected missing HAST metadata typings, avoided requiring new Node type dependencies in the existing Vite typecheck, and fixed exact-copy terminal newline preservation with RED/GREEN evidence. Changed-file lint is clean. No unresolved correctness issue identified. Maintenance constraint: pinned lowlight internal path/alias requires revalidation on upgrade. Existing global lint and bundle-size warnings remain. Coordinator progress edits and the user's `grill-truth.md` were not staged or changed by this task. No push, ledger/plan edits, consumer migration, or E/F work.

## Review fix round 1 — exact-copy fence context (base f8f61ac)

Result: PASS. Review correctly identified that the previous closing-fence regex accepted literal four-space indentation and `>` inside fenced code. For ` ```text\nbody\n    ``` ` and ` ```text\nbody\n> ``` ` (without surrounding example spaces, EOF without LF), CommonMark retains the last line as code; the previous copy metadata incorrectly appended LF.

Removed the approximate closing-marker regex. Terminal separator recovery now compares the existing parser's code-content line count with its source-position line span: a real closing line is absent from parsed content, while a literal remains. Source EOF separators and empty unterminated fences are handled directly. This uses the same parsed HAST and existing VFile, adds no parser or business state, and leaves highlighting configuration/display AST unchanged.

- RED: `npm run test:unit -- MarkdownRenderer` after adding the regression cases reported `2 failed | 34 passed (36)`; the failing cases were `four-space literal fence` and `quote literal fence`, both exact clipboard argument assertions. The valid blockquote/list fence cases passed.
- GREEN: the same command after correction plus the empty-open-fence edge case reported `Test Files 1 passed (1)`, `Tests 37 passed (37)`, exit 0, no warnings.
- PASS: `npx playwright test tests/ui/markdown.spec.ts --project=chromium -g 'preserves fence context'` reported `1 passed (5.3s)`, exit 0. At desktop 1280 this new browser test clicks the real CodeBlock button and reads `navigator.clipboard.readText()` for both reported literal cases and valid blockquote/list nested fences. Only existing NO_COLOR/FORCE_COLOR environment notices appeared.
- PASS: `npx eslint src/components/markdown/MarkdownRenderer.tsx tests/unit/MarkdownRenderer.test.tsx tests/ui/markdown.spec.ts` exited 0 with no warnings/errors.
- PASS: `npx tsc --noEmit --pretty false` exited 0 with no output.
- PASS: `git diff --check`, no whitespace errors.
- NOT RUN in this fix round: full unit suite, full lint/build, previous browser layout cases and bundle module assertions. Their prior PASS evidence above remains historical; this amendment changes only copy metadata and its focused tests, with no dependency/config/style/consumer changes.

Files in this fix: renderer, renderer unit tests, focused browser spec, and this report appendix. No remaining issue identified in the reviewed defect; no ledger/plan/E/F changes.
