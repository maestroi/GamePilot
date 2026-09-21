# Deterministic RAM discovery

`gamepilot-discover` is the first layer for deriving game state from Game Boy RAM without hard-coding addresses first. It does not try to name state yet. Its job is to produce clean, ranked evidence that a later semantic classifier (for example OpenJev) can label.

## Why compare against an idle control

A normal before/after RAM diff is noisy because timers, animation counters, sound state, and the game loop change memory even when the player does nothing. GamePilot instead runs two traces from the exact same save-state checkpoint:

1. an idle control trace;
2. the same number of frames with one controlled input.

The two traces are compared frame-by-frame. An address is a candidate only when the input trace differs from what the idle trace did at the same frame. The emulator is restored to the original checkpoint after discovery, so probing does not alter the live state.

By default the scanner reads software-owned memory only:

- WRAM: `0xC000..0xDFFF`
- HRAM: `0xFF80..0xFFFE`

This includes the known Tetris Rev 1 state while avoiding volatile hardware registers.

## Run it

For a supported Tetris Rev 1 ROM:

```sh
go run ./cmd/gamepilot-discover \
  -rom ./roms/tetris-rev1.gb \
  -out tetris-discovery.json
```

The command auto-runs the existing deterministic startup for Tetris and Boxxle. For an unknown ROM, use `-startup none` and optionally `-warmup N`; in practice a new game should eventually get a tiny startup adapter so probes begin in meaningful gameplay state.

Useful controls:

```text
-inputs left,right,up,down,a,b
-hold-frames 1
-release-frames 2
-trials 3
-trial-advance 2
-top 32
```

## Report shape

Each action contains ranked byte candidates with:

- divergence rate from the idle control;
- dominant signed byte delta;
- delta consistency;
- a combined score.

The report also contains cross-action `opposite_delta` relations. These are particularly useful for coordinate-like state and increment/decrement counters. On Tetris Rev 1, for example, left/right should provide strong evidence for the active X byte and A/B should provide strong evidence for the raw orientation byte, without those addresses being supplied to the investigator.

The JSON is intentionally model-friendly. The next layer can ask a semantic classifier questions such as "which candidate is most likely the active piece X coordinate?" while keeping memory collection and causality testing deterministic.

## Current boundary

This first slice discovers byte-level causal state. It does not yet infer multi-byte integers, packed BCD fields, board-shaped regions, or semantic names. Those should be layered on top of the same control-trace evidence rather than mixed into emulator probing.
