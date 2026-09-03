# GamePilot

GamePilot is a game-agent runtime built on top of the public [`pkg/gomeboy`](https://github.com/maestroi/gomeboy/tree/main/pkg/gomeboy) API.

The first supported profile is **Game Boy Tetris, Rev 1**. The current vertical slice loads the exact ROM, boots Type A level 0 deterministically, decodes structured state from memory, executes verified game-level placements, supports deterministic heuristic and OpenAI-compatible LLM planners, and records/verifies planner-independent replays without screenshots or vision.

## Current slice

```text
Gomeboy
  -> Tetris memory decoder
  -> structured Observation
  -> exact ROM-derived tetromino geometry
  -> deterministic legal placement simulations
       -> heuristic planner
       -> OpenAI-compatible LLM planner
  -> Placement{rotation, target_column}
  -> deterministic input controller
  -> next Observation
  -> canonical state hash + replay record
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
- repeated heuristic placement execution with a configurable move limit
- an OpenAI-compatible `/v1/chat/completions` client with configurable base URL/model/API-key environment variable
- fast LLM defaults for game control: Qwen/vLLM-style thinking disabled and a 64-token completion cap
- an LLM planner that sees the board, current/next pieces, score/lines/level, and deterministic legal candidate metrics
- strict model-output decoding: exactly `rotation` and `target_column`, checked against the legal candidate set before execution
- up to three retries for malformed or illegal model output
- versioned JSON replay records with ROM/profile/startup metadata
- canonical SHA-256 hashes of decoded Tetris state plus separately verified frame numbers
- fresh-boot replay verification that re-executes every recorded `Placement` through the strict controller
- ROM-free tests for emulator-independent planning, LLM HTTP compatibility, output validation/retries, replay hashing, serialization, and verification

Not implemented yet: MCP, deeper lookahead/search, or a provider-specific Structured Outputs adapter.

## Run

You must supply your own legally obtained Rev 1 ROM. ROM files are intentionally ignored by git.

```bash
# Decode the first ready position.
go run ./cmd/gamepilot -rom /path/to/tetris.gb -planner observe

# Place the first piece. target column means the leftmost occupied board column.
go run ./cmd/gamepilot -rom /path/to/tetris.gb -planner place -rotation 1 -column 6

# Let the deterministic heuristic choose and execute 25 placements.
go run ./cmd/gamepilot -rom /path/to/tetris.gb -planner heuristic -pieces 25

# Record a heuristic run as a deterministic replay.
go run ./cmd/gamepilot \
  -rom /path/to/tetris.gb \
  -planner heuristic \
  -pieces 25 \
  -replay-out tetris-25.json

# Fresh-boot the ROM and prove the recorded placements reproduce the same states.
go run ./cmd/gamepilot \
  -rom /path/to/tetris.gb \
  -planner replay \
  -replay-in tetris-25.json
```

### Local/OpenAI-compatible LLM planner

The default LLM base URL is `http://localhost:1234/v1`, which is convenient for a local OpenAI-compatible server. Supply the model identifier exposed by your server. Keyless local servers can disable the API-key environment lookup with `-llm-api-key-env ""`.

GamePilot defaults to `-llm-thinking off` and `-llm-max-tokens 64`. For Qwen3/vLLM-style endpoints, non-thinking mode sends `chat_template_kwargs.enable_thinking=false`, which is much better suited to the low-latency `Observation -> Placement` loop than long reasoning traces.

```bash
go run ./cmd/gamepilot \
  -rom ./roms/tetris.gb \
  -planner llm \
  -pieces 5 \
  -llm-base-url http://localhost:8002/v1 \
  -llm-model '<model-id>' \
  -llm-api-key-env '' \
  -replay-out llm-5.json
```

The startup line prints the effective settings, for example:

```text
LLM: model=<model-id> base_url=http://localhost:8002/v1 thinking=off max_tokens=64
```

If a compatible server rejects the provider-specific thinking field, use `-llm-thinking auto` to omit it. `-llm-thinking on` explicitly enables it.

For another local server, change only the compatible base URL and model. For example, an OpenAI-compatible server on port `11434` can use `http://localhost:11434/v1`.

For a hosted compatible API, set the normal environment variables instead of putting secrets on the command line. `-llm-thinking auto` is safest when the provider does not implement Qwen/vLLM chat-template extensions:

```bash
export OPENAI_BASE_URL='https://api.openai.com/v1'
export OPENAI_MODEL='<model-id>'
export OPENAI_API_KEY='<secret>'

go run ./cmd/gamepilot \
  -rom ./roms/tetris.gb \
  -planner llm \
  -pieces 5 \
  -llm-thinking auto \
  -replay-out llm-5.json
```

The model never sends emulator inputs. GamePilot first computes the deterministic non-top-out candidate set, asks the model to choose one candidate, strictly validates the returned JSON, and only then passes the selected `Placement` to the same controller used by the heuristic and manual modes. LLM runs can therefore be replayed later without contacting the model again.

`-replay-out` works with `-planner place`, `heuristic`, and `llm`. Replay verification does not invoke any planner: it reboots the ROM, replays the recorded actions through the verified controller, and stops at the first state or timing mismatch.

See [`docs/architecture.md`](docs/architecture.md), [`docs/llm-planner.md`](docs/llm-planner.md), [`docs/replay.md`](docs/replay.md), and [`docs/tetris-rev1.md`](docs/tetris-rev1.md).
