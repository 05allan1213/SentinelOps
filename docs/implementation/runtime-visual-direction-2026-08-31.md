# Runtime Operate Visual Direction (E-00 gate)

> Task: E-00 of `output/final-implementation-plan-2026-08-31.md`.
> Date: 2026-09-10 (Asia/Shanghai). Branch `feat/phase-e-20260910`, base `f461a3c`.
> Status: PASS (documentation only). No product code, API, migration, route or dependency was changed by this gate.
> Scope: desktop 1280 and 1440. Mobile navigation is `OUT OF SCOPE`.

This artifact fixes the visual and interaction direction for the Runtime surfaces before any Runtime page
component is implemented (E-01 data layer may proceed in parallel because it contains no page code). E-03..E-07
must follow it; deviations must be recorded as findings in the task report instead of being invented silently.
Missing direction fields are a gate failure: all fields listed in the task card are covered below.

## 1. Pre-flight record (gpt-taste, Operate mode)

The `gpt-taste` skill was read in full (`/home/monody/.agents/skills/gpt-taste/SKILL.md`) and its mandatory
deterministic selection record is reproduced here. Because this product is a frozen operations console rather
than a marketing page, the skill is applied in **Operate mode**: its structural discipline (variation,
density, contrast, spacing) is used, and its AIDA / hero / bento / GSAP / random-font mandates are explicitly
overridden by the frozen SentinelOps direction (plan `Global Constraints`, `grill-truth.md` section 4).

```python
>>> prompt = "SentinelOps Runtime Operate surface"
>>> seed = sum(ord(c) for c in prompt) % 997
>>> seed
631
>>> random.seed(631)
>>> random.choice(["cinematic_center", "artistic_asymmetry", "editorial_split"])
'cinematic_center'
>>> random.choice(["Satoshi", "Cabinet Grotesk", "Outfit", "Geist"])
'Cabinet Grotesk'
>>> [random.choice(COMPONENTS) for _ in range(3)]
['horizontal_accordion', 'infinite_marquee', 'feedback_carousel']
>>> [random.choice(GSAP_PARADIGMS) for _ in range(2)]
['scroll_pinning', 'scrubbing_text_reveal']
```

### Override table (required)

| Skill mandate | Simulated selection | Ruling | Applied instead |
| --- | --- | --- | --- |
| AIDA page structure with hero / bento / desire / CTA | `cinematic_center` hero | Rejected. Runtime is an authenticated console with no marketing funnel, no hero, no pricing/CTA. | Persistent dark sidebar + one work area; per-surface header strip, filter bar, content region. |
| Display typography from the four skill fonts; "never Inter" | `Cabinet Grotesk` | Rejected. Adding a webfont would change the product's established shell and bundle behaviour. | Keep `Inter` + `JetBrains Mono` (`web/src/assets/styles/index.css:94`, `web/index.html:10`). |
| Unique component architectures (accordion / marquee / carousel) | all three | Rejected. They hide operational state or add motion without diagnostic value. | Dense data table, tabbed detail shell, timeline list, metadata panels, DAG/history list. |
| GSAP scroll pinning and scrubbing text reveals | both | Rejected. Global constraint forbids GSAP; motion must stay local and cheap. | CSS/Framer transitions 150-250ms, streaming-only feedback, terminal states stop animating. |
| Randomized layout per generation; huge section padding (`py-32`+) | n/a | Rejected. Console layout must be stable, predictable and dense. | Fixed 8/16/24/32px spacing scale; work area keeps the existing `p-8` shell padding (`Layout.tsx:16`). |
| "No emojis" and professional formatting | n/a | Kept. | Text + Lucide icons only; no emoji in Runtime UI copy. |
| Contrast and legibility discipline | n/a | Kept. | Body text `gray-700`+ on white; muted text never used for status-critical values. |

## 2. Current console inventory (read-only evidence)

