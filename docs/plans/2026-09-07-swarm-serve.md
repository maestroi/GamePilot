# GamePilot Swarm Serve Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a `-planner serve` mode that starts the existing public spectator and private operator surfaces, then package that process for Swarm behind `gamepilot.maestroi.cc` (inspect) and `gamepilot.labstack.cc` (admin).

**Architecture:** One in-process `sessions.Manager` owns live play. `runtime/websurfaces` already builds two `http.Server` values with separate route tables. The CLI wires catalogs/token/LLM aliases into that constructor. Docker publishes one image; Traefik routes two Hostnames to two ports on the same replica. The ROM stays a host bind-mount.

**Tech Stack:** Go 1.26 CLI, existing `websurfaces`/`operatorapi`/`spectator`, GHCR, Docker Swarm + Traefik `web` overlay.

---

### Task 1: Serve config validation

**Files:**
- Create: `cmd/gamepilot/serve.go`
- Test: `cmd/gamepilot/serve_test.go`
- Modify: `cmd/gamepilot/main.go`

**Step 1: Write the failing test**

`serveOptions.validate` rejects empty token, empty ROM path, and identical public/private addresses. `operatorCatalog` always includes Tetris heuristic/lookahead; `llm` + model alias only when a model id is set.

**Step 2: Run test to verify it fails**

Run: `go test ./cmd/gamepilot -count=1`

**Step 3: Write minimal `serveOptions` + `runServe`**

`runServe` builds `NewTetrisManager` with optional `LLMPlannerFactory`, `websurfaces.NewServers`, then listens on both addresses until SIGINT/SIGTERM.

**Step 4: Wire `-planner serve` plus `-public-addr`, `-private-addr`, `-rom-alias`, `-operator-token-env`. Reuse existing `-rom` and `-llm-*` flags. Empty private addr keeps the library loopback default; Docker overrides to `:8081`.

**Step 5: `go test ./...`**

---

### Task 2: Image + GHCR

**Files:**
- Create: `deploy/Dockerfile`
- Create: `.dockerignore`
- Create: `.github/workflows/publish.yml`

Image: `ghcr.io/maestroi/gamepilot:<full SHA>` and `:latest`. No ROM in the build context. CMD serves `:8080` public and `:8081` private.

---

### Task 3: Swarm stack

**Files:**
- Create: `deploy/stack.yml`

One replica, two Traefik routers:

- `Host(\`gamepilot.maestroi.cc\`)` → port 8080, `websecure` + TLS (public inspect)
- `Host(\`gamepilot.labstack.cc\`)` → port 8081, `web` (private admin)

ROM: `/opt/gamepilot/roms/tetris.gb:/roms/tetris.gb:ro`. Token/LLM via env, not git.

---

### Task 4: Docs

Update README live-sessions / run section with serve command and the two DNS names.
