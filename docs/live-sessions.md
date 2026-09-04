# Live session runtime

`runtime/sessions` is the lifecycle and read-model layer between planner logic and the browser/API surfaces.

It makes a GamePilot run long-lived, cancellable, observable through copied state, and safe to host inside a server process. Managed observable sessions publish the latest rendered Game Boy framebuffer as an encoded PNG and can run controller execution at a watchable real-time cadence.

## Ownership rule

A running Gomeboy emulator has one owner goroutine:

```text
Manager.Start
    ↓
managed session goroutine
    ↓
profile runner
    ↓
Gomeboy emulator
```

Other goroutines never receive the emulator pointer. They call `Manager.Snapshot`, `Manager.List`, `Manager.Frame`, `Manager.Wait`, or `Manager.Replay`, which return copied read models/bytes.

That boundary is required because one Gomeboy `Emulator` is not safe for concurrent use. Frame capture also happens inside the owner goroutine: Gomeboy's RGB framebuffer is immediately encoded to PNG before anything is published, so the framebuffer's aliased memory never escapes to readers.

## Lifecycle

Sessions use these states:

```text
queued → starting → running → done
                    ↓
                 stopping → done

startup/run failure → failed
```

`Stop` is idempotent and uses context cancellation. A stopped session still finalizes any enabled replay using only placements that fully reached the next deterministic observation boundary.

`Manager.Close(ctx)` cancels and joins every non-terminal session.

## Launch configuration

The current internal launch object contains:

- ROM path
- profile ID
- planner ID
- optional move limit (`0` means until game over/cancel)
- replay recording toggle
- pacing mode (`fast` or `realtime`)
- safe planner options such as model alias and shortlist size

Unspecified pacing normalizes to `fast` for backward compatibility. `realtime` is intended for human-observable live sessions.

`ROMPath` has `json:"-"` so a generic serialized session snapshot cannot accidentally reveal a server filesystem path. The private control API will later resolve an allowlisted ROM alias to this internal path.

Credentials and raw model endpoints are deliberately not part of launch configuration.

## Tetris runtime

`NewTetrisManager` supports the built-in deterministic planners:

- `heuristic`
- `lookahead`

The Tetris runner owns, in one goroutine:

1. opening the ROM with framebuffer generation enabled;
2. exact Rev 1 hash validation;
3. deterministic Type A level-0 startup;
4. memory observation;
5. planner selection;
6. strict placement execution;
7. optional wall-clock pacing of the controller's already-required emulator frames;
8. framebuffer capture/PNG encoding for sampled presentation frames;
9. replay append/finalization;
10. emulator close.

The one-shot CLI still uses the video-disabled emulator path when rendered frames are not needed.

A watchable manager session can be constructed programmatically:

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
if err != nil {
    return err
}

snapshot, err := manager.Snapshot(id)
if err != nil {
    return err
}
fmt.Println(snapshot.Status, snapshot.Moves, snapshot.PlannerActivity)

frame, err := manager.Frame(id)
if err != nil {
    return err
}
fmt.Println(frame.ContentType, frame.Width, frame.Height, frame.EmulatorFrame)
```

`Snapshot.Observation` and `Snapshot.Decision` are profile/planner-specific JSON payloads. They are copied on publish/read so consumers cannot mutate the runner's state.

## Realtime pacing

Gomeboy is intentionally unthrottled. GamePilot therefore treats pacing as presentation behavior, not emulation behavior.

`fast` mode preserves the existing unthrottled controller path. `realtime` mode uses the same controller but attaches an observer after each emulator frame that the controller already stepped. The observer may wait in wall-clock time and publish a frame, but it never calls `StepFrame`, changes buttons, or changes placement legality.

The target cadence uses the native DMG timing relationship:

```text
70224 clocks/frame at 4194304 Hz ≈ 59.7 frames/second
```

The pacer schedules every controller frame at that cadence. PNG publication is sampled at roughly 30 fps to reduce encoding work, with immediate publication for important visible changes such as rotation, horizontal movement, ready/visibility transitions, and game over.

Cancellation is context-aware and interrupts pacing sleeps promptly.

Tests assert that observed/realtime controller execution finishes on the same emulator frame and final state as the ordinary path, and that fast/realtime runs produce identical replay bytes for the same planner choices and stable state boundaries.

## Planner activity and latency

While a planner is running, snapshots expose:

- `planner_activity: "planning"`
- `planner_started_at`

When a decision returns and controller execution starts, snapshots expose:

- `planner_activity: "executing"`
- `planner_latency_ms`

This is especially useful for local/remote LLM planners: a frozen Game Boy image while the model is thinking is represented as planner work rather than appearing to be a hung emulator.

## Frame publication

A session retains only the most recently published encoded frame. There is no frame queue.

That means a slow browser may skip old presentation frames, but it cannot backpressure emulator execution or cause unbounded buffering. `Manager.Frame(id)` returns a defensive copy of the latest image bytes.

The snapshot exposes freshness metadata without embedding the image itself:

- `sequence`: monotonically increases when session-visible state changes;
- `frame_sequence`: the snapshot sequence that produced the currently retained frame;
- `frame_available`: whether a frame has been published;
- `updated_at`: wall-clock timestamp for the latest session-visible update;
- `frame`: deterministic emulator frame number represented by the current structured observation.

The retained `Frame` includes its own sequence, emulator frame number, dimensions, MIME type, capture time, and image bytes.

The planner continues to read RAM only. Calling Gomeboy `Frame()` does not advance the emulator or change input boundaries.

## Read-only HTTP transport

`NewReadHandler(manager)` provides a small GET-only transport for later browser surfaces:

```text
GET /v1/sessions/{id}
GET /v1/sessions/{id}/frame
```

The state route returns the copied `Snapshot` as JSON with `Cache-Control: no-store`.

The frame route returns the current encoded PNG directly, also with `Cache-Control: no-store`, plus:

- `X-GamePilot-Sequence`
- `X-GamePilot-Emulator-Frame`

No launch, stop, delete, ROM-selection, credential, or manual-control routes are mounted by this handler. The future private operator API (#9) owns mutations. The public spectator (#8) must still define a narrower explicitly allowlisted public DTO rather than serializing the generic internal snapshot directly.

## LLM sessions

The runtime intentionally does not know provider credentials. Server code may add an `llm` planner by mapping a safe model alias to a configured `JSONCompleter`:

```go
factory := sessions.NewTetrisRunnerFactory(map[string]sessions.TetrisPlannerFactory{
    "llm": sessions.LLMPlannerFactory(func(config sessions.LaunchConfig) (tetris.JSONCompleter, error) {
        return resolveConfiguredModelAlias(config.PlannerOptions.Model)
    }),
})
manager := sessions.NewManager(factory)
```

The existing strict shortlist/output validation remains inside the Tetris planner. The model still has no emulator access.

## Replay behavior

When `RecordReplay` is enabled, terminal sessions retain encoded replay bytes in memory. `Manager.Replay(id)` returns a defensive copy.

This is intentionally in-memory for now. Durable artifact storage/retention belongs to issue #11.

Cancellation during planning or a placement never fabricates a completed move. The finalized replay ends at the last fully completed placement boundary and remains valid according to the existing replay format.

## What remains

Issues #9/#10 add the private authenticated session API/operator console, and #8 adds the separate public read-only spectator. Those surfaces can now request `realtime` pacing and consume the existing copied snapshot/latest-frame read model without owning emulator timing.
