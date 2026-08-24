# P02 前端框架升级与浏览器 smoke 基线

- Status: `PASS`
- Started from: `main` / `e12d7106f027641499c13da3962c8bbab6602847`，起始工作树干净
- Spec references: 上位 Spec `4.3`、`4.4`、`Task 1B`；执行 Plan `P02`
- Actual files: `.gitignore`、`.dockerignore`、`manifest/docker/Dockerfile.frontend`、`web/package.json`、`web/package-lock.json`、`web/eslint.config.js`、`web/playwright.config.ts`、`web/tests/smoke/current-ui.spec.ts`、`web/vite.config.ts`、`web/tsconfig*.json`、`web/src/vite-env.d.ts`、`web/src/assets/styles/index.css`；删除重复 lockfile、旧 PostCSS/Tailwind 配置、Vite 生成物和已跟踪 tsbuildinfo；另删除 3 条已失效的 TypeScript-ESLint 抑制注释
- Red test and expected failure: 先建立 Playwright 当前 UI smoke；旧依赖没有 `@playwright/test`，目标命令在加载配置时按预期 `FAIL`
- Local commands: 见“执行记录”
- Results: P02 局部门禁 `PASS`；完整前端 E2E、完整后端回归和 P43 全量门禁 `NOT RUN`
- Key assertions: 目标矩阵 16 项精确；Node 镜像内为 v24.19.0；唯一 npm lockfile、Vite 配置和 CSS theme 真值；5173 保持；Tailwind 4 官方 Vite Plugin 与 class dark variant 生效；3 个 Chromium Case 全部执行；官方源 audit 为 0 vulnerabilities；没有 Approval UI 或页面重设计
- Deviations from recommended route: TypeScript 7 删除 `baseUrl` 并要求显式 CSS side-effect 类型；稳定版 typescript-eslint 尚不接受 TypeScript 7 peer，lint 改用支持 ESLint 10 的稳定 Babel parser，严格类型检查仍由 `tsc` 执行；React 19 要求把 Lucide 升至仍保留现有 GitHub icon 的兼容稳定版；安全审计要求额外升级 ECharts 并刷新 flatted
- Raw artifact references: 无；失败信息与关键短输出已内联，Playwright 失败产物由后续 PASS 运行清理，两个本地测试镜像验证后已删除
- Unfinished items: P02 无未完成项；67 条存量 ESLint warning、bundle size warning、完整 E2E 与 P03 及后续能力不属于本单元

## Boundary Audit

- 目标：精确升级 P02 版本矩阵，收敛 npm lockfile、Vite 配置和 Tailwind theme 真值，并建立可复用的 Chromium smoke。
- 明确非目标：不开发 Approval UI，不重设计页面，不修改后端、Agent、API contract、Runtime、数据库或 P03 及后续能力。
- 兼容契约：保留现有路由、认证重定向、主布局、5173 开发端口、class-based dark variant 和当前视觉/交互语义。
- 安全不变量：浏览器 smoke 只使用虚构 Token 并拦截 API；不读取或记录真实 Cookie、Token、Key、DSN 或业务数据。
- 预计修改：`web/package.json`、`web/package-lock.json`、前端 Dockerfile、Vite/CSS/TypeScript/ESLint 配置、Playwright 配置与 smoke、Git/Docker 忽略规则；删除 P02 指定的重复 lockfile、生成物和旧 CSS 工具链真值。
- 验证方式：真实 Red；`npm ci`、lint、build、Chromium smoke；版本、端口、lockfile/config/theme 唯一性和禁止项扫描；P01 直接回归门禁。
- 回滚方式：通过本单元独立本地提交的普通反向提交恢复；不涉及远程、数据库或持久运行栈。

## Build-or-Reuse