| Area | Current fact | Anchor |
| --- | --- | --- |
| App shell | Fixed dark sidebar plus a `p-8` scrollable main region; no Runtime route exists. | `web/src/components/layout/Layout.tsx:8-21` |
| Sidebar groups | Three groups: `AI 能力`, `数据管理`, `系统监控`, each with an accent colour and Lucide icons; width 200-370px, collapsed 72px. | `web/src/components/layout/Sidebar.tsx:44-75`, `:114-119` |
| Tokens | Tailwind 4 `@theme` variables for primary/success/warning/danger/gray, plus `.dark` overrides; indigo/blue accents already used in navigation. | `web/src/assets/styles/index.css:5-56`, `:84-90` |
| Typography | `Inter` for UI, `JetBrains Mono` available for code/identifiers. | `web/src/assets/styles/index.css:94`, `web/index.html:10` |
| Primitives | `StatCard` (blue/emerald/amber/red/gray/indigo tones), `DataBlock` (dark `#0D1117` code surface), `Pagination`, `CustomSelect`, `ConfirmDialog`. | `web/src/components/common/` |
| Chat reading | Assistant column is clamped to 760px and now `min-w-0 max-w-none` inside the bubble; user bubbles stay right-aligned. | `web/src/pages/chat/index.tsx:681`, `:932-946` |
| Legacy wide page | Event Analysis still contains a fixed `w-[960px] shrink-0` column; F-01/F-15 own that regression. | `web/src/pages/event-analysis/index.tsx:366` |
| Cyber theme | A legacy cyber palette + scan-line keyframes exist for the old Agent Analysis page. | `web/src/assets/styles/index.css:722-740` |
| Frozen Runtime contract | Canonical statuses, `availability`/`data_quality`/`reason_code`/`not_run`, query keys and routes. | `grill-truth.md` sections 4/5.4.2/9; plan `Frozen Public Contract` |

Consequence: the Runtime surfaces inherit the existing shell and tokens. The legacy cyber theme is **not** a
Runtime direction; Runtime must not import or extend it.

## 3. Information architecture

```
Agent Runtime                     (new sidebar group, accent #818CF8)
  Runs            /runtime/runs
  Capabilities    /runtime/capabilities
  Safety          /runtime/safety
  Worker Health   /runtime/worker-health

/runtime/runs/:runId              tabs, active tab is URL search state
  overview | timeline | attempts | effects | evidence | context | trace
```

- No second top-level console, no `/status` center, no per-page navigation duplication.
- Eval / Release / Retention are **read-only sections inside Worker Health**, never new nav items.
- Existing routes and labels are untouched; `/ops/v1` OpsRun is never reused as a Runtime row.

## 4. Hierarchy, density, typography, spacing

| Token | Rule |
| --- | --- |
| Page header | Title (18-20px, semibold) + one-line factual subtitle, right-aligned actions/refresh. No hero, no illustration. |
| Section | 20-24px vertical rhythm between sections; 8px inside a metadata row; 16px inside a panel. |
| Panel | `rounded-xl border border-gray-200 bg-white`; shadow only on hover for interactive rows, never stacked shadows. |
| Density | Runs table 40-44px rows; detail metadata 2- or 3-column grid with `min-w-0`; code/identifier values in `font-mono text-xs`. |
| Typography | Numeric/timestamp/ID values use `tabular-nums` or mono; labels are 12px `text-gray-500`; values 13-14px `text-gray-900`. |
| Colour | Indigo/blue primary for navigation and primary actions; gray surfaces; semantic colours only for status. Chart-like decoration is out. |
| Width | Content fills the work area up to ~1440px; tables scroll locally, page body never scrolls horizontally at 1280/1440. |
| Elevation | One level: border-led surfaces. No Card Soup (repeated decorative cards), no neon borders, no gradients other than existing page backgrounds. |

## 5. Observability-console language

- Say what the server proved and when: `状态 / 阶段 / 最后心跳 / generation / 数据质量`.
- Distinguish **configured** (`configured_state`) from **observed** (`observed_worker_state`); never show an
  inferred value as observed fact.
- Distinguish Run status from transport status (`连接中 / 已断开 / 重试中`) and from operation status.
- Use `availability=available|partial|unavailable` + `data_quality=complete|reconstructed|partial|unknown` +
  `reason_code` verbatim from the server; `not_run` is shown as independent evidence, never as an availability value.
- `reason_code=not_observed` never renders as `已加载 / 健康 / active`.

## 6. Status semantics (frozen)

| Server value | Label | Tone | Notes |
| --- | --- | --- | --- |
| `pending` | 待执行 | gray | Not success, not failure. |
| `running` | 运行中 | blue | Streaming allowed; terminal never inferred from transport. |
| `waiting_approval` | 等待审批 | amber | Links to Approval facts; approver role unchanged. |
| `retryable_failed` | 可重试失败 | amber-outline | Non-terminal; distinct from `failed`. |
| `parked` | 已搁置 | amber | Requires admin Recovery; never success. |
| `reconciling` | 对账中 | violet-outline | Non-success, non-terminal. |
| `succeeded` | 成功 | emerald | The only success styling in the surface. |
| `failed` | 失败 | red | Terminal failure. |
| `canceled` | 已取消 | gray-strikethrough | Terminal, not failure-success ambiguity. |
| legacy `success` (read only) | 成功（旧记录） | emerald-outline | Compatibility display only. |
| unknown value | 未知 | gray-dashed | Fail-closed presentation. |

