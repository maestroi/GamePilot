# Private operator session API

`runtime/operatorapi` is the authenticated mutation/read control plane for browser operators. It is deliberately separate from the public spectator trust boundary.

The handler owns HTTP policy only. Session lifecycle, emulator ownership, planning, pacing, frame publication, and replay finalization remain in `runtime/sessions`.

## Authentication and mounting

The operator handler uses one deployment-configured Bearer token.

If the token is empty, `operatorapi.NewHandler` returns a handler with **no operator routes mounted**: every path returns `404`. Missing auth configuration therefore never turns launch/stop/delete into anonymous endpoints.

When a token is configured, every route below requires:

```text
Authorization: Bearer <operator-token>
```

The token itself is not exposed by `/v1/config`, session snapshots, or structured errors.

The intended deployment is a private process/interface/VPN/reverse-proxy location. The later public spectator must not mount this handler.

## Safe server-side catalog

The server configures three allowlists:

- ROM aliases: browser-visible alias -> server-local ROM path + profile;
- profile/planner combinations;
- optional model aliases for model-backed planners.

Example construction:

```go
handler, err := operatorapi.NewHandler(operatorapi.Options{
    Manager:       manager,
    OperatorToken: os.Getenv("GAMEPILOT_OPERATOR_TOKEN"),
    ROMs: []operatorapi.ROMOption{
        {
            Alias:   "tetris-rev1",
            Label:   "Tetris Rev 1",
            Profile: tetris.ProfileID,
            Path:    "./roms/tetris.gb", // server-side only
        },
    },
    Profiles: []operatorapi.ProfileOption{
        {
            ID: tetris.ProfileID,
            Planners: []operatorapi.PlannerOption{
                {ID: "heuristic"},
                {ID: "lookahead"},
                {ID: "llm", RequiresModel: true},
            },
        },
    },
    Models: []operatorapi.ModelOption{
        {Alias: "qwen-local", Label: "Qwen local"},
    },
})
```

`ROMOption.Path` has no HTTP representation. Model aliases similarly refer to provider configuration already installed in the session runner; raw base URLs, API keys, and credentials are not launch fields.

## Routes

All routes are private/authenticated:

```text
GET    /v1/config
GET    /v1/sessions
GET    /v1/sessions/{id}
GET    /v1/sessions/{id}/frame
POST   /v1/sessions
POST   /v1/sessions/{id}/stop
DELETE /v1/sessions/{id}
```

The frame route preserves the live-session image headers from the read side:

- `Content-Type: image/png`
- `Cache-Control: no-store`
- `X-GamePilot-Sequence`
- `X-GamePilot-Emulator-Frame`

## Launch request

The browser submits aliases and bounded safe knobs only:

```json
{
  "rom_alias": "tetris-rev1",
  "profile": "tetris",
  "planner": "lookahead",
  "move_limit": 0,
  "record_replay": true,
  "pacing": "realtime"
}
```

For a model-backed planner:

```json
{
  "rom_alias": "tetris-rev1",
  "profile": "tetris",
  "planner": "llm",
  "move_limit": 0,
  "record_replay": true,
  "pacing": "realtime",
  "model": "qwen-local",
  "shortlist_size": 10
}
```

If `pacing` is omitted, the operator API defaults to `realtime` because this surface is intended for observable live play. The lower-level session manager still defaults to `fast` for backwards-compatible programmatic/tests usage.

`move_limit: 0` means continuous play until game-over or explicit stop. Positive values are bounded by the configured API maximum.

Unknown JSON fields are rejected. In particular, a client cannot replace `rom_alias` with `rom_path` or submit an arbitrary filesystem path.

Successful creation returns `201 Created`, the copied session snapshot, and a `Location: /v1/sessions/{id}` header.

## Stop and deletion

`POST /v1/sessions/{id}/stop` calls `Manager.Stop`. Stop remains idempotent and cancellation is handled by the owned session runner; the HTTP layer never touches an emulator directly.

`DELETE /v1/sessions/{id}` only removes retained terminal history. A queued/running/stopping session returns `409 session_not_terminal`. Stop it first and wait until its snapshot is `done` or `failed`.

Deletion removes the manager's retained snapshot/frame/replay references for that session ID. Durable history is still a separate concern (#11).

## Errors

API errors use a stable JSON envelope:

```json
{
  "error": {
    "code": "invalid_rom_alias",
    "message": "ROM alias is not configured"
  }
}
```

Responses intentionally do not pass internal manager/factory error strings through to the browser, because those strings may contain implementation details.

Common statuses:

- `400` invalid request or allowlisted launch configuration;
- `401` missing/wrong Bearer token;
- `404` missing session/frame, or all operator routes when auth is disabled;
- `409` attempt to delete a non-terminal session;
- `429` mutation rate limit exceeded;
- `500` internal start/read/stop/delete failure with a generic public message.

## Mutation limits

Defaults:

- body size: 16 KiB;
- rate: 60 mutation requests per minute per mounted operator handler;
- request context timeout: 5 seconds;
- move limit maximum: 100,000;
- shortlist maximum: 100.

These are server configuration, not client-controlled fields. Session execution itself is asynchronous: a successful create only needs to validate/configure and register the session before its owner goroutine performs ROM startup/gameplay.

## Next surface

Issue #10 should build the private operator console against this API. The console can discover safe ROM/profile/planner/model choices from `/v1/config`, launch with `pacing: realtime`, poll the copied session state, and display `/frame` without gaining any direct emulator access.

Issue #8 must define a narrower public DTO and separate read-only handler rather than exposing this authenticated operator contract publicly.