- 现有代码能力：原位保留 React 页面、Router 路由、Zustand Store、TanStack Query、Axios Client、ActionQueue、ConfirmDialog 和 RunsPanel，不创建新页面、Store、Client 或设计系统。
- 官方能力：使用 React 19、Router 7、Vite 8、TypeScript 7、Tailwind 官方 Vite Plugin/CSS-first dark variant 和 Playwright Chromium runner，不复制框架或浏览器驱动能力。
- 剩余业务缺口：仅有框架兼容、唯一配置/lockfile、CSS theme 迁移和当前 UI smoke 基线。
- 最薄 Adapter：只允许现有调用点因锁定版公开 API 或类型检查产生的必要兼容修改；smoke 通过虚构认证状态与网络拦截观察现有 UI。

## Red 证据

先新增 `playwright.config.ts` 与 3 个当前 UI Case，再在旧依赖下执行：

```text
$ npx --yes @playwright/test@1.62.1 test tests/smoke/current-ui.spec.ts --project=chromium
Error: Cannot find package '@playwright/test' imported from .../web/playwright.config.ts
```

命令退出 1，状态为预期 `FAIL`。失败发生在目标配置加载阶段，不是 0 tests。Case 固定未认证重定向、关键受保护路由与主布局、class-based dark variant，并捕获浏览器 `pageerror`。

## 实现与兼容证据

### 精确版本与唯一真值

结构化 package 检查逐项核对 Node、React/DOM、Router、Vite、TypeScript、Tailwind/官方 Vite Plugin、React types、Zustand、TanStack Query、Axios、ESLint、Vite React Plugin 和 Playwright，共 16 项全部精确匹配 Spec，状态 `PASS`。

`package.json` 已无直接 `autoprefixer`、`postcss` 或 `rollup`。仓库实际文件只有 `web/package-lock.json` 一个前端 lockfile、`web/vite.config.ts` 一个 Vite 配置；不存在 `*.tsbuildinfo`、`tailwind.config.*` 或 `postcss.config.*`。`vite.config.ts` 仍为 5173，并组合 `react()` 与官方 `tailwindcss()` Plugin。

Tailwind theme 已全部迁入 `index.css` 的 `@theme`；入口使用 `@import "tailwindcss"`，并精确保留：

```css
@custom-variant dark (&:where(.dark, .dark *));
```

Tailwind 4 不再通过 `@apply` 组合项目自定义 `.btn` / `.tag` / `.card` class，因此仅将同一声明组展开到既有组件选择器，没有改变组件 JSX 或设计语义。

### 必要兼容调整

- TypeScript 7 已移除 `baseUrl`；alias 改为显式 `./src/*`。构建改为两个 `tsc --noEmit` 加 Vite，禁止重新生成 Vite JS/d.ts 和 tsbuildinfo；`vite-env.d.ts` 提供官方 CSS side-effect import 类型。
- 当前稳定 `typescript-eslint@8.67.0` 的 TypeScript peer 上限仍为 `<6.1.0`。没有使用预发布版、`--force` 或 `--legacy-peer-deps`；ESLint 10 使用稳定 `@babel/eslint-parser@8.0.1` + TypeScript/JSX syntax plugin，`tsc` 保持严格 error 门禁。
- React 19 与存量 `lucide-react@0.312.0` peer 不兼容。采用支持 React 19 且保留现有 `Github` export 的稳定 `0.545.0`；未采用已移除该 icon 的 1.x，从而无需改 UI。
- 官方 npm audit 发现 ECharts `<6.1.0` XSS 与 ESLint 间接 `flatted@3.3.4` high advisory。按 Spec 普通库安全升级规则，将 ECharts 精确升至 6.1.0，并在既有 semver 范围内刷新 flatted 至 3.4.4；最终 audit 为 0 vulnerabilities。
- npm 12 对镜像源中的 remote tarball 有 deny-by-default 约束；最终 lockfile 省略 registry-specific `resolved` URL，保留版本与 integrity，使宿主机镜像源和官方 Node 镜像内的普通 `npm ci` 均通过。

## 局部门禁

从一次干净 `npm ci` 后执行最终门禁：

