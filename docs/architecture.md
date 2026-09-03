# GamePilot architecture

The ownership boundary for the project is intentionally simple:

```text
Gomeboy = emulator truth
GamePilot = generic agent/runtime
Profile = game-specific interpretation
Planner = strategic decision maker
Controller = deterministic short-horizon execution
```

## Current packages

```text
GamePilot
├── emulator/session   thin lifetime/checkpoint wrapper around pkg/gomeboy
├── profiles           minimal profile-selection boundary
├── profiles/tetris    all Tetris-specific addresses and semantics
└── cmd/gamepilot      runnable CLI
```

The session package does not recreate emulation primitives. Profiles and future controllers use Gomeboy's public `StepFrame`/`StepFrames`, `Press`/`Release`, `Peek8`/`PeekInto`, `FrameCount`, `SaveState`/`LoadState`, cartridge metadata, and ROM SHA-256 directly.

## Next vertical slices

1. Add `Placement{Rotation, TargetColumn}` and a deterministic Tetris controller.
2. Stop execution only after the placed piece has locked and the next piece is ready.
3. Add a deterministic heuristic planner and the observation -> decision -> action loop.
4. Add high-level replay records with frame numbers and checkpoint reproducibility tests.
5. Add one OpenAI-compatible LLM planner behind the planner boundary, with strict JSON validation and configurable base URL/model/API key.
6. Consider a thin MCP surface only after the local Tetris loop is working.

Future games are expected to own different observation models; the generic runtime should not absorb Tetris board geometry, piece IDs, or RAM addresses.
