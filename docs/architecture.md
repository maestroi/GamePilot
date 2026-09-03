# GamePilot architecture

The ownership boundary for the project is intentionally simple:

```text
Gomeboy = emulator truth
GamePilot = generic agent/runtime
Profile = game-specific interpretation
Planner = strategic decision maker
Controller = deterministic short-horizon execution
Replay = planner-independent reproducibility record
Benchmark = replay-derived fixed workload for planner comparison
```

## Current packages

```text
GamePilot
├── emulator/session   thin lifetime/checkpoint wrapper around pkg/gomeboy
├── planner/openai     minimal OpenAI-compatible chat-completions transport
├── profiles           minimal profile-selection boundary
├── profiles/tetris    Tetris-specific addresses, semantics, planning, benchmark, controller, and replay state
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
deterministic legal simulations
        ↓
    one-ply heuristic
        OR
    two-ply preview search
        ↓
unique best-first current placements
        ↓
lookahead planner OR top-N shortlist -> LLM planner
        ↓
validated Placement
        ↓
strict controller executes inputs
        ↓
next Observation
        ↓
canonical state hash + replay record
```

A replay contains game-level placements and decoded state, not emulator-internal input timing or opaque save-state bytes. Verification starts from the same deterministic startup, re-executes each recorded placement through the normal controller, and compares both canonical state hashes and frame counts after every boundary.

This preserves the most important planner boundary: heuristic, lookahead, LLM, or future planners only produce `Placement{Rotation, TargetColumn}`. They do not own frame timing, collision assumptions, lock detection, or replay execution.

## Preview-piece lookahead

Tetris Rev 1 exposes the next piece in memory, so the profile can evaluate two plies without guessing. For each strategically distinct current placement it simulates the current piece, then every legal reply for the known preview piece, and evaluates the final leaf board. Equivalent first moves that produce the same settled board are collapsed before ranking so symmetric raw rotations do not consume multiple shortlist entries.

The standalone `lookahead` planner chooses the top-ranked two-ply candidate directly. The LLM planner uses the same ordered candidates but only receives the best N (default 10). The simulated preview reply is never queued for execution; after the real current placement, GamePilot observes the ROM again and replans from truth.

The external-model transport remains deliberately small. `planner/openai` owns only the OpenAI-compatible HTTP wire shape. Tetris owns its prompt, shortlist generation, output schema validation, and retries. Fast control-loop defaults disable Qwen/vLLM-style thinking and bound response tokens, while `auto` mode can omit provider-specific thinking fields.

## Fixed-sequence planner benchmark

Separate live planner runs are not assumed to be directly comparable. Different placements require different controller inputs and frame counts, so a timing-sensitive ROM RNG could expose different future pieces to each planner.

Benchmark mode therefore consumes a validated replay as a scenario source:

```text
validated replay
      ↓
initial settled board
+ preview-consistent piece sequence
      ↓
canonical scenario hash
      ↓
reset simulated board for each planner
      ↓
heuristic / lookahead / optional LLM
      ↓
placement trace + final board + quality/latency metrics
```

Only the replay's initial board and piece stream are reused. Its recorded placements and later boards are ignored. Every planner builds its own board through the deterministic simulator while consuming the same current/preview sequence.

This benchmark is intentionally planner-level, not emulator-level. It measures lines, board quality, planner timing, retries, and candidate compression. Exact Game Boy score progression, soft-drop scoring, controller timing, level progression, and future RNG beyond the frozen sequence remain emulator-truth concerns and are validated through live runs plus replay verification instead.

## Vertical slices

Completed:

1. `Placement{Rotation, TargetColumn}` and deterministic Tetris controller.
2. Lock/new-piece boundary detection.
3. Deterministic one-ply heuristic planner and repeated observation -> decision -> action loop.
4. Versioned high-level replay records with canonical state hashes and fresh-boot verification.
5. OpenAI-compatible/local LLM planner behind the same placement boundary, with strict JSON/legal-action validation and replay recording.
6. Deterministic two-ply preview-piece lookahead plus strategically unique top-N LLM candidate shortlisting.
7. Replay-backed fixed-sequence planner benchmarking with board-quality, latency, retry, and candidate-compression reporting.

Next:

8. Use benchmark reports across longer/multiple replay scenarios to decide whether deeper search or the LLM adds enough value to justify its cost.
9. Add replay fixtures/integration checks around real-ROM/model runs when an appropriate local test harness is available without committing copyrighted ROM data.
10. Consider a thin MCP surface once the planner/benchmark interfaces are stable.

Future games are expected to own different observation models; the generic runtime should not absorb Tetris board geometry, piece IDs, RAM addresses, lookahead rules, benchmark scenario semantics, or replay-state hashing rules.
