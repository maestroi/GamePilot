# GamePilot

GamePilot is a game-agent runtime built on top of the public [`pkg/gomeboy`](https://github.com/maestroi/gomeboy/tree/main/pkg/gomeboy) API.

The first supported profile is **Game Boy Tetris, Rev 1**. The current vertical slice loads the exact ROM, boots Type A level 0 deterministically, decodes structured state from memory, executes verified game-level placements, and can run a small deterministic heuristic planner without screenshots or vision.

## Current slice

```text
Gomeboy
  -> Tetris memory decoder
  -> structured Observation
  -> exact ROM-derived tetromino geometry
  -> placement simulator + heuristic planner
  -> Placement{rotation, target_column}
  -> deterministic input controller
  -> next Observation
```

Implemented now:

- Gomeboy `v1.0.0` integration through a thin session wrapper
- exact Tetris Rev 1 SHA-256 gating
- deterministic Type A level-0 startup using frame stepping and button input
- 18x10 settled-board decoding from the ROM's WRAM playfield buffer
- active and preview tetromino decoding, including rotation
- score, level, lines, ready-state, and game-over decoding
- save/load checkpoint primitives delegated to Gomeboy
- exact Rev 1 tetromino geometry shared by controller and planner
- deterministic `Placement{Rotation, TargetColumn}` execution using verified rotate/shift inputs and soft drop
- lock/new-piece transition detection before returning control
- deterministic placement simulation with line clears and top-out detection
- a one-piece heuristic over aggregate height, completed lines, holes, and bumpiness
- repeated heuristic placement execution with a configurable move limit
- ROM-free tests for detection, decoding, startup, geometry, controller, simulation, and planner behavior

Not implemented yet: replay logging/reproducibility records, OpenAI-compatible LLM planning, MCP, or deeper lookahead/search.

## Run

You must supply your own legally obtained Rev 1 ROM. ROM files are intentionally ignored by git.

```bash
# Decode the first ready position.
go run ./cmd/gamepilot -rom /path/to/tetris.gb -planner observe

# Place the first piece. target column means the leftmost occupied board column.
go run ./cmd/gamepilot -rom /path/to/tetris.gb -planner place -rotation 1 -column 6

# Let the deterministic heuristic choose and execute 25 placements.
go run ./cmd/gamepilot -rom /path/to/tetris.gb -planner heuristic -pieces 25
```

The heuristic is intentionally small: for every raw ROM rotation and legal leftmost column it simulates a straight drop, applies completed line clears, scores the resulting board using aggregate height, line clears, holes, and bumpiness, then sends the chosen `Placement` through the same verified controller used by manual placement mode.

See [`docs/architecture.md`](docs/architecture.md) and [`docs/tetris-rev1.md`](docs/tetris-rev1.md).
