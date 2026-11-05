# Norm

[English](README.md) · 简体中文

[![ci](https://github.com/yanmxa/norm/actions/workflows/ci.yml/badge.svg)](https://github.com/yanmxa/norm/actions/workflows/ci.yml)
[![license](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

**面向人工编写和 AI 生成代码的量化质量检查工具。**

测试通过、覆盖率高，并不代表代码质量好。新代码仍可能重复已有实现，
让函数越来越难维护，或依赖「执行了代码却没有检查结果」的测试。
Norm 用指标、代码位置和可操作的发现，帮助开发者识别这些问题。

Norm 是原生 CLI，通过源码解析、静态规则和实际运行测试完成评估。
**不需要 LLM 或模型 API key。** 可用于本地开发、检出后的 PR 和 CI。
名字借自数学中的 *norm（范数）*，取度量之意，也呼应规范与标准。

## 检查什么

| 维度 | 原理 | 价值与详情 |
| --- | --- | --- |
| 冗余与结构 | 将函数体和静态可解析的调用关系与仓库已有代码对照。 | 复用已有实现，遵循统一入口。[冗余规则](docs/metrics.zh.md#redundant-code冗余代码) · [一致性规则](docs/metrics.zh.md#inconsistent-code不一致代码) |
| 认知复杂度 | 计算分支、嵌套，以及相对基线版本的复杂度变化。 | 保持函数易读，控制维护成本。[规则与样例](docs/metrics.zh.md#cognitive-complexity认知复杂度) |
| 测试有效性 | 每次变异一处业务代码，保持测试不变，检查测试能否发现变化。 | 找到覆盖率无法揭示的边界用例缺失和薄弱断言。[变异得分](docs/metrics.zh.md#mutation-score变异得分) · [锐度与触及率](docs/metrics.zh.md#test-strength--reach测试锐度--触及率) |

指标是审查证据，不是业务正确性的证明。存活变异也可能与原代码行为等价，
补测试前需要复核。[完整定义与样例](docs/metrics.zh.md)说明了各项指标的边界。

## 安装

源码构建需要 **Go 1.26+、C 编译器和 CGO**，用于编译内置的 Tree-sitter 解析器：

```bash
CGO_ENABLED=1 go install github.com/yanmxa/norm/cmd/norm@main
```

仓库已更名为 Norm，但尚未发布 Norm 版本；`v0.1.0` 仍包含旧的 `metron` 产物。
过渡期间请使用 `@main`，而不是 `@latest`；也可以在源码目录中构建：

```bash
CGO_ENABLED=1 go build -o bin/norm ./cmd/norm
```

构建后的原生二进制进行 graph 分析时，不需要外部索引工具或 Node 运行时。
变异测试还需要 Go 工具链，以执行目标项目的测试。

## 使用

分析 PR 时，先检出 PR 分支、获取基线分支，并确保工作区干净。
在目标 git 仓库中运行：

```bash
git fetch origin main
norm --since origin/main                         # 默认：complexity + graph
norm --since origin/main --axes all              # 加上变异测试
norm --since origin/main --axes all --format json # 机器可读结果
norm --all                                      # 检查整个仓库，而不是 diff
```

`--since` 与 merge base 比较。`--all` 没有基线，因此复杂度增量和依赖新增边的
检查会显示未测，而不是通过。

可选：运行 `norm init`，根据仓库现状生成 `norm.json` 中的阈值。
将配置作为团队质量门槛前，请先审查并提交。
Graph 索引自动建立并缓存到 `.norm/index.json`；变异结果缓存于 `.norm/`，
支持中断后续跑。使用 `--fresh` 忽略变异测试缓存，重新测量。

## 当前支持范围

- **Graph 分析：** Go、JavaScript、TypeScript、JSX、TSX、Python；不同语言启用的规则不同。
- **仅 Go：** 认知复杂度、变异测试、孤立符号检测、context/error 惯例检查。
- **保守解析：** 不猜测动态或有歧义的调用；结构相似只是复用候选，不是重复的证明。
- **函数级范围：** 检查覆盖改动涉及的整个函数，因此可能发现历史问题，而不全是本次 PR 引入的问题。

## CI 与 AI 助手

终端报告面向审查者；`--format json` 输出供自动化使用的发现与诊断信息。
默认按各项主指标独立判断是否达标；`--fail-on` 可选 `headline`、`any`、`none`。
CRAP 用于排序高风险函数，不参与门槛判断。

| 退出码 | 含义 |
| --- | --- |
| `0` | 已测指标没有违反所选门槛策略。 |
| `1` | 执行出错。 |
| `2` | 已测指标在所选策略下超出范围。 |
| `3` | 运行不完整，证据不足，包括预算受限的情况。 |

不完整状态优先于阈值失败，即使设置 `--fail-on none` 也返回 `3`。
**不要把 `3` 或未测指标当作通过**；CI 应显式处理这些情况。

参考 [agent 指令](agent/norm.md)，建立「评估 → 改进 → 再评估」的循环。
应改进代码和测试，而不是放宽阈值或删除测试来过关。
执行、缓存及可靠性细节见[变异测试设计](docs/mutation-design.md)（英文）。

## 参与贡献

欢迎提交 issue 和 pull request，详见 [CONTRIBUTING.md](CONTRIBUTING.md)。

```bash
CGO_ENABLED=1 go test ./...
```

## 许可证

[MIT](LICENSE)。
