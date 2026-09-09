# D-04 implementation report

Status: PASS. All eight existing Markdown call sites now route through the D-03 shared renderer; D-05 throttling and D-07 transport remain NOT RUN.

## Implementation

- Replaced the eight opening `ReactMarkdown` call sites across Chat, Thinking, Event Detail, Event Analysis Result, Report Detail and ReportViewer with the named `MarkdownRenderer` barrel import from `@/components/markdown`.
- Assigned the existing visual contexts to `chat`, `thinking`, `event`, `analysis` and `report`. Live Chat and Thinking calls pass `streaming` with `complete={false}`; Event Analysis passes its existing `isStreaming` state. No render cursor, frame/100 ms throttle, retry, resume or transport behavior was added.
- Removed consumer-owned `remark-gfm` imports, plugin arrays and `prose`/`chat-markdown`/`think-markdown` wrappers that duplicated or conflicted with shared renderer ownership. Existing content strings and API/data interfaces are unchanged.
- Constrained `normalizeMarkdown(md: string)` to return the source unchanged. This includes empty strings, LF/CRLF/CR, legal headings/lists/tables, multiple blank lines, fenced code, malformed Markdown and raw HTML. The renderer remains solely responsible for parsing, rejecting raw HTML and safe fallback.
- Added consumer-contract regressions for every consumer and all eight calls. A recursive `web/src` source assertion permits parser package imports only in `components/markdown/MarkdownRenderer.tsx`.

The Vercel React best-practices guidance informed the single shared rendering dependency and removal of repeated parser/plugin configuration. No new memoization, state, effect or dynamic loading was needed.

## TDD evidence

### RED

Command from `web`:

```sh
npm run test:unit -- markdown
```

Result: FAIL as expected before migration: 3 files executed, 12 of 165 tests failed. Failures proved that legal headings, inline heading-like text, fenced code and multiple blank lines were rewritten; mixed newlines lacked the asserted compatibility contract; all five consumer files still imported/used direct parsers; the eight shared-renderer variant calls and streaming props were absent.

After the first implementation pass, the recursive source assertion had one test-only failure because Vite rewrote a statically analyzable `new URL` as a non-file asset URL. The test now supplies the relative path at runtime; this does not alter product code.

### GREEN

Command from `web`:

```sh
npm run test:unit -- markdown
```

Result: PASS, 3 files and 165 tests passed, including 17 D-04 regression cases; no warnings.

## Verification

| Check | Result | Evidence |
| --- | --- | --- |
| `npm run test:unit -- markdown` | PASS | 3 files, 165 tests passed |
| `npm run test:unit` | PASS | 4 files, 166 tests passed |
| Focused ESLint on six product files plus the D-04 test | PASS | Exit 0; 11 warnings are pre-existing findings in the large Chat/Event consumers; no new errors |
| `npm run lint` | PASS | TypeScript and ESLint exit 0; 0 errors and the documented 68 existing warnings |
| `npm run build` | PASS | TypeScript and Vite production build pass; 2728 modules; JS 2147.86 kB / gzip 671.02 kB; documented >500 kB warning remains |
| `rg -n 'ReactMarkdown\|remark-gfm' web/src` | PASS | Matches only `components/markdown/MarkdownRenderer.tsx`; recursive unit assertion enforces the same import boundary |
| Eight-call source scan | PASS | Chat 4, Event Detail 1, Event Analysis 1, Report Detail 1, ReportViewer 1; all use the expected variant |
| `git diff --check` | PASS | No whitespace errors |
| D-05 throttling / D-07 transport / E / F / browser smoke | NOT RUN | Outside D-04; the known old dark smoke failure is not expanded here |

## Files changed

- `web/src/pages/chat/index.tsx`
- `web/src/pages/events/components/EventDetailModal.tsx`
- `web/src/pages/event-analysis/components/ResultPanel.tsx`
- `web/src/pages/reports/components/ReportDetailModal.tsx`
- `web/src/components/report/ReportViewer.tsx`
- `web/src/utils/index.ts`
- `web/tests/unit/markdown-consumers.test.tsx`
- `.superpowers/sdd/final-implementation-plan-2026-08-31/task-D-04-report.md`

## Self-review and concerns

- Re-read the complete scoped diff and verified consumer props/data shapes, modal/report services, Chat state and transport logic are unchanged.
- Tightened the initial newline-normalization implementation during self-review: converting CRLF/CR to LF would have changed valid fenced-code copy source, so the final helper preserves every input byte.
- No D-04 design finding required a product change or suppression. The automatic design hook reported two gray-on-color findings at existing Chat lines 787 and 928; those lines were not changed by D-04 and remain standing for their owning UI scope.
- Existing shared `progress.md` changes and untracked `grill-truth.md` were preserved and excluded from this task commit.
