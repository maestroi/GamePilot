# Planner benchmarking

GamePilot benchmarks planners against a fixed replay-derived Tetris scenario instead of comparing separate live ROM runs directly.

## Why the benchmark uses a replay

Different planners can choose different rotations and columns, which changes controller input counts and frame timing. If future Tetris pieces depend on timing, two live runs can stop seeing the same piece stream even when they started from the same ROM boot. Comparing their final line count as if the workload were identical would therefore be misleading.

A benchmark scenario freezes:

- the replay's initial settled board
- initial lines, level, and score metadata
- the current piece followed by each recorded preview piece
- the number of moves used from the source replay

The benchmark does **not** reuse the source replay's recorded placements or later boards. Each planner starts from the same initial board and builds its own board through `SimulatePlacement` while consuming the same frozen piece sequence.

The scenario has a canonical SHA-256 hash so reports produced from the same scenario can be matched reliably.

## Planners

Supported benchmark planners are:

- `heuristic`: deterministic one-ply heuristic
- `lookahead`: deterministic two-ply preview-piece planner
- `llm`: the existing two-ply shortlist plus external model choice

The default benchmark compares `heuristic,lookahead`. Add `llm` explicitly when a model endpoint is available.

## Metrics

Each result records:

- pieces placed before the scenario ends or the planner tops out
- lines cleared during the simulated run
- final aggregate height
- final holes
- final bumpiness
- planner call count
- total planner nanoseconds
- maximum single-call planner nanoseconds
- exact placement trace
- final settled board

LLM results additionally record:

- total model attempts
- retry count
- total shortlisted candidates shown
- total strategically distinct candidates considered before shortlisting

CLI output shows average and maximum planning latency. For the LLM, this measures the model request/planning path only; emulator/controller execution is not part of benchmark latency.

## Deterministic benchmark

A replay such as `lookahead-25.json` can be used without loading the ROM:

```bash
go run ./cmd/gamepilot \
  -planner benchmark \
  -benchmark-replay lookahead-25.json \
  -pieces 25 \
  -benchmark-out benchmark-25.json
```

The default planners are `heuristic,lookahead`.

Example table shape:

```text
planner    pieces lines  topout  height holes   avg_plan   max_plan  retries  candidates
heuristic      25     8   false      21     1       ...        ...        0           -
lookahead      25     9   false      17     0       ...        ...        0           -
```

Exact values depend on the replay-derived piece scenario.

## Include the local LLM

Use the same replay and add `llm` to the planner list:

```bash
go run ./cmd/gamepilot \
  -planner benchmark \
  -benchmark-replay lookahead-25.json \
  -benchmark-planners heuristic,lookahead,llm \
  -pieces 25 \
  -llm-base-url http://localhost:8002/v1 \
  -llm-model 'qwen3.8-27b' \
  -llm-api-key-env '' \
  -llm-candidates 10 \
  -benchmark-out benchmark-all-25.json
```

The existing LLM defaults still apply: thinking is off, completion budget is 64 tokens, and shortlist size is 10 unless overridden.

## What the benchmark does not claim

The benchmark is an offline planning comparison, not a replacement for real-ROM replay validation.

It intentionally does not model exact Game Boy score progression, soft-drop score, level progression, controller timing, or the ROM's future RNG after the frozen sequence ends. Those remain emulator-truth concerns.

Use live runs plus replay verification to validate controller/emulator behavior. Use benchmark mode to compare planner quality and planner latency on the same board/piece workload.
