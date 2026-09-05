# Private operator console

`runtime/operatorconsole` is the browser UI for the private GamePilot control plane. It is intentionally a thin client over `runtime/operatorapi`: the browser never receives an emulator pointer, ROM filesystem path, model provider URL, API key, or server-side operator token.

## Mounting

Compose the console with the private API on the same origin. `NewHandlerWithReplay` keeps the normal operator routes and adds the authenticated terminal-replay download used by the console:

```go
api, err := operatorapi.NewHandlerWithReplay(operatorapi.Options{
    Manager:       manager,
    OperatorToken: os.Getenv("GAMEPILOT_OPERATOR_TOKEN"),
    // ROM/profile/planner/model allowlists omitted here.
})
if err != nil {
    return err
}

handler, err := operatorconsole.NewHandler(api)
if err != nil {
    return err
}

return http.ListenAndServe("127.0.0.1:8080", handler)
```

Keep this listener behind the same private network/VPN/reverse-proxy boundary as the operator API. The future public spectator must use its own read-only handler and DTO.

## Authentication model

The embedded HTML, CSS, and JavaScript are deployment-agnostic and contain no credentials or private paths. The shell can therefore be served before API authentication without disclosing operator data.

The operator enters the configured Bearer token in the browser. The console stores it in `sessionStorage`, so it is scoped to the current browser tab/session rather than durable local storage. Every `/v1/...` request then sends the normal `Authorization: Bearer ...` header already required by `runtime/operatorapi`.

Disconnect clears the token and live UI state. A `401` from the API also returns the UI to the authentication screen.

## Live behavior

The console discovers all safe choices from `GET /v1/config`; it does not hard-code ROM paths, planner/model provider settings, or profile internals.

The first UI supports:

- ROM/profile/planner selection from the allowlisted catalog;
- model alias and shortlist size for model-backed planners;
- continuous or move-limited sessions;
- fast/realtime pacing, defaulting the form to realtime;
- replay-recording selection;
- active-first session list with recent terminal sessions;
- live 160x144 framebuffer polling through the authenticated frame endpoint;
- Tetris settled-board/current/next-piece rendering from structured observations;
- score, lines, level, frame, moves, planner activity/latency, and latest decision;
- bounded in-browser activity events derived from immutable snapshot changes;
- stop for active sessions, replay download for retained terminal sessions, and terminal-session deletion.

The event list is presentation state only; it is not a durable server log. Durable history/artifact retention remains issue #11.

## Polling and stale frames

The browser polls copied session state and the latest retained frame. It never asks the emulator to step and it cannot backpressure the owner goroutine. Re-fetching the same `X-GamePilot-Sequence` does not replace the displayed image, and the UI marks a running session's framebuffer stale after a short period without a new frame publication.

## Security headers

The console handler sets a restrictive same-origin Content Security Policy, disables framing, uses `nosniff`, and sends a no-referrer policy. The UI has no inline scripts or third-party assets, so it does not need `unsafe-inline` or external script/style origins.
