# 实施证据协议

本目录保存 P00～P43 的简明、可复核实施证据。每个实施单元只记录本单元实际执行的命令、结果、边界和偏差；P00～P42 的局部结果不得表述为整个二次改造已经通过。

原始长日志、截图、Trace、数据库 dump 和 Eval 输出放在仓库已忽略的 `.artifacts/implementation/Pxx/`，简明证据只引用其相对路径。CI 运行时可以改为引用对应 Artifact。

## 状态词

- `PASS`：该项要求已经由本次记录的直接证据证明。
- `FAIL`：命令或必需断言失败，且尚未修复；已知基线失败也必须原样标注。
- `NOT RUN`：因授权、凭证、基础设施或其他明确外部条件未执行，不能视为通过。
- `BLOCKED`：命中实施计划停止条件，需要新的事实、权限或 Spec 决策。

不得使用“基本通过”“应该正常”等模糊表述。过滤测试必须确认预期 Case 确实执行；`[no tests to run]`、0 tests 或空 Dataset 不得记为 `PASS`。用于证明禁止项不存在的 `rg` 返回 1 时，应同时记录查询范围，区分预期空结果与命令错误。

## 单元证据模板

```markdown
# Pxx <标题>

- Status:
- Started from:
- Spec references:
- Actual files:
- Red test and expected failure:
- Local commands:
- Results: PASS / FAIL / NOT RUN
- Key assertions:
- Deviations from recommended route:
- Raw artifact references:
- Unfinished items:

## Boundary Audit

- 目标：
- 明确非目标：
- 兼容契约：
- 安全不变量：
- 预计文件：
- 验证方式：
- 回滚方式：

## Build-or-Reuse

- 现有代码能力：
- Eino / Eino-ext 官方能力：
- 剩余业务缺口：
- 最薄 Adapter：
```

## 记录规则

- 命令记录包含工作目录、关键参数、退出状态和实际执行的 Case；必要时注明工具版本与执行时间。
- 单元局部门禁、已知失败、外部 `NOT RUN` 分开记录，不能用一个绿色命令覆盖另一项缺失证据。
- 只暂存当前单元的显式文件；提交前检查 cached diff，提交后由仓库外执行台账记录 Commit SHA。
- 证据中禁止出现 Secret、完整 DSN、Authorization、Cookie、Token、模型输入原文或真实业务数据。
- 配置证据只记录路径、选择规则、引用关系和脱敏元数据，不读取、打印或复制 `config.local.yaml` 的值。
- Schema 证据只允许来自明确授权的一次性测试库、脱敏 schema-only 副本或版本化 contract fixture；没有授权时必须记 `NOT RUN`。
- 在线供应商测试只有在显式设置对应 Gate 时运行，并与完整 Eval 分列。
