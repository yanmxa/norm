# Norm

English · [简体中文](README.zh.md)

[![ci](https://github.com/yanmxa/norm/actions/workflows/ci.yml/badge.svg)](https://github.com/yanmxa/norm/actions/workflows/ci.yml)
[![license](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

**Quantitative code-quality checks for human- and AI-written code.**

Passing tests and high coverage do not prove code quality. New code can still
repeat an existing implementation, make a function harder to maintain, or rely on
tests that execute behavior without checking it. Norm surfaces these problems
with measurements, code locations, and actionable findings.

Norm is a native CLI using source parsing, static rules, and actual test execution.
**No LLM or model API key required.** Run it locally, on a checked-out PR, or in CI.
The name borrows *norm* from mathematics: magnitude and measurement, with an echo of standards.

## What it checks

| Area | Principle | Benefit and details |
| --- | --- | --- |
| Redundancy and structure | Compare function bodies and statically resolved call relationships with existing code. | Reuse implementations and respect established entry points. [Redundancy](docs/metrics.md#redundant-code) · [Consistency](docs/metrics.md#inconsistent-code) |
| Cognitive complexity | Measure branches, nesting, and complexity changes against a base revision. | Keep functions understandable and maintenance costs under control. [Rules and examples](docs/metrics.md#cognitive-complexity) |
| Test effectiveness | Mutate production code one change at a time, keeping tests unchanged, and see whether they detect it. | Find missing boundary cases and weak assertions, beyond coverage. [Mutation score](docs/metrics.md#mutation-score) · [Strength and reach](docs/metrics.md#test-strength-and-reach) |

Metrics are evidence for review, not proof of business correctness. Surviving
mutants may be behaviorally equivalent; review them before adding tests.
[Full definitions and worked examples](docs/metrics.md) explain the limits.

## Install

Source builds require **Go 1.26+, a C compiler, and CGO** for the bundled Tree-sitter parsers:

```bash
CGO_ENABLED=1 go install github.com/yanmxa/norm/cmd/norm@main
```

The repository has been renamed to Norm, but no Norm release is published yet;
`v0.1.0` still contains legacy `metron` assets. Use `@main` rather than `@latest`
during this transition, or build from a checkout:

```bash
CGO_ENABLED=1 go build -o bin/norm ./cmd/norm
```

The resulting native binary needs no separate indexer or Node runtime for graph
analysis. Mutation testing also needs the Go toolchain to run the target project's tests.

## Usage

For a PR, check out its branch, fetch the base branch, and start with a clean
working tree. Run inside the target git repository:

```bash
git fetch origin main
norm --since origin/main                         # complexity + graph (default)
norm --since origin/main --axes all              # also run mutation tests
norm --since origin/main --axes all --format json # machine-readable results
norm --all                                      # whole repository, not a diff
```

`--since` compares against the merge base. `--all` has no base, so complexity
delta and checks requiring new dependency edges are unmeasured, not passes.

Optionally run `norm init` to write repository-calibrated thresholds to `norm.json`.
Review and commit that configuration before using it as a shared quality gate.
Graph indexing is automatic and cached in `.norm/index.json`; mutation results
are cached under `.norm/` for resumable runs. Use `--fresh` to ignore mutation checkpoints.

## Current scope

- **Graph analysis:** Go, JavaScript, TypeScript, JSX, TSX, and Python; rules vary by language.
- **Go only:** cognitive complexity, mutation testing, orphan detection, and context/error conventions.
- **Conservative resolution:** dynamic or ambiguous calls are not guessed; similarity is a reuse candidate, not proof of duplication.
- **Function-level scope:** checks cover whole touched functions, so findings can include legacy issues rather than only problems introduced by a PR.

## CI and AI assistants

Terminal reports help reviewers; `--format json` provides findings and diagnostics
for automation. Headline readings gate independently by default; `--fail-on`
selects `headline`, `any`, or `none`. CRAP ranks risky functions but does not gate.

| Exit | Meaning |
| --- | --- |
| `0` | No measured reading violates the selected gate policy. |
| `1` | Execution error. |
| `2` | A measured reading is out of range under the selected policy. |
| `3` | Partial run; evidence is incomplete, including budget-limited runs. |

Partial status takes precedence over threshold failures, even with `--fail-on none`.
**Do not treat `3` or unmeasured readings as a pass**; CI should handle them explicitly.

Use the [agent instructions](agent/norm.md) for an evaluate → improve → re-evaluate
loop. Fix code and tests rather than relaxing thresholds or deleting tests to pass.
See the [mutation design](docs/mutation-design.md) for execution, caching, and reliability details.

## Contributing

Issues and pull requests are welcome. See [CONTRIBUTING.md](CONTRIBUTING.md).

```bash
CGO_ENABLED=1 go test ./...
```

## License

[MIT](LICENSE).
