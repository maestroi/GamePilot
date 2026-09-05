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
Session runtime = long-lived lifecycle + copied read model + presentation pacing
```

## Current packages

```text
GamePilot
├── emulator/session        thin lifetime/checkpoint wrapper around pkg/gomeboy
├── planner/openai          minimal OpenAI-compatible chat-completions transport
├── profiles                minimal profile-selection boundary
├── profiles/tetris         Tetris-specific addresses, semantics, planning, benchmark, controller, and replay state
├── runtime/sessions        lifecycle, frame publication, pacing, read transport, and Tetris runner adapter
├── runtime/operatorapi     private authenticated session control plane and optional replay download
├── runtime/operatorconsole embedded same-origin private browser operator UI
├── runtime/spectatorapi    explicit allowlisted public read-only watch DTO/API
├── runtime/spectator       embedded public browser spectator UI
├── runtime/websurfaces     separate public/private HTTP server composition and limits
└── cmd/gamepilot           runnable CLI and benchmark entrypoint
```

The emulator session package does not recreate emulation primitives. Profiles and controllers use Gomeboy's public `StepFrame`/`StepFrames`, `Press`/`Release`, `Peek8`/`PeekInto`, `FrameCount`, `SaveState`/`LoadState`, cartridge metadata, ROM SHA-256, and framebuffer output directly.

## Live-session ownership

The live runtime adds a strict single-owner rule above those primitives:

```text
browser/API readers
        ↓
Manager.Snapshot / Frame / List / Replay
        ↓ copied immutable data
managed session goroutine
        ↓
profile runner
        ↓
Gomeboy emulator
```

One managed session goroutine is the only caller allowed to open, step, inspect, control, capture frames from, and close its emulator. HTTP handlers, spectators, persistence workers, and other readers never receive the emulator pointer. This matches Gomeboy's non-concurrent instance contract and prevents frame publication or UI polling from racing deterministic controller execution.

`runtime/sessions` stores profile observation and planner decision payloads as copied JSON plus one latest encoded framebuffer image. That keeps lifecycle/state management generic while leaving Tetris board/action semantics inside `profiles/tetris`. Slow readers may miss presentation frames, but there is no frame queue and therefore no observer backpressure.

Internal ROM paths are excluded from serialized session snapshots. Model credentials/provider URLs are not part of session launch configuration; private server code resolves safe aliases to those secrets outside the session read model. `runtime/operatorapi` enforces the alias allowlists and Bearer-authenticated mutation/read boundary, while `runtime/operatorconsole` embeds only deployment-agnostic assets and delegates its `/v1/*` traffic back to that API.

The public side is a different trust boundary, not a filtered operator handler. `runtime/spectatorapi` depends on a read-only `SessionReader` interface and builds explicit `PublicSession` DTOs instead of serializing `sessions.Snapshot`. It allowlists only spectator-safe state and omits ROM hashes/paths, raw errors/reasons, raw observation/decision payloads, replay bytes, operator metadata, and provider configuration. `runtime/spectator` constructs that API internally, so an arbitrary private handler cannot be mounted behind the public assets.

`runtime/websurfaces` composes the public spectator and private operator as different `http.Server` values with different default bind addresses and HTTP limits. The public server receives only the spectator handler; the private server receives the authenticated operator console/API. Network policy remains a deployment responsibility, but the public process/port route table never contains operator mutations.

## Deterministic loop

The first Tetris implementation has a complete planner-to-replay loop:

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

This preserves the most important planner boundary: heuristic, lookahead, LLM, or future planners only produce `Placement{Rotation, TargetColumn}`. They do not own frame timing, collision assumptions, lock detection, replay execution, or presentation pacing.

The live session runner reuses this same loop. Cancellation can stop a session at any point, but a replay records only placements that fully reached the next stable observation boundary. Terminal session state may therefore retain a valid partial replay without pretending an interrupted placement completed.

## Presentation frames and realtime pacing

Framebuffer output is strictly a human-observation path:

```text
controller-required emulator frame
        ↓
memory-derived Observation
        ↓
optional presentation observer
        ├─ wall-clock wait only
        └─ sampled framebuffer capture → PNG
```

`ExecutePlacementObserved` calls a presentation observer after frames the controller already executed. The observer cannot cause extra emulator stepping. Normal `ExecutePlacement` uses the same controller implementation with no observer.

Session pacing has two modes:

- `fast`: existing unthrottled behavior, used by tests and non-visual runs;
- `realtime`: schedules each controller frame near the native DMG cadence, ~59.7 Hz.

Realtime mode samples encoded PNG publication around 30 fps and publishes immediately for important visible transitions such as rotation, horizontal movement, visibility/ready changes, and game over. Only wall-clock time changes; emulator frame counts and input boundaries do not.

Planner waits are represented separately from emulator pacing. A session exposes `planner_started_at` while planning and `planner_latency_ms` once execution begins, so an LLM delay is visible as planner activity instead of being confused with a frozen emulator.

## Public spectator trust boundary

The public watch flow is deliberately one-way:

```text
internet browser
      ↓ GET only
runtime/spectator
      ↓
runtime/spectatorapi
      ↓ copied read capability only
sessions.Manager
```

The public JSON contains a selected active/recent session plus a bounded completed-session tail. Tetris observation JSON is decoded into an explicit board/piece/score/lines/level DTO, and planner decision JSON is reduced to the latest validated placement. Arbitrary internal JSON fields therefore do not survive projection.

The latest frame endpoint only serves retained `image/png` data and never reflects arbitrary internal MIME types. Backend failures are collapsed to a generic `spectator_unavailable` response so raw errors cannot cross the public boundary.

Public assets contain no operator token or authentication storage. Private paths such as `/v1/config`, `/v1/sessions`, stop/delete controls, replay download, ROM aliases, and model-provider configuration are not routed by the spectator handler.

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
8. Long-lived session manager/runtime with cancellation, copied snapshots, replay finalization, multiple independent sessions, and a production Tetris runner.
9. Rendered Gomeboy frame publication plus GET-only structured snapshot/frame transport.
10. Watchable realtime session pacing and intermediate controller-frame publication.
11. Private Bearer-authenticated session control API and embedded browser operator console (#9/#10).
12. Explicit public read-only spectator plus separated public/private web trust surfaces (#8/#12).

Next live-product slices:

13. Add durable session history/artifact retention (#11).
14. Add MCP only as an adapter over the stable private session service (#14).

Benchmark reports should be used across longer/multiple replay scenarios to decide whether deeper search or the LLM adds enough value to justify its cost; deeper search is not a prerequisite for the live-product work.

Future games are expected to own different observation models; the generic runtime should not absorb Tetris board geometry, piece IDs, RAM addresses, lookahead rules, benchmark scenario semantics, or replay-state hashing rules.

See [`live-sessions.md`](live-sessions.md) for the session API, frame/pacing semantics, and ownership details, [`operator-api.md`](operator-api.md) for the private control plane, [`operator-console.md`](operator-console.md) for private browser mounting/security, [`public-spectator.md`](public-spectator.md) for the public DTO/routes/deployment trust boundary, and [`benchmark.md`](benchmark.md) for benchmark fairness and limitations.
