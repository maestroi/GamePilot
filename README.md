# GamePilot

GamePilot is a game-agent runtime built on top of the public [`pkg/gomeboy`](https://github.com/maestroi/gomeboy/tree/main/pkg/gomeboy) API.

The first supported profile is **Game Boy Tetris, Rev 1**. The current vertical slice loads the exact ROM, boots Type A level 0 deterministically, decodes structured state from memory, and can execute one verified game-level placement without screenshots or vision.

## Current slice

```text
Gomeboy
  -> Tetris memory decoder
  -> structured Observation
  -> Placement{rotation, target_column}
  -> deterministic input controller
```

Implemented now:

- Gomeboy `v1.0.0` integration through a thin session wrapper
- exact Tetris Rev 1 SHA-256 gating
- deterministic Type A level-0 startup using frame stepping and button input
- 18x10 settled-board decoding from the ROM's WRAM playfield buffer
- active and preview tetromino decoding, including rotation
- score, level, lines, ready-state, and game-over decoding
- save/load checkpoint primitives delegated to Gomeboy
- exact Rev 1 tetromino geometry shared by controller and future planners
- deterministic `Placement{Rotation, TargetColumn}` execution using verified rotate/shift inputs and soft drop
- lock/new-piece transition detection before returning control
- decoder, startup, geometry, and controller tests that require no copyrighted ROM fixture

Not implemented in this slice yet: repeated placement loop, heuristic planning, replay logging, OpenAI-compatible LLM planning, or MCP.

## Run

You must supply your own legally obtained Rev 1 ROM. ROM files are intentionally ignored by git.

```bash
# Decode the first ready position.
go run ./cmd/gamepilot -rom /path/to/tetris.gb -planner observe

# Place the first piece. target column means the leftmost occupied board column.
go run ./cmd/gamepilot -rom /path/to/tetris.gb -planner place -rotation 1 -column 6
```

See [`docs/architecture.md`](docs/architecture.md) and [`docs/tetris-rev1.md`](docs/tetris-rev1.md).
