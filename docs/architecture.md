# GamePilot architecture

The ownership boundary for the project is intentionally simple:

```text
Gomeboy = emulator truth
GamePilot = generic agent/runtime
Profile = game-specific interpretation
Planner = strategic decision maker
Controller = deterministic short-horizon execution
Replay = planner-independent reproducibility record
```

## Current packages

```text
GamePilot
├── emulator/session   thin lifetime/checkpoint wrapper around pkg/gomeboy
├── profiles           minimal profile-selection boundary
├── profiles/tetris    all Tetris-specific addresses, semantics, planning, controller, and replay state
└── cmd/gamepilot      runnable CLI
```

The session package does not recreate emulation primitives. Profiles and controllers use Gomeboy's public `StepFrame`/`StepFrames`, `Press`/`Release`, `Peek8`/`PeekInto`, `FrameCount`, `SaveState`/`LoadState`, cartridge metadata, and ROM SHA-256 directly.

## Deterministic loop

The first Tetris implementation now has a complete planner-to-replay loop:

```text
ROM + deterministic startup
        ↓
structured Observation
        ↓
planner chooses Placement
        ↓
strict controller executes inputs
        ↓
next Observation
        ↓
canonical state hash + replay record
```

A replay contains game-level placements and decoded state, not emulator-internal input timing or opaque save-state bytes. Verification starts from the same deterministic startup, re-executes each recorded placement through the normal controller, and compares both canonical state hashes and frame counts after every boundary.

This preserves the most important planner boundary: heuristic, LLM, or future planners only need to produce `Placement{Rotation, TargetColumn}`. They do not own frame timing, collision assumptions, lock detection, or replay execution.

## Vertical slices

Completed:

1. `Placement{Rotation, TargetColumn}` and deterministic Tetris controller.
2. Lock/new-piece boundary detection.
3. Deterministic heuristic planner and repeated observation -> decision -> action loop.
4. Versioned high-level replay records with canonical state hashes and fresh-boot verification.

Next:

5. Add one OpenAI-compatible LLM planner behind the same planner boundary, with strict JSON validation and configurable base URL/model/API key.
6. Add replay fixtures/integration checks around real-ROM runs when an appropriate local test harness is available without committing copyrighted ROM data.
7. Consider a thin MCP surface only after the planner interfaces are stable.

Future games are expected to own different observation models; the generic runtime should not absorb Tetris board geometry, piece IDs, RAM addresses, or replay-state hashing rules.
