# GamePilot

GamePilot is a game-agent runtime built on top of the public [`pkg/gomeboy`](https://github.com/maestroi/gomeboy/tree/main/pkg/gomeboy) API.

The first supported profile is **Game Boy Tetris, Rev 1**. The current vertical slice loads the exact ROM, boots Type A level 0 deterministically, decodes structured state from memory, executes verified game-level placements, supports one-ply heuristic, two-ply preview lookahead, and OpenAI-compatible LLM planners, records/verifies planner-independent replays, and benchmarks planners on fixed replay-derived scenarios without screenshots or vision.

## Current slice

```text
Gomeboy
  -> Tetris memory decoder
  -> structured Observation
  -> exact ROM-derived tetromino geometry
  -> deterministic legal placement simulations
       -> one-ply heuristic planner
       -> two-ply preview-piece planner
       -> top-N two-ply shortlist -> OpenAI-compatible LLM planner
  -> Placement{rotation, target_column}
  -> deterministic input controller
  -> next Observation
  -> canonical state hash + replay record

validated replay
  -> fixed initial board + piece sequence
  -> heuristic / lookahead / optional LLM benchmark
  -> comparable board quality + planner latency report
```

Implemented now:

- Gomeboy `v1.0.0` integration through a thin session wrapper
- exact Tetris Rev 1 SHA-256 gating
- deterministic Type A level-0 startup using frame stepping and button input
- 18x10 settled-board decoding from the ROM's WRAM playfield buffer
- active and preview tetromino decoding, including rotation
- score, level, lines, ready-state, and game-over decoding
- save/load checkpoint primitives delegated to Gomeboy
- exact Rev 1 tetromino geometry shared by controller and planners
- deterministic `Placement{Rotation, TargetColumn}` execution using verified rotate/shift inputs and soft drop
- lock/new-piece transition detection before returning control
- deterministic placement simulation with line clears and top-out detection
- a one-piece heuristic over aggregate height, completed lines, holes, and bumpiness
- deterministic two-ply lookahead using the known ROM preview piece
- deduplication of equivalent first-move board outcomes so symmetric raw rotations do not waste candidate slots
- a standalone `lookahead` planner mode for deterministic preview-aware play
- an OpenAI-compatible `/v1/chat/completions` client with configurable base URL/model/API-key environment variable
- fast LLM defaults for game control: Qwen/vLLM-style thinking disabled and a 64-token completion cap
- an LLM planner that sees only the best N unique two-ply candidates instead of every raw legal placement
- configurable LLM shortlist size via `-llm-candidates` (default 10)
- strict model-output decoding: exactly `rotation` and `target_column`, checked against the shortlisted legal candidate set before execution
- up to three retries for malformed or illegal model output
- versioned JSON replay records with ROM/profile/startup metadata
- canonical SHA-256 hashes of decoded Tetris state plus separately verified frame numbers
- fresh-boot replay verification that re-executes every recorded `Placement` through the strict controller
- replay-backed fixed-sequence planner benchmarking for heuristic, lookahead, and optional LLM planning
- benchmark metrics for survival, lines, final height/holes/bumpiness, planner latency, LLM retries, candidate compression, placement trace, and final board
- versioned benchmark JSON reports with a canonical scenario hash
- ROM-free tests for emulator-independent planning, lookahead, LLM HTTP compatibility, output validation/retries, replay hashing, benchmarking, serialization, and verification

Not implemented yet: MCP, deeper-than-preview search, or a provider-specific Structured Outputs adapter.

## Run

You must supply your own legally obtained Rev 1 ROM for live play. ROM files are intentionally ignored by git.

```bash
# Decode the first ready position.
go run ./cmd/gamepilot -rom /path/to/tetris.gb -planner observe

# Place the first piece. target column means the leftmost occupied board column.
go run ./cmd/gamepilot -rom /path/to/tetris.gb -planner place -rotation 1 -column 6

# One-ply deterministic heuristic.
go run ./cmd/gamepilot -rom /path/to/tetris.gb -planner heuristic -pieces 25

# Two-ply deterministic planner using the preview piece.
go run ./cmd/gamepilot \
  -rom /path/to/tetris.gb \
  -planner lookahead \
  -pieces 25 \
  -replay-out lookahead-25.json

# Fresh-boot the ROM and prove recorded placements reproduce the same states.
go run ./cmd/gamepilot \
  -rom /path/to/tetris.gb \
  -planner replay \
  -replay-in lookahead-25.json
```

### Planner benchmark

Benchmark mode uses a validated replay as a fixed initial board + piece-sequence scenario. It does **not** reuse the replay's recorded placements. Each planner builds its own board through deterministic simulation, so heuristic and lookahead see the same workload even if separate live runs would consume different frame counts.

Benchmark mode does not require the ROM:

```bash
go run ./cmd/gamepilot \
  -planner benchmark \
  -benchmark-replay lookahead-25.json \
  -pieces 25 \
  -benchmark-out benchmark-25.json
```

The default comparison is `heuristic,lookahead`. Add the local model to the same scenario with:

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

The summary reports pieces placed, lines cleared, top-out, final aggregate height/holes, average/max planner latency, LLM retries, and average candidate compression. The JSON report also retains each placement trace and final board. See [`docs/benchmark.md`](docs/benchmark.md) for the fairness model and limitations.

### Local/OpenAI-compatible LLM planner

The default LLM base URL is `http://localhost:1234/v1`. Supply the model identifier exposed by your server. Keyless local servers can disable the API-key environment lookup with `-llm-api-key-env ""`.

GamePilot defaults to `-llm-thinking off`, `-llm-max-tokens 64`, and `-llm-candidates 10`. The candidate list is no longer the full raw one-ply action set: GamePilot deduplicates equivalent first outcomes, evaluates each with the known preview piece, sorts them deterministically, and sends only the best N to the model.

```bash
go run ./cmd/gamepilot \
  -rom ./roms/tetris.gb \
  -planner llm \
  -pieces 5 \
  -llm-base-url http://localhost:8002/v1 \
  -llm-model '<model-id>' \
  -llm-api-key-env '' \
  -llm-candidates 10 \
  -replay-out llm-5.json
```

The startup line prints the effective settings:

```text
LLM: model=<model-id> base_url=http://localhost:8002/v1 thinking=off max_tokens=64 candidates=10
```

Move logs also show prompt compression, for example `candidates=10/34` means the model saw the best 10 of 34 strategically distinct first-move outcomes.

For Qwen3/vLLM-style endpoints, non-thinking mode sends `chat_template_kwargs.enable_thinking=false`. If a compatible server rejects the provider-specific thinking field, use `-llm-thinking auto` to omit it. `-llm-thinking on` explicitly enables it.

For a hosted compatible API, set the normal environment variables instead of putting secrets on the command line. `-llm-thinking auto` is safest when the provider does not implement Qwen/vLLM chat-template extensions.

The model never sends emulator inputs. GamePilot computes and ranks deterministic candidates, asks the model to choose one shortlisted current-piece placement, strictly validates the returned JSON, and only then passes the selected `Placement` to the same controller used by deterministic planner modes. The simulated preview reply is advisory only; the real ROM is observed again after every executed placement.

`-replay-out` works with `place`, `heuristic`, `lookahead`, and `llm`. Replay verification does not invoke any planner.

See [`docs/architecture.md`](docs/architecture.md), [`docs/benchmark.md`](docs/benchmark.md), [`docs/lookahead.md`](docs/lookahead.md), [`docs/llm-planner.md`](docs/llm-planner.md), [`docs/replay.md`](docs/replay.md), and [`docs/tetris-rev1.md`](docs/tetris-rev1.md).
