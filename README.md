# GamePilot

GamePilot is a game-agent runtime built on top of the public [`pkg/gomeboy`](https://github.com/maestroi/gomeboy/tree/main/pkg/gomeboy) API.

The first supported profile is **Game Boy Tetris, Rev 1**. The initial vertical slice is deliberately small: load the exact ROM, boot Type A level 0 deterministically, decode structured state from memory, and print it without using screenshots or vision.

## Current slice

```text
Gomeboy
  -> Tetris memory decoder
  -> structured Observation
  -> CLI output
```

Implemented now:

- Gomeboy `v1.0.0` integration through a thin session wrapper
- exact Tetris Rev 1 SHA-256 gating
- deterministic Type A level-0 startup using frame stepping and button input
- 18x10 settled-board decoding from the ROM's WRAM playfield buffer
- active and preview tetromino decoding, including rotation
- score, level, lines, ready-state, and game-over decoding
- save/load checkpoint primitives delegated to Gomeboy
- decoder and ROM-detection tests that require no copyrighted ROM fixture

Not implemented in this slice yet: placement execution, heuristic planning, replay logging, OpenAI-compatible LLM planning, or MCP.

## Run

You must supply your own legally obtained Rev 1 ROM. ROM files are intentionally ignored by git.

```bash
go run ./cmd/gamepilot -rom /path/to/tetris.gb -planner observe
```

See [`docs/architecture.md`](docs/architecture.md) and [`docs/tetris-rev1.md`](docs/tetris-rev1.md).
