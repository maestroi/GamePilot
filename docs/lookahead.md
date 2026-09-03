# Tetris two-ply lookahead

GamePilot's deterministic lookahead planner uses the next-piece preview already decoded from the supported Tetris Rev 1 ROM. It does not predict unknown pieces and it does not alter the controller.

## Evaluation

For each strategically distinct legal placement of the current piece:

1. simulate the current placement and line clears;
2. place the known preview piece on that resulting board in every legal way;
3. keep the best deterministic preview reply;
4. score the final leaf board with the existing aggregate-height, holes, bumpiness, and line-clear weights;
5. credit line clears from both plies;
6. order first moves deterministically.

A first move that leaves no legal placement for the known preview piece receives a large finite penalty, so every survivable first move ranks above a forced next-piece top-out.

## Strategically distinct candidates

Raw ROM rotations can be equivalent. For example, every O rotation has identical geometry, while I/S/Z each have pairs of identical orientations. `UniqueLegalSimulations` collapses first moves that produce the exact same settled board and keeps the normal deterministic cheaper/stabler input representation.

This matters for LLM prompting: a shortlist of 10 should contain 10 different resulting positions rather than several aliases of the same move.

## Standalone planner

Run the deterministic two-ply planner directly:

```bash
go run ./cmd/gamepilot \
  -rom ./roms/tetris.gb \
  -planner lookahead \
  -pieces 25 \
  -replay-out lookahead-25.json
```

Each move log includes the chosen current placement, the preview piece, the best simulated preview reply, total two-ply line clears, and the leaf score.

## LLM shortlist

The LLM planner uses the same ordered two-ply candidate list and sends only the best N entries. The default is:

```text
-llm-candidates 10
```

Each prompt candidate contains:

- current placement
- immediate line clears and board metrics
- best deterministic preview-piece reply
- total line clears across both plies
- final leaf-board metrics
- deterministic two-ply score

The model must still return only the current-piece `Placement{Rotation, TargetColumn}`. The preview reply is advisory simulation context; it is not queued or executed automatically because the ROM state is observed again after every real placement.

The shortlist size can be tuned for latency/quality experiments:

```bash
-llm-candidates 6
-llm-candidates 10
-llm-candidates 16
```

Smaller values reduce prompt prefill. Larger values give the model more alternatives. Replay remains the comparison/verification mechanism for all planner variants.
