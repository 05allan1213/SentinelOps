# P06 Policy 基元、Canonical JSON 与统一脱敏

- Status: `PASS`
- Started from: `3379a5050e5f4fb0f294345f27f09b47a8bae451`；`main`；开工时工作树干净
- Spec references: 上位 Spec `6.6`、Task 4、Task 9；执行 Plan `P06`、`2.1～2.7`
- Results: P06 Red 测试、实现、计划两条局部门禁、同包回归、`go vet`、race、短时 fuzz、格式与安全负向扫描均 `PASS`
- Raw artifact references: 无；命令与摘要直接记录于本文件，不记录 Secret、DSN、Token 或模型输入
- Unfinished items: P07 及后续单元未实施；P43 全量门禁未执行

## Boundary Audit

- 目标：定义 L0/L1/L2 风险等级和不可变 Proposal；实现 `fo/proposal/v1`、`fo/approval/v1`、`fo/effect/v1` 域分隔身份；提供 map 顺序、数字、时间和空值表示稳定的 Canonical JSON；提供日志、Event、Trace、Approval、Effect、Langfuse 和 Eval 后续统一复用的 Redactor；在 Hash 前拒绝明文 Secret，仅让合法 Secret 引用参与身份计算。
- 明确非目标：不实现 Approval Store、HITL、Effect Ledger、Mutation Catalog、RuntimeHandler、Langfuse 接线或 Eval 接线；不启用 Agent Mutation；不修改 Schema、配置、依赖或 P07 及后续单元。
- 兼容契约：P05 的 Identity/RBAC API 和行为保持不变；Hash 严格使用上位 Spec `6.6` 的字段、域分隔符和 NUL 分隔；Secret 引用继续复用 P04 `internal/config.SecretRef` 的 `env:`/`file:` 校验，不解析 Secret；展示用脱敏 JSON 与 Hash 输入分离。
- 安全不变量：出现明文凭证、Authorization/Cookie、DSN、PEM 私钥或常见云/API Key 时 Proposal fail-closed；Canonical JSON 不接受非 JSON 值、非有限浮点数、重复对象键或非字符串 map key；Redactor 对敏感键和文本模式统一处理；Hash 不使用 tool_call_id、随机数或运行时 Secret。
- 预计文件：`internal/ai/policy/levels.go`、`canonical.go`、`hash.go`、`redact.go` 及同包单元/fuzz 测试；本证据文件；仓库外执行 Plan 台账。
- 验证方式：先运行新增 P06 测试并保存预期编译失败；实现后运行计划两条局部命令、同包未过滤测试、`go vet`、格式检查和“无第二套脱敏器/无后续能力”负向扫描；不运行全仓门禁。
- 回滚方式：通过本单元独立本地提交的普通反向提交恢复；不执行数据库 Down，不修改 remote，不 push。

## Build-or-Reuse

- 现有代码能力：复用 P04 的 `config.SecretRef.Validate`，复用 P05 已有 `internal/ai/policy` 包；仓库中不存在 Canonical JSON、Proposal identity 或统一 Redactor，现有零散 `json.Marshal` 和测试内 DSN 字符串替换不能覆盖本单元安全契约。
- 官方/标准能力：复用 Go 标准库 `encoding/json` 做 JSON 类型/标签边界，复用 `crypto/sha256` 与 `encoding/hex` 做稳定 Hash；锁定的 Eino/Eino-ext 没有替代业务 Proposal identity 和跨日志/持久化后端统一脱敏的 API。本单元不需要新增依赖或 Eino Adapter。
- 剩余业务缺口：标准 `encoding/json` 虽稳定排序字符串 map key，但不会把 `1`、`1.0`、`1e0` 和等价时区统一为项目身份格式，也不会拒绝 Proposal 明文 Secret；现有代码没有域分隔 golden identity 或统一结构化/文本脱敏入口。
- 最薄实现：在现有 policy 包提供纯函数 Canonical/Hash 基元、只保存冻结 canonical bytes/hash 的不可变 Proposal，以及一个无状态并发安全 Redactor；所有后续消费者只能注入/调用这一实现，本单元不接线后续系统。

## Red evidence

~~~bash
go test ./internal/ai/policy \
  -run 'Fuzz|TestCanonical|TestProposal|TestRedact|TestFrozen|TestRisk' \
  -count=1
~~~

- `PASS`（预期 Red）：先加入命名测试后执行，包按预期编译失败；编译器只报告 `CanonicalJSON`、`Proposal`、`RiskL2`、`ProposalHash`、`ApprovalID`、`EffectKey` 等 P06 生产符号不存在。该失败证明测试不是在已有实现上直接变绿，且没有环境、依赖或 0 Case 干扰。
- Golden vector 在实现前按上位 Spec `6.6` 的精确 canonical JSON、域分隔符和 NUL 分隔，使用独立 `printf ... | sha256sum` 计算：proposal `82d1c4aa...775d5`、approval `bd3373f3...99af4`、effect `91f03283...bddc`；测试保存完整 64 位值。

## Local commands and results

### P06 计划局部门禁

~~~bash
goimports -w internal/ai/policy/levels.go internal/ai/policy/canonical.go \
  internal/ai/policy/hash.go internal/ai/policy/redact.go \
  internal/ai/policy/canonical_test.go internal/ai/policy/hash_test.go \
  internal/ai/policy/redact_test.go

