# Public spectator

GamePilot's public spectator is a separate read-only trust surface for watching live sessions. It is designed so internet-facing HTTP traffic never reaches the private operator API, ROM configuration, model credentials, filesystem paths, or session mutation handlers.

## Packages

- `runtime/spectatorapi` owns the public JSON/frame contract.
- `runtime/spectator` embeds the public HTML/CSS/JavaScript shell and mounts only the spectator routes.
- `runtime/websurfaces` composes the public spectator and private operator into different `http.Server` values with different bind addresses and HTTP limits.

The spectator reads copied data from `sessions.Manager`. Its `SessionReader` interface contains only `List`, `Snapshot`, and `Frame`; it has no launch, stop, delete, replay, ROM-catalog, or model-configuration capability.

## Public routes

```text
GET /                    embedded spectator page
GET /app.js              embedded dependency-free client
GET /styles.css          embedded styles
GET /v1/watch            selected live/recent public session summary
GET /v1/watch?session=ID explicitly select a retained public session
GET /v1/frame/{id}       latest PNG frame for a public session
GET /healthz             non-sensitive liveness
GET /readyz              non-sensitive readiness
```

There are no public POST, PUT, PATCH, or DELETE handlers. Private paths such as `/v1/config` and `/v1/sessions` are not delegated to the public API at all.

## Explicit public DTO

`sessions.Snapshot` is never serialized directly on the public surface. `runtime/spectatorapi` constructs an allowlisted `PublicSession` containing only:

- opaque session ID and public lifecycle status;
- sanitized profile, planner, and model labels;
- move/frame counters and frame availability;
- planner activity and bounded latency metadata;
- Tetris board, current/next piece, score, lines, level, ready/game-over state;
- the latest validated placement;
- elapsed time and public lifecycle timestamps.

The projection intentionally omits internal ROM paths, ROM hashes, cartridge metadata, raw errors/reasons, raw observation JSON, raw decision JSON, provider URLs, credentials, operator metadata, replay bytes, and configuration catalogs.

Model/planner labels are treated as presentation data rather than trusted configuration. Labels that look path-, URL-, credential-, or endpoint-shaped are replaced with a generic safe label before serialization.

## Live behavior

When no `session` query parameter is supplied, `/v1/watch` chooses an active session first and otherwise the most recently updated retained session. The completed-session tail is bounded to eight entries.

The browser polls the public watch document and latest frame independently. It follows the active session by default, allows selecting a recent completed session, and can return to follow-live mode. PNG frames use `Cache-Control: no-store`; duplicate frame sequence values are not re-rendered.

If the reader/backend is unavailable, the API fails closed with a generic `503` response:

```json
{"error":"spectator_unavailable"}
```

No backend error string is copied into that response.

## Separate public/private servers

`runtime/websurfaces.NewServers` constructs two servers around the same in-process `sessions.Manager` while keeping their HTTP route tables separate:

```go
servers, err := websurfaces.NewServers(websurfaces.Options{
    Manager: manager,
    Operator: operatorapi.Options{
        OperatorToken: os.Getenv("GAMEPILOT_OPERATOR_TOKEN"),
        ROMs:           romCatalog,
        Profiles:       profileCatalog,
        Models:         modelCatalog,
    },
    PublicAddr:  ":8080",
    PrivateAddr: "127.0.0.1:8081",
})
if err != nil {
    return err
}
```

Both returned `http.Server` values have non-zero read-header, read, write, idle, and header-size limits. The operator API additionally retains its mutation body/rate/time limits. The public server has no body-consuming/mutation routes.

A deployment can then start the two listeners independently and place different network policy in front of them. The private listener should stay on loopback, a private interface, or a VPN-only reverse proxy.

A reverse-proxy split looks like:

```text
internet :443
  -> public virtual host
  -> GamePilot public listener :8080

private/VPN :443
  -> operator virtual host
  -> GamePilot private listener 127.0.0.1:8081
```

Do not proxy the private listener through the public virtual host, and do not mount the operator handler beneath the spectator handler.

## Security headers

The public HTML/API surface uses `no-store`, a same-origin Content Security Policy, `Referrer-Policy: no-referrer`, `X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY`, and same-origin cross-origin isolation/resource policy headers. The frame endpoint only reflects `image/png`; arbitrary internal content types are rejected.

Tests enumerate private/mutation paths against the public handler and verify that sensitive internal fields never serialize into the public JSON.
