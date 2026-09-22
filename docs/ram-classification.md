# OpenJev RAM semantic classification

`gamepilot-classify` is the second layer of GamePilot's RAM reverse-engineering pipeline. It consumes the deterministic JSON emitted by `gamepilot-discover` and asks a Jev-compatible `/v1/systemone` endpoint to assign bounded semantic meanings to the strongest candidate bytes.

The classifier never asks the model to invent an address. Addresses, deltas, divergence rates, and opposite-action relations come only from the deterministic investigator. OpenJev receives those facts and chooses among an explicit vocabulary such as `piece_x`, `piece_y`, `rotation`, `game_state`, `timer_or_counter`, or `insufficient_evidence`.

## Why a bounded classifier

The model is used only where the deterministic layer becomes ambiguous. For example:

- `left = -8`, `right = +8` at the same address is strong evidence for a horizontal coordinate, but it could still be a generic signed counter;
- `A = -1`, `B = +1` is consistent with a small rotation/orientation state, but not proof by itself;
- a byte that merely changes after any input may be an input latch, timer, animation counter, or game-state phase.

Every classification therefore retains the full option distribution and OpenJev confidence. A semantic mapping is promoted into `state` only when its selected probability and confidence cross configurable thresholds. `insufficient_evidence` is always an available answer.

Non-scalar meanings such as `board_or_tile` and `timer_or_counter` remain in the classification audit trail but are not emitted as misleading single-byte state fields.

## Run OpenJev

The client uses the Jev-compatible wire contract directly and expects a base URL ending in `/v1`. For a local OpenJev server listening on port 8000:

```sh
export OPENJEV_BASE_URL=http://localhost:8000/v1
export OPENJEV_MODEL=openjev-latest
```

If the server requires a bearer token:

```sh
export OPENJEV_API_KEY=...
```

## Discover, then classify

```sh
go run ./cmd/gamepilot-discover \
  -rom ./roms/tetris-rev1.gb \
  -out tetris-discovery.json

go run ./cmd/gamepilot-classify \
  -in tetris-discovery.json \
  -out tetris-schema.json
```

The classifier also accepts discovery JSON on stdin:

```sh
go run ./cmd/gamepilot-discover -rom ./roms/tetris-rev1.gb | \
  go run ./cmd/gamepilot-classify -in - -out tetris-schema.json
```

Useful classification controls:

```text
-base-url http://localhost:8000/v1
-model openjev-latest
-api-key-env OPENJEV_API_KEY
-min-probability 0.55
-min-confidence 0.10
-max-addresses 48
-batch-size 16
-timeout 60s
```

## Output

A successful Tetris classification can promote scalar state like:

```json
{
  "state": {
    "piece_x": {
      "semantic": "piece_x",
      "address": "0xC202",
      "encoding": "u8",
      "probability": 0.94,
      "confidence": 0.72,
      "evidence_score": 1.0
    },
    "rotation": {
      "semantic": "rotation",
      "address": "0xC203",
      "encoding": "u8",
      "probability": 0.91,
      "confidence": 0.65,
      "evidence_score": 0.95
    }
  }
}
```

The complete output also contains every attempted classification, its full probability distribution, rejection reason when it was not promoted, the source discovery configuration, ROM hash, and source frame.

## Profile-aware vocabulary

When the discovery report identifies a known profile, the classifier uses game-specific names:

- Tetris: `piece_x`, `piece_y`, `rotation`, `piece_type`, `score`, `lines`, `level`, `game_state`, and bounded diagnostic categories;
- Boxxle: `player_x`, `player_y`, `facing_or_direction`, `room_or_level`, `move_or_progress_counter`, `game_state`, and bounded diagnostic categories;
- unknown games: generic `controlled_x`, `controlled_y`, `orientation`, `object_id`, `score`, `progress`, `level`, and `game_state` semantics.

This profile context changes only the semantic vocabulary. It does not inject known addresses into classification.

## Current boundary

The current investigator is byte-level and action-driven, so the classifier is strongest at directly controlled state such as X/Y coordinates, rotation, facing, input state, and small game-state counters.

`score`, `lines`, `level`, object identity, and board regions are included in the vocabulary, but they should normally remain unresolved until GamePilot gains event probes that deliberately trigger scoring, line clears, level transitions, spawns, deaths, pickups, room transitions, and other semantic events. The abstention option and promotion thresholds are there specifically to avoid pretending those meanings are known too early.

The next useful extension is therefore event-driven experiments plus multi-byte inference (u16, packed BCD, enums, and contiguous board regions) feeding the same classifier interface.