go test ./internal/ai/policy -count=1
go test ./internal/ai/policy \
  -run 'Fuzz|TestCanonical|TestProposal|TestRedact' -count=1 -v
~~~

- `PASS`：同包未过滤测试全部通过，包含 P05 `authorize` 回归与 P06 新增测试。
- `PASS`：计划过滤命令的 `-v` 输出明确执行 Canonical map/number/time/empty、不支持值 fail-closed、Proposal/Approval/Effect golden、明文 Secret 拒绝、统一 Redactor、幂等和 3 个 fuzz seed；不存在 `[no tests to run]` 或 0 Case。
- 首轮绿测曾真实失败：MCP env 的 `AWS_SECRET_ACCESS_KEY` 未被敏感键规则覆盖；只扩充统一 `isSensitiveKey` 的 `_access_key`/`_secret_` 规则后重跑转绿，未增加旁路脱敏器。

### 直接受影响回归、静态检查与 fuzz

~~~bash
go vet ./internal/ai/policy
go test -race ./internal/ai/policy -count=1
go test ./internal/ai/policy -run '^$' \
  -fuzz '^FuzzCanonicalJSONStable$' -fuzztime=3s
~~~

- `PASS`：`go vet` 无输出。
- `PASS`：同包 race 测试通过；Redactor 与 Hash/Canonical 基元不保存请求级可变状态。
- `PASS`：短时 fuzz 完成 15,491 次执行，发现并保留 53 个新 interesting inputs，总 corpus 56，未崩溃；fuzz cache 未加入工作树。

### 格式、范围与禁止项扫描

~~~bash
git status --short
git diff --check

rg -n --glob '*.go' \
  -e 'type .*Redactor' -e 'func .*Redact' -e 'MaskFunc' \
  -e 'sanitize|Sanitize' internal utility

rg -n --glob '*.go' \
  -e 'StatefulInterrupt' -e 'Approval(Store|ID)?' \
  -e 'Effect(Ledger|Key)?' -e 'RuntimeHandler' \
  internal/ai/policy
~~~

- `PASS`：工作树仅有 P06 预计的 policy 源码/测试和本证据；`go.mod`、`go.sum`、Schema、配置及 P07+ 文件未修改。
- `PASS`：显式暂存 8 个 P06 文件后，`git diff --cached --check` 无输出；cached stat 为 8 files changed、1,047 insertions，完整 cached diff 已审查。
- `PASS`：生产代码只存在 `internal/ai/policy.Redactor` 这一套脱敏器；`trace/usage.go` 的 `sanitizeUsage` 只规范 token 数值，不处理或保存 Secret，不是 Langfuse/Eval 脱敏器。
- `PASS`：policy 包只定义本单元要求的稳定 `ApprovalID`/`EffectKey` 纯 Hash；不存在 StatefulInterrupt、Approval Store、Effect Ledger 或 RuntimeHandler，未实施后续单元。

## Key assertions

- `RiskLevel.Validate` 只接受 L0/L1/L2，未知值 fail-closed。
- Canonical JSON 对字符串 map key 排序，将 `1`/`1.0`/`1e0` 统一为 `1`，将 RFC3339 等价时区统一为 UTC RFC3339Nano，并区分 null、空数组、空对象和空字符串；非有限浮点、非字符串 map key、重复 JSON object key 和过大数字拒绝。
- `FrozenProposal` 只保存复制后的 canonical bytes 和 Hash，调用方后续修改输入 map 或返回 byte slice 都不能改变已冻结 identity。
- proposal/approval/effect identity 严格使用 `fo/*/v1` 域和上位 Spec NUL 分隔；不包含 tool_call_id、随机数、Attempt 或解析后的 Secret。
- Proposal Hash 前遍历规范 JSON 参数：敏感键只接受复用 P04 校验的 `env:`/`file:` 引用，PEM、Bearer/Basic、DSN 和常见云/API Key 文本 fail-closed；不同 Secret 引用产生不同 Hash。
- 同一无状态 Redactor 同时处理结构化值和文本；Authorization、Cookie、API key、PEM、DSN、SMTP 与 MCP header/env 输出统一占位符。展示用 redacted JSON 对不同引用相同，而 Hash 仍不同，证明两条数据路径分离。

## Deviations from recommended route

- 无文件或架构偏差；实现落在计划指定的 `levels.go`、`canonical.go`、`hash.go`、`redact.go`。
- 为让 canonical 语义可验证，测试名比计划示例更具体，并增加 `TestFrozenProposalIsImmutable`、未知风险 fail-closed、同包 race 和 3 秒 fuzz；计划两条原命令仍原样通过。
- Canonical JSON 没有引入第三方 JCS 包：上位 Spec 只要求项目内稳定排序及明确数字/时间格式，标准库加最薄规范化足以覆盖，避免新增依赖和重复抽象。

## Final scope statement

P06 局部门禁为 `PASS`。本结论只覆盖 Policy 风险/Proposal 基元、Canonical JSON、稳定 identity 与统一 Redactor；不代表整个二次开发完成。Approval/HITL/Effect/Runtime、Langfuse/Eval 接线、P07 及后续实现、全仓门禁、Hosted CI、共享数据库、发布与生产动作均 `NOT RUN`。
