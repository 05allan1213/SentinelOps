# SentinelOps 二次开发执行规则

## 适用范围与执行依据

- 本文件只约束 SentinelOps 二次开发过程，用于避免重复性执行错误，不替代架构文档或实施计划。
- 当前用户的明确指令优先级最高；架构与安全契约以 `../Fo-Sentinel-Agent-secondary-development-plan.md` 为准，实施顺序、单元边界和证据要求以 `../Fo-Sentinel-Agent-secondary-development-implementation-plan.md` 为准。
- 按 P00～P43 顺序一次只执行一个实施单元，不跨单元顺手开发后续能力。
- 全程在 `main` 分支实施并逐单元本地提交；不得自动新建分支、修改 remote、push、rebase 或改写历史。
- 未经用户明确授权，不得修改上位 Spec、执行 Plan 或本文件。发现源码、锁定依赖或安全契约与计划冲突时，停止当前单元，记录证据和最小修正建议，等待确认。

## 开工边界与复用门禁

- 每个实施单元开始前重新检查当前分支、`git status --short`、相关源码、已有测试，以及锁定版本的 Eino / Eino-ext 官方 API。
- 编码前在当前单元既定的 `docs/implementation/evidence/Pxx-<slug>.md` 中记录 Boundary Audit：目标、明确非目标、兼容契约、安全不变量、预计修改的包或文件、验证方式和回滚方式。
- 新增包、框架层接口、Registry、运行时循环或跨层抽象前，必须完成 Build-or-Reuse 记录：现有代码能力、Eino / Eino-ext 官方能力、剩余业务缺口、最薄 Adapter 方案。
- 优先原位复用语义匹配的现有代码和官方能力。已有实现若违反安全不变量、官方生命周期或已冻结契约，可以收缩或替换，但必须提供源码证据。
- 官方或现有能力已经覆盖时，重复实现直接视为门禁失败。禁止创建第二套 Runtime、Store、Registry、Trace、Agent Loop、Tool Gateway、模型配置、RAG、MCP 协议栈或同构项目接口。
- 实际改动一旦超出已记录边界，立即停止扩张并重新审计；不得以顺手优化、清理旧代码或提高通用性为由扩大范围。

## 实现与代码风格

- 优先完成当前单元边界内的功能。单元测试推荐优先覆盖核心逻辑、Bug 回归、安全不变量、状态机和并发行为，但不强制先写失败测试。
- 维护所有受影响的已有测试；禁止通过删除测试、跳过用例或弱化断言使实现通过。
- Go 代码使用 `goimports` 格式化并整理 import，不使用 `gofmt` 作为本项目的格式化入口。
- 注释统一使用中文，技术名词、协议名和代码标识符保留英文。Go 包、导出类型、函数和方法采用 `// 标识符 中文说明`。
- 注释放在被说明的声明或代码块正上方；短字段说明可沿用所在文件已有的行尾布局。优先说明行为边界、安全不变量、并发原因、单位和非直观取舍，不逐行复述代码。
- 沿用所在文件已有的阶段分隔线、DAG 图、编号、标点和注释位置，不在同一文件引入另一套布局。不得为了统一注释或格式批量改写未触及的存量代码；生成文件和第三方代码保持原样。
- 不在实施阶段继续 brainstorm、扩展功能或打磨架构，不夹带无关重构、Prompt 调优、依赖升级或全仓格式化。

## 验证与证据

- P00～P42 只执行当前单元要求的局部门禁：对受影响 Go 包运行相关测试和 `go vet`，并覆盖当前单元的安全负向测试；具体范围以执行 Plan 为准。
- 修改前端的单元才运行前端验证，默认执行相关 lint、TypeScript / build 和当前单元测试；完整 Playwright E2E、浏览器矩阵、Eval 和故障矩阵留给对应单元与 P43。
- P43 是唯一最终全量验证单元。任何局部结果都不得宣称整个二改已经完成或整体通过。
- 计划规定了更严格门禁时执行更严格门禁。外部依赖、在线供应商、Hosted CI、共享数据库或发布动作未执行时，必须记录为 `NOT RUN` 并说明原因，不能写成 `PASS`。
- 所有验收结果只使用 `PASS`、`FAIL`、`NOT RUN` 或执行 Plan 已定义的阻塞状态。证据只记录真实执行的命令、结果和路径，禁止记录 Secret、完整 DSN、Authorization、Cookie、Token 或模型输入原文。
- 单元证据文件随当前单元提交，但不自引用该提交 SHA；提交完成后只在仓库外的执行 Plan 台账回填 SHA。

## Git 与提交规则

- 用户已有修改一律保留。只使用显式路径暂存当前单元文件，禁止无审查的 `git add .` 或 `git add -A`。
- 提交前依次检查 `git status --short`、`git diff --cached --check`、`git diff --cached --stat` 和完整的 `git diff --cached`，确认没有无关改动或后续单元内容。
- 无关改动与当前单元修改同一文件且无法安全拆分时，停止并说明；不得自行 reset、checkout、stash、覆盖或丢弃用户内容。
- 当前单元局部门禁通过后创建一个独立本地提交；失败或半成品状态不得提交。
- 提交信息使用英文 Conventional Commits：`<type>: <imperative English summary>` 或 `<type>(<scope>): <imperative English summary>`。scope 可选，不得为满足格式随意创造 scope。
- 允许的 type 为 `feat`、`fix`、`docs`、`test`、`refactor`、`perf`、`build`、`ci`、`chore`；使用英文半角冒号，标题不加句号。
- 提交主题和正文只描述实际结果，不得出现 `Task`、`Phase`、`P00`、`Pxx` 等流程步骤编号。
