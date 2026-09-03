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
├── planner/openai     generic OpenAI-compatible chat-completions transport
├── profiles           minimal profile-selection boundary
├── profiles/tetris    Tetris-specific addresses, semantics, simulation, planners, controller, and replay state
└── cmd/gamepilot      runnable CLI
```

The session package does not recreate emulation primitives. Profiles and controllers use Gomeboy's public `StepFrame`/`StepFrames`, `Press`/`Release`, `Peek8`/`PeekInto`, `FrameCount`, `SaveState`/`LoadState`, cartridge metadata, and ROM SHA-256 directly.

The OpenAI-compatible transport is deliberately generic: it knows how to send system/user text to `/chat/completions` and retrieve the returned text, but it does not know about Tetris, boards, pieces, or placements. Tetris owns prompt construction and action validation.

## Planner boundary

Both current planners consume the same decoded observation and produce the same game-level action:

```text
structured Observation
        ↓
LegalSimulations (deterministic)
        ↓
   ┌───────────────┬────────────────────┐
   │ heuristic     │ external LLM       │
   │ scores locally│ chooses candidate  │
   └───────────────┴────────────────────┘
        ↓
Placement{rotation, target_column}
        ↓
strict controller
        ↓
next Observation
```

The external model is not trusted with emulator timing or arbitrary inputs. Before the model is called, the profile enumerates the deterministic non-top-out candidate set. The model receives the current state plus candidate simulation metrics and must return one candidate as JSON. GamePilot rejects malformed JSON, unknown fields, invalid rotations, or placements outside that set before the controller sees anything.

This means local and hosted models are strategic plug-ins, not emulator drivers.

## Replay boundary

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

Replay is planner-independent: an LLM-driven run can be verified later without contacting the model again.

## Vertical slices

Completed:

1. `Placement{Rotation, TargetColumn}` and deterministic Tetris controller.
2. Lock/new-piece boundary detection.
3. Deterministic heuristic planner and repeated observation -> decision -> action loop.
4. Versioned high-level replay records with canonical state hashes and fresh-boot verification.
5. OpenAI-compatible LLM planner behind the same action boundary, with strict JSON/legal-action validation and configurable base URL/model/API-key environment variable.

Next:

6. Exercise local/hosted model behavior and use replay files to compare model choices against the heuristic baseline.
7. Consider deeper lookahead/search or richer candidate summaries if one-piece LLM decisions are not strong enough.
8. Consider a thin MCP surface only after the planner interfaces are stable.

Future games are expected to own different observation models; the generic runtime should not absorb Tetris board geometry, piece IDs, RAM addresses, prompt semantics, or replay-state hashing rules.
