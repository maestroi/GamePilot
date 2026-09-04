# Live session runtime

`runtime/sessions` is the lifecycle layer between one-shot planner logic and the future browser/API surfaces.

It does **not** expose HTTP and it does **not** render public/private UI. Its job is to make a GamePilot run long-lived, cancellable, observable through copied state, and safe to host inside a server process.

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

Other goroutines never receive the emulator pointer. They call `Manager.Snapshot`, `Manager.List`, `Manager.Wait`, or `Manager.Replay`, which return copied read models/bytes.

That boundary is required because one Gomeboy `Emulator` is not safe for concurrent use. It also gives the upcoming frame publisher and HTTP handlers one safe read model instead of making them coordinate emulator access themselves.

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
- safe planner options such as model alias and shortlist size

`ROMPath` has `json:"-"` so a generic serialized session snapshot cannot accidentally reveal a server filesystem path. The private control API will later resolve an allowlisted ROM alias to this internal path.

Credentials and raw model endpoints are deliberately not part of launch configuration.

## Tetris runtime

`NewTetrisManager` supports the built-in deterministic planners:

- `heuristic`
- `lookahead`

The Tetris runner owns, in one goroutine:

1. opening the ROM;
2. exact Rev 1 hash validation;
3. deterministic Type A level-0 startup;
4. memory observation;
5. planner selection;
6. strict placement execution;
7. replay append/finalization;
8. emulator close.

A manager can be constructed programmatically:

```go
manager := sessions.NewTetrisManager(nil)

id, err := manager.Start(sessions.LaunchConfig{
    ROMPath:      "./roms/tetris.gb",
    Profile:      tetris.ProfileID,
    Planner:      "lookahead",
    MoveLimit:    25,
    RecordReplay: true,
})
if err != nil {
    return err
}

snapshot, err := manager.Wait(context.Background(), id)
if err != nil {
    return err
}
fmt.Println(snapshot.Status, snapshot.Moves, snapshot.Reason)
```

`Snapshot.Observation` and `Snapshot.Decision` are profile/planner-specific JSON payloads. They are copied on publish/read so consumers cannot mutate the runner's state.

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

## What this slice does not do

Issue #7 adds framebuffer generation and immutable image/frame publication. Current production live sessions still open the emulator through the memory-only `OpenROM` path with video generation disabled.

Issue #13 adds wall-clock pacing and intermediate presentation frames. The deterministic planner/controller remains frame-based and unthrottled until that outer runtime layer is added.

Issues #9/#10 add the private session API/operator console, and #8 adds the separate public read-only spectator.