Same rule for operation status (`accepted/running/succeeded/failed/canceled/rejected`) and for quality
metadata: only `succeeded` (operation) or `available + complete` (quality) may look "green".

## 7. Timeline, tabs, table, detail usage

- **Runs table**: server-driven columns `run_id / session_id / agent / status / current_phase / attempt /
  worker_id / recovery_mode / budget / created_at / duration_ms`; filter bar above; pagination below; row click
  navigates to detail. Legacy rows carry a read-only chip and no Recovery affordance.
- **Tabs**: semantic buttons with `role="tab"`/`aria-selected`, keyboard reachable, active tab in the URL
  (`?tab=`); tab bodies keep stable min-heights and own their scroll regions.
- **Timeline**: one row per canonical event, ordered by `seq`; each row shows `seq / created_at / event_type /
  attempt / generation / trace_id / operation_id`; payload collapsed by default and rendered through the shared
  `MarkdownRenderer` only for bounded summaries. Grouping/filtering is display-only and never drops events.
- **Metadata panels**: label/value rows or definition lists; long identifiers wrap or scroll locally.
- **Tables inside tabs** (attempts, checkpoints, effects, evidence, traces): same density rules; independent
  horizontal scroll; empty states distinguish "no rows" from "source unavailable".

## 8. State language for every surface

| State | Rendering rule |
| --- | --- |
| loading | Skeleton rows in place, header remains, no layout jump; never a spinner covering the whole page. |
| empty (successful zero rows) | Neutral empty panel "暂无数据" + the active filters; not an error. |
| partial | Rows render + amber "数据部分可用" with `reason_code`; partial data is never presented as complete. |
| unavailable | Gray panel with `reason_code` (`not_observed` etc.) and no fabricated values. |
| failed | Red inline error with retry; previously loaded rows stay visible. |
| not_run | Explicit "未执行" evidence chip, independent of availability. |
| forbidden | Neutral "无权限查看" without leaking whether the resource exists. |

## 9. Recovery interaction hierarchy (E-05)

1. Entry: only from Run Detail, only for admin, only when the server's `allowed_recovery_actions` contains the
   action; legacy/terminal/incompatible Runs expose no bypass.
2. Dialog: action-specific title, impact statement, required non-empty reason, required expected generation
   (plus expected compatibility hash for `restore`), submit disabled while pending, one idempotency key per user
   submission.
3. Result: `202 + operation_id` is acknowledged immediately; `OperationProgress` then polls
   `GET /runtime/v1/operations/{id}` only while non-terminal and stops at terminal. No optimistic Run success.
4. Destructive styling: `cancel`/`restore` use the danger/outline treatment; `resume`/`replay` use primary.

## 10. Motion, performance, accessibility

- Transitions 150-250ms (`duration-150`/`duration-200`), transform/opacity only; no layout-thrashing animation.
- Streaming rows update without re-animating the whole timeline; terminal rows stop pulsing.
- `prefers-reduced-motion: reduce` disables non-essential motion (existing CSS pattern in
  `web/src/assets/styles/index.css` is extended to Runtime surfaces).
- Focus rings remain visible on all interactive elements; dialogs trap focus (F-05 owns the primitives, E-05
  reuses `ConfirmDialog` until then); tables expose a captioned, keyboard-navigable structure.

## 11. Per-surface acceptance (reviewer checklist)

| Surface | Must be verifiable from the artifact + later task evidence |
| --- | --- |
| Navigation | Agent Runtime group with exactly four children; existing groups untouched; dark sidebar retained. |
| Runs | Filter bar (URL state), canonical badges, legacy chip, five distinct states, local table scroll, row navigation. |
| Run Detail | Tabs with URL state; Overview shows status/phase/worker/lease/generation/compatibility/gates/budget quality; Timeline keeps every event. |
| Attempts/Checkpoints | Modes, worker/generation/trace, fingerprints, missing/corrupt/expired/incompatible reasons; no opaque bytes. |
| Recovery | Server-gated buttons, required reason/generation, 202 acknowledgement, reconnectable operation, no optimistic success. |
| Effects/Evidence/Context/Trace | DAG + history, explicit quote expansion, revisions/hashes, trace quality, local scroll, no raw provider/secret data. |
| Capabilities/Safety/Worker Health | configured vs observed separation, shadow/effective gates, stale/missing worker states, Eval/Release/Retention `not_run`/`unavailable` honesty, no write controls. |
| Global | No page-level horizontal overflow at 1280/1440; no Neon/Cyberpunk/Card Soup/GSAP; existing brand primitives reused. |

Gate result: all required direction fields are present. Reviewer acceptance is recorded in
`output/runtime-visual-gate-2026-08-31.md`; a rejected field becomes a named F-07 fix instead of a silent change.