```text
$ npm run lint
✖ 67 problems (0 errors, 67 warnings)

$ npm run build
vite v8.2.2 building client environment for production...
✓ 2696 modules transformed.
✓ built

$ npx playwright test tests/smoke/current-ui.spec.ts --project=chromium
Running 3 tests using 1 worker
3 passed

$ npm_config_registry=https://registry.npmjs.org npm audit --audit-level=moderate
found 0 vulnerabilities
```

lint 的 67 条均为启用 ESLint 10 / React Hooks 新规则后暴露的存量 warning；严格 TypeScript 与 ESLint 解析均无 error。批量重写 effect 数据加载、函数顺序或 React Compiler 建议会改变大量页面行为并越过 P02，因此没有纳入本单元。Vite 仍报告既有单 bundle 大于 500 kB 的 warning，构建退出 0；代码拆分不是 P02 的框架兼容目标。

浏览器 Case 实际覆盖：

1. 未认证访问 `/dashboard` 重定向 `/login`，登录标题与提交按钮可见。
2. 虚构 Token + 拦截 API 后，`/dashboard`、`/chat`、`/events/analysis`、`/settings` 可达，现有 Sidebar/Main 渲染且无 `pageerror`。
3. 为文档根增加 `.dark` 后，现有“思考链路”头部计算背景为 `rgb(22, 27, 34)`，证明 class variant 未退回 media mode。

### 前端镜像

根 `.dockerignore` 排除 Git、`.runtime`、本地 Artifact / 配置和前端生成物，防止宿主机 `node_modules` 覆盖镜像内 `npm ci` 结果。使用真实仓库 context 构建 builder 和最终 nginx 镜像；镜像内 `npm ci`、TypeScript 和 Vite 构建均通过，audit 输出 0 vulnerabilities；builder 运行结果：

```text
$ docker run --rm sentinelops-p02-frontend-builder:local node --version
v24.19.0
```

两个明确命名的本地测试镜像已删除。没有启动 Compose、占用固定端口或触碰现有栈。

第一次使用真实 context 的重建在镜像内 `npm ci` 静默等待 601 秒后以 `read ETIMEDOUT` 退出，属于官方 registry 网络失败；该命令已按真实 `FAIL` 保留，没有归为依赖或构建 PASS。随后用本机既有回环代理和 Docker host network 对同一 Dockerfile、lockfile 与真实 context 重试，`npm ci` 在 12 秒完成、0 vulnerabilities，builder 与最终镜像均构建成功。最终镜像门禁以该成功重试和此前无代理 tar context 的成功构建共同证明。

## 执行记录

以下命令均在仓库根或 `web/` 执行：

```bash
npx --yes @playwright/test@1.62.1 test tests/smoke/current-ui.spec.ts --project=chromium
npm install --save-exact <P02 runtime matrix>
npm install --save-dev --save-exact <P02 toolchain and lint/smoke adapters>
npm uninstall autoprefixer postcss rollup
npm install --save-exact lucide-react@0.545.0
npm install --save-exact echarts@6.1.0
npm update flatted
npm ci
npx playwright install chromium
npm run lint
npm run build
npx playwright test tests/smoke/current-ui.spec.ts --project=chromium
npm_config_registry=https://registry.npmjs.org npm audit --audit-level=moderate
node --input-type=module <P02 exact package matrix check>
find / rg <lockfile, Vite config, tsbuildinfo, Tailwind/PostCSS truth checks>
docker build --network host --build-arg HTTPS_PROXY=<local-loopback-proxy> --file manifest/docker/Dockerfile.frontend <builder and final image> .
docker run --rm sentinelops-p02-frontend-builder:local node --version
docker image rm sentinelops-p02-frontend:local sentinelops-p02-frontend-builder:local
```

未运行完整前端 E2E、浏览器矩阵、完整后端 `go test -race ./...`、完整 Eval、故障矩阵、Hosted CI、Compose 或任何 P03 动作；按执行 Plan 留给对应单元或 P43。
