# Tetris replay format

GamePilot replays are versioned JSON records of **decoded Tetris state plus game-level placements**. They are designed to prove reproducibility without depending on screenshots, wall-clock sleeps, or Gomeboy save-state internals.

## Header

Each replay records:

- `format_version`: replay schema version, currently `1`
- `profile`: currently `tetris`
- `rom_sha256`: exact supported Rev 1 ROM hash
- `startup`: deterministic boot contract, currently `type_a_level_0`
- `initial`: the first ready observation after startup
- `moves`: ordered placement records

The exact ROM hash remains mandatory because all Tetris addresses and semantics are revision-specific.

## State boundaries

A replay state contains:

```json
{
  "state_hash": "...sha256...",
  "observation": {
    "frame": 265,
    "board": [],
    "current_piece": {},
    "next_piece": {},
    "score": 0,
    "level": 0,
    "lines": 0,
    "ready": true,
    "game_over": false,
    "game_state": 0
  }
}
```

The canonical state hash includes the settled board, current and next piece data, score, level, lines, ready/game-over flags, and game state. It deliberately excludes `frame`.

Frame numbers are still recorded and verified exactly. Keeping the two checks separate means a replay failure can distinguish:

- same decoded game state reached on the wrong frame; or
- a true game-state divergence such as a different board cell, score, piece, or line count.

The hash payload is versioned internally so its semantics can evolve only through an explicit code/schema change.

## Move records

Every move contains:

```json
{
  "index": 1,
  "placement": {
    "rotation": 2,
    "target_column": 0
  },
  "before": {
    "state_hash": "...",
    "observation": {}
  },
  "after": {
    "state_hash": "...",
    "observation": {}
  }
}
```

`Placement` has exactly the same semantics as the live controller: raw ROM rotation `0..3` and the leftmost occupied playfield column.

Replay append validation requires each move's `before` state to equal the prior recorded state. This prevents producing a replay with gaps or accidentally mixing runs.

## Verification

Replay verification performs these steps:

1. validate the replay schema and every recorded state's canonical hash;
2. require the loaded ROM SHA-256 to match the replay header;
3. deterministically boot Type A level 0;
4. compare the fresh initial observation and frame with the replay;
5. execute each recorded `Placement` through the normal strict controller;
6. compare the resulting observation hash and frame after every move;
7. stop at the first mismatch.

Mismatch errors report the state hash and relevant decoded differences such as frame, score, lines, level, current/next piece, ready/game-over state, and the first differing board cell.

The verifier does **not** rerun the heuristic planner. This is important: a replay proves that an action sequence is reproducible independently of the planner that originally chose it.

## CLI

Record a heuristic run:

```bash
go run ./cmd/gamepilot \
  -rom ./roms/tetris.gb \
  -planner heuristic \
  -pieces 25 \
  -replay-out tetris-25.json
```

Verify it from a fresh emulator boot:

```bash
go run ./cmd/gamepilot \
  -rom ./roms/tetris.gb \
  -planner replay \
  -replay-in tetris-25.json
```

Manual placement mode can also write a replay with `-replay-out`.

Replay JSON contains no ROM bytes and can be inspected as ordinary structured test/debug data.
