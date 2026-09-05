# GamePilot

GamePilot is a game-agent runtime for Game Boy titles. It sits on top of the public [`pkg/gomeboy`](https://github.com/maestroi/gomeboy/tree/main/pkg/gomeboy) emulator API: the emulator is the source of truth, and GamePilot observes memory, chooses a move, and executes it through a deterministic controller.

The first supported game is **Game Boy Tetris, Rev 1**. GamePilot boots Type A level 0, reads the playfield and pieces from WRAM, and plays by placing tetrominoes — not by sending ad-hoc button sequences.

## What it is used for

- **Autonomous play.** Heuristic, two-ply lookahead, and OpenAI-compatible LLM planners all emit the same `Placement{Rotation, TargetColumn}` and share one controller.
- **Reproducible runs.** Placements and decoded state are recorded as versioned JSON replays and can be verified from a fresh ROM boot.
- **Planner comparison.** A validated replay becomes a fixed piece sequence so heuristic, lookahead, and LLM planners can be scored on the same workload.
- **Observable sessions.** A long-lived session manager hosts play inside a server process, publishes copied snapshots and PNG frames, and exposes a private authenticated operator API plus an embedded browser console.

GamePilot is a Go library, CLI, and private operator console. The public spectator is not included yet.

## Requirements

- [Go](https://go.dev/dl/) 1.26 or later
- A legally obtained **Tetris (World) Rev 1** ROM (`.gb`). ROM files are gitignored; GamePilot checks the exact SHA-256 and refuses other dumps.

Optional, for the LLM planner:

- An OpenAI-compatible Chat Completions server (local or remote)
- `OPENAI_MODEL`, and `OPENAI_API_KEY` unless the server is keyless

## Install

```bash
git clone https://github.com/maestroi/GamePilot.git
cd GamePilot
go test ./...
```

Place the ROM somewhere local, for example `./roms/tetris.gb`.

## Run

All live play commands need `-rom`. Target column is the leftmost occupied board column of the placed piece.

```bash
# Decode the first ready position.
go run ./cmd/gamepilot -rom ./roms/tetris.gb -planner observe

# Place one piece by hand.
go run ./cmd/gamepilot -rom ./roms/tetris.gb -planner place -rotation 1 -column 6

# One-ply deterministic heuristic.
go run ./cmd/gamepilot -rom ./roms/tetris.gb -planner heuristic -pieces 25

# Two-ply planner using the ROM preview piece; write a replay.
go run ./cmd/gamepilot \
  -rom ./roms/tetris.gb \
  -planner lookahead \
  -pieces 25 \
  -replay-out lookahead-25.json

# Fresh-boot the ROM and prove the recorded placements reproduce the same states.
go run ./cmd/gamepilot \
  -rom ./roms/tetris.gb \
  -planner replay \
  -replay-in lookahead-25.json
```

### LLM planner

The model never presses buttons. GamePilot ranks legal two-ply candidates, asks the model to pick one shortlisted placement, validates the JSON, then runs the same controller as the deterministic planners.

Default base URL is `http://localhost:1234/v1`. Keyless local servers can skip the API-key lookup with `-llm-api-key-env ""`.

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

Defaults: `-llm-thinking off`, `-llm-max-tokens 64`, `-llm-candidates 10`. Use `-llm-thinking auto` if the server rejects the Qwen/vLLM `enable_thinking` field.

`-replay-out` works with `place`, `heuristic`, `lookahead`, and `llm`. Replay verification does not call a planner.

### Planner benchmark

Benchmark mode does not need the ROM. It takes a validated replay as a frozen initial board and piece sequence, then lets each planner build its own board through deterministic simulation.

```bash
go run ./cmd/gamepilot \
  -planner benchmark \
  -benchmark-replay lookahead-25.json \
  -pieces 25 \
  -benchmark-out benchmark-25.json
```

The default comparison is `heuristic,lookahead`. Include an LLM on the same scenario with `-benchmark-planners heuristic,lookahead,llm` and the usual `-llm-*` flags.

The report covers pieces placed, lines cleared, top-out, aggregate height/holes, planner latency, LLM retries, and candidate compression. See [docs/benchmark.md](docs/benchmark.md).

## CLI modes

| `-planner` | What it does |
| --- | --- |
| `observe` | Boot Type A level 0 and print the first ready observation |
| `place` | Execute one explicit rotation and target column |
| `heuristic` | One-ply height / holes / bumpiness planner |
| `lookahead` | Two-ply search using the known preview piece |
| `llm` | Shortlist of two-ply candidates sent to an OpenAI-compatible model |
| `replay` | Re-execute a recorded replay from a fresh boot |
| `benchmark` | Compare planners on a replay-derived piece sequence |

## Live sessions

The long-lived runtime is a Go package, not a second emulator. One goroutine owns each Gomeboy instance. Readers only see copied snapshots and the latest encoded 160×144 PNG.

```go
manager := sessions.NewTetrisManager(nil)
id, err := manager.Start(sessions.LaunchConfig{
    ROMPath:      "./roms/tetris.gb",
    Profile:      tetris.ProfileID,
    Planner:      "lookahead",
    MoveLimit:    25,
    RecordReplay: true,
    Pacing:       sessions.PacingRealtime,
})
```

- `PacingFast` — unthrottled, for tests and batch runs
- `PacingRealtime` — wall-clock delay near native DMG cadence (~59.7 Hz); emulator frames and input boundaries stay the same

```text
Manager.Snapshot(id)
Manager.Frame(id)
Manager.List()
Manager.Stop(id)
Manager.Wait(ctx, id)
Manager.Replay(id)
Manager.Delete(id)  # terminal history only
```

Read-only HTTP:

```text
GET /v1/sessions/{id}
GET /v1/sessions/{id}/frame
```

Details: [docs/live-sessions.md](docs/live-sessions.md).

## Private operator API

`runtime/operatorapi` is the authenticated control plane for the embedded operator console. Every mounted route requires a deployment-configured Bearer token. If the token is empty, no operator routes are mounted.

The browser only sees server-configured aliases for ROMs, profiles, planners, and models. Raw filesystem paths and provider secrets are not request fields.

```text
GET    /v1/config
GET    /v1/sessions
GET    /v1/sessions/{id}
GET    /v1/sessions/{id}/frame
POST   /v1/sessions
POST   /v1/sessions/{id}/stop
DELETE /v1/sessions/{id}
```

`operatorapi.NewHandlerWithReplay` adds authenticated `GET /v1/sessions/{id}/replay` for retained replay bytes. `NewHandler` keeps the original route set when artifact download should stay unmounted.

Launch on this surface defaults to `realtime` pacing. Delete only removes terminal retained history; stop an active session first.

Details: [docs/operator-api.md](docs/operator-api.md).

## Private operator console

`runtime/operatorconsole` embeds the HTML/CSS/JavaScript shell and delegates every `/v1/*` request to the private API on the same origin. The operator enters the deployment token in the browser; it stays in `sessionStorage` and is never baked into the assets.

```go
api, err := operatorapi.NewHandlerWithReplay(operatorOptions)
if err != nil {
    return err
}
handler, err := operatorconsole.NewHandler(api)
if err != nil {
    return err
}
return http.ListenAndServe("127.0.0.1:8080", handler)
```

Keep this listener private (for example loopback plus a VPN or reverse proxy). Details: [docs/operator-console.md](docs/operator-console.md).

## Architecture

```text
Gomeboy
  → Tetris memory decoder
  → structured Observation
  → legal placement simulations
       → heuristic | lookahead | LLM (top-N shortlist)
  → Placement{rotation, target_column}
  → deterministic input controller
  → next Observation
  → canonical state hash + replay
```

Planners choose placements. They do not own frame timing, collision, lock detection, or button sequences.

```text
GamePilot
├── emulator/session    thin wrapper around pkg/gomeboy
├── planner/openai      OpenAI-compatible chat-completions client
├── profiles/tetris     observation, planning, controller, replay, benchmark
├── runtime/sessions         long-lived lifecycle, frames, pacing
├── runtime/operatorapi      private authenticated control plane
├── runtime/operatorconsole  embedded private browser console
└── cmd/gamepilot            CLI
```

## Documentation

| Doc | Topic |
| --- | --- |
| [docs/architecture.md](docs/architecture.md) | Ownership boundaries and package layout |
| [docs/tetris-rev1.md](docs/tetris-rev1.md) | Supported ROM and memory map |
| [docs/lookahead.md](docs/lookahead.md) | Two-ply preview search |
| [docs/llm-planner.md](docs/llm-planner.md) | LLM shortlist, validation, and wire protocol |
| [docs/replay.md](docs/replay.md) | Replay format and verification |
| [docs/benchmark.md](docs/benchmark.md) | Fairness model and limitations |
| [docs/live-sessions.md](docs/live-sessions.md) | Session manager, frames, pacing |
| [docs/operator-api.md](docs/operator-api.md) | Auth, catalog, routes, limits |
| [docs/operator-console.md](docs/operator-console.md) | Embedded private browser console |

## Status

Implemented: Tetris Rev 1 observation and control, heuristic and lookahead planners, OpenAI-compatible LLM planning, replays, planner benchmarks, long-lived sessions with realtime pacing and frame capture, a private operator API, and an embedded private operator console.

Not implemented: the public spectator, durable session history, MCP, deeper-than-preview search, or a provider-specific Structured Outputs adapter.

You must supply your own legally obtained ROM. GamePilot does not distribute copyrighted game data.
