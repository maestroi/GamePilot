# Jev Tetris player

This is the first GamePilot loop where a Jev-compatible model can actually play a Game Boy game.

The player deliberately works at the existing Tetris game-action boundary rather than exposing raw joypad frames to the model. For each falling piece GamePilot:

1. reads the current memory-derived Tetris observation;
2. enumerates a bounded shortlist of legal placements;
3. saves the exact Gomeboy checkpoint;
4. restores that checkpoint and executes each candidate placement through the real deterministic controller;
5. records the resulting board/state from the emulator;
6. restores the original live checkpoint;
7. asks Jev/OpenJev to choose exactly one of those verified outcomes;
8. rejects any choice that was not in the verified option set;
9. executes the chosen placement on the live emulator;
10. repeats for the next piece.

The model therefore never invents controller commands or placements. It decides between concrete outcomes that GamePilot has already proven the emulator can reach.

## Run

Start a Jev-compatible server first. The client uses the same `POST /v1/systemone` contract as the RAM semantic classifier from PR #27.

For a local server:

```sh
export OPENJEV_BASE_URL=http://localhost:8000/v1
export OPENJEV_MODEL=openjev-latest

go run ./cmd/gamepilot-jev \
  -rom ./roms/tetris-rev1.gb \
  -pieces 100
```

If the server requires authentication:

```sh
export OPENJEV_API_KEY=...
```

Useful flags:

```text
-rom          Tetris Rev 1 ROM path
-pieces       maximum pieces to play (default 25)
-base-url     Jev-compatible base URL (default OPENJEV_BASE_URL or http://localhost:8000/v1)
-model        model name (default OPENJEV_MODEL or openjev-latest)
-api-key-env  bearer-token environment variable (default OPENJEV_API_KEY)
-timeout      timeout per model decision (default 60s)
-candidates   verified placements exposed to Jev per piece (default 12)
```

Example move output:

```text
Move 7: piece=T rotation=1 target_column=4 choice=placement_03 p=0.7210 confidence=0.6400 candidates=12/23
```

At the end the command prints the final structured Tetris observation.

## What Jev sees

The decision state contains:

- the current memory-derived observation;
- every verified shadow outcome;
- resulting board state;
- score and line deltas;
- aggregate stack height;
- hole count;
- bumpiness;
- game-over status.

The choice criteria contain only generated IDs such as `placement_00`, `placement_01`, and so on. Jev returns a probability distribution over those IDs.

The live emulator is restored to the original checkpoint before the model request is made. Unit tests verify that RAM/frame/controller state is unchanged after shadow search and that an invented option is rejected.

## Why placements first

GamePilot already has a deterministic placement controller that converts:

```text
rotation + target column
```

into verified rotate/shift/drop inputs.

Using this boundary gets a Jev model playing immediately without making it solve frame timing, button debouncing, wall collisions, and lock timing at the same time. A later controller layer can expose primitive inputs when that is useful for games that do not have a clean placement abstraction.

## Current boundary

The shortlist is bounded using GamePilot's existing deterministic two-ply candidate generation before the expensive real-emulator shadow runs. Jev makes the final decision among those verified candidates.

This PR intentionally uses the hand-verified Tetris observation/profile. The automatic RAM discovery/classification work in PR #27 can progressively replace those known mappings later without changing the Jev decision loop.
