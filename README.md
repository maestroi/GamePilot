# GamePilot

GamePilot is a game-agent runtime built on top of the public [`pkg/gomeboy`](https://github.com/maestroi/gomeboy/tree/main/pkg/gomeboy) API.

The first supported profile is **Game Boy Tetris, Rev 1**. The current vertical slice loads the exact ROM, boots Type A level 0 deterministically, decodes structured state from memory, executes verified game-level placements, supports one-ply heuristic, two-ply preview lookahead, and OpenAI-compatible LLM planners, records/verifies planner-independent replays, benchmarks planners on fixed replay-derived scenarios, and can host those planners inside observable long-lived sessions with rendered Game Boy frames.

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

long-lived mode
  -> session Manager
  -> one owner goroutine per emulator
  -> copied observation/decision snapshots
  -> latest encoded Game Boy frame
  -> optional realtime presentation pacing
  -> private authenticated operator API + embedded operator console
  -> explicit public spectator DTO/API + embedded watch UI
  -> separate public/private HTTP trust surfaces
  -> cancellation + terminal status
  -> finalized replay bytes
```

Implemented now:

- Gomeboy `v1.0.0` integration through a thin emulator session wrapper
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
- one-ply heuristic planning
- deterministic two-ply lookahead using the known ROM preview piece
- deduplication of equivalent first-move board outcomes
- OpenAI-compatible `/v1/chat/completions` planning with strict JSON/legal-action validation and retries
- versioned planner-independent replay recording and fresh-boot verification
- replay-backed fixed-sequence planner benchmarking
- `runtime/sessions` long-lived lifecycle management with one goroutine owning each emulator
- cancellation-safe partial replay finalization and exact-once emulator close
- rendered 160x144 Game Boy frame capture for managed observable sessions
- latest-frame-only publication so slow readers cannot backpressure emulator execution
- GET-only session snapshot/frame transport
- `fast` and `realtime` session pacing modes
- realtime controller presentation near native DMG cadence (~59.7 Hz) without changing emulator frames/input boundaries
- sampled intermediate frame publication during rotations, movement, falling, lock/ready transitions, and game over
- planner activity timestamps/latency so LLM waits are visible as planning rather than a frozen emulator
- `runtime/operatorapi` private Bearer-authenticated launch/list/get/frame/stop/delete control plane
- allowlisted ROM/profile/planner/model launch configuration with no raw filesystem paths or provider secrets in requests
- terminal-only retained-session deletion, structured API errors, mutation body/rate/time limits, and auth-disabled route removal
- `runtime/operatorconsole` dependency-free embedded private browser UI for launch, active-first session management, live framebuffer/Tetris state, planner metadata, bounded activity events, stop/delete, and replay download
- `runtime/spectatorapi` explicit allowlisted public read model with bounded completed-session history and generic fail-closed errors
- `runtime/spectator` dependency-free public watch UI for live framebuffer, board/pieces, score/lines/level, planner summary, and recent completed sessions
- `runtime/websurfaces` separate public/private `http.Server` composition with different default binds, health/readiness, and HTTP limits

Not implemented yet: durable session history/artifact retention, MCP, deeper-than-preview search, or a provider-specific Structured Outputs adapter.

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

### Programmatic live sessions

The long-lived runtime is a Go package rather than a second emulator implementation. A server can start a session and read copied snapshots/frames while the owner goroutine runs the normal profile/planner/controller loop:

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

Use `PacingFast` for unthrottled/test-style execution and `PacingRealtime` for human-observable sessions. The realtime mode delays wall clock only; it does not add/remove emulator frames or change controller input boundaries.

Readers use copied state only:

```text
Manager.Snapshot(id)
Manager.Frame(id)
Manager.List()
Manager.Stop(id)
Manager.Wait(ctx, id)
Manager.Replay(id)
Manager.Delete(id)  # terminal history only
```

The read-only HTTP adapter exposes:

```text
GET /v1/sessions/{id}
GET /v1/sessions/{id}/frame
```

See [`docs/live-sessions.md`](docs/live-sessions.md).

### Private operator API

`runtime/operatorapi` provides the separate authenticated control plane for the private browser operator console. Every mounted route requires a deployment-configured Bearer token. If the token is empty, the handler mounts no operator routes and returns `404` for the whole surface.

The browser receives only server-configured aliases for ROMs, profiles/planners, and optional model choices. Raw ROM paths, model provider URLs, and credentials are not request fields.

Private routes:

```text
GET    /v1/config
GET    /v1/sessions
GET    /v1/sessions/{id}
GET    /v1/sessions/{id}/frame
POST   /v1/sessions
POST   /v1/sessions/{id}/stop
DELETE /v1/sessions/{id}
```

`operatorapi.NewHandlerWithReplay` composes the same route set with an authenticated `GET /v1/sessions/{id}/replay` download for retained replay bytes. `NewHandler` keeps the original route set for callers that do not want artifact download mounted.

Launch requests default to `realtime` pacing on this surface so the operator console gets watchable play without changing the lower-level session manager's `fast` default. Delete only removes terminal retained history; active sessions must be stopped first.

See [`docs/operator-api.md`](docs/operator-api.md) for the trust model, catalog setup, request shapes, limits, and structured errors.

### Private operator console

`runtime/operatorconsole` embeds the HTML/CSS/JavaScript shell and delegates every `/v1/*` request to the private API on the same origin. The deployment token is entered by the operator and kept in browser `sessionStorage`; it is never serialized into the embedded assets.

Compose it with the replay-enabled API when you want the complete console surface:

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

Keep this listener private (for example loopback + VPN/reverse proxy). See [`docs/operator-console.md`](docs/operator-console.md).

### Public spectator and trust split

`runtime/spectatorapi` exposes a separate allowlisted read model rather than serializing `sessions.Snapshot` or reusing the private operator API. The public browser gets only spectator-safe session status, sanitized planner/model labels, Tetris board/piece/score state, latest placement, elapsed progress, a bounded completed-session tail, and the latest PNG frame.

Public routes are GET-only:

```text
GET /v1/watch
GET /v1/watch?session=<id>
GET /v1/frame/{id}
```

`runtime/websurfaces` can compose the public spectator and private operator onto different listeners:

```go
servers, err := websurfaces.NewServers(websurfaces.Options{
    Manager: manager,
    Operator: operatorapi.Options{
        OperatorToken: os.Getenv("GAMEPILOT_OPERATOR_TOKEN"),
        ROMs:           romCatalog,
        Profiles:       profileCatalog,
        Models:         modelCatalog,
    },
    PublicAddr:  ":8080",
    PrivateAddr: "127.0.0.1:8081",
})
```

The public handler never receives operator routes, and tests explicitly prove `/v1/config`, launch, stop, and delete paths are unavailable on the public surface. Keep the private listener behind loopback/private networking or a VPN even though it also requires Bearer authentication.

See [`docs/public-spectator.md`](docs/public-spectator.md) for the public DTO, security headers, fail-closed behavior, and deployment trust boundary.

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

The summary reports pieces placed, lines cleared, top-out, final aggregate height/holes, average/max planner latency, LLM retries, and average candidate compression. See [`docs/benchmark.md`](docs/benchmark.md) for the fairness model and limitations.

### Local/OpenAI-compatible LLM planner

The default LLM base URL is `http://localhost:1234/v1`. Supply the model identifier exposed by your server. Keyless local servers can disable the API-key environment lookup with `-llm-api-key-env ""`.

GamePilot defaults to `-llm-thinking off`, `-llm-max-tokens 64`, and `-llm-candidates 10`. GamePilot deduplicates equivalent first outcomes, evaluates each with the known preview piece, sorts them deterministically, and sends only the best N to the model.

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

For Qwen3/vLLM-style endpoints, non-thinking mode sends `chat_template_kwargs.enable_thinking=false`. If a compatible server rejects the provider-specific thinking field, use `-llm-thinking auto` to omit it. `-llm-thinking on` explicitly enables it.

The model never sends emulator inputs. GamePilot computes and ranks deterministic candidates, asks the model to choose one shortlisted current-piece placement, strictly validates the returned JSON, and only then passes the selected `Placement` to the same controller used by deterministic planner modes.

`-replay-out` works with `place`, `heuristic`, `lookahead`, and `llm`. Replay verification does not invoke any planner.

See [`docs/architecture.md`](docs/architecture.md), [`docs/live-sessions.md`](docs/live-sessions.md), [`docs/operator-api.md`](docs/operator-api.md), [`docs/operator-console.md`](docs/operator-console.md), [`docs/public-spectator.md`](docs/public-spectator.md), [`docs/benchmark.md`](docs/benchmark.md), [`docs/lookahead.md`](docs/lookahead.md), [`docs/llm-planner.md`](docs/llm-planner.md), [`docs/replay.md`](docs/replay.md), and [`docs/tetris-rev1.md`](docs/tetris-rev1.md).
