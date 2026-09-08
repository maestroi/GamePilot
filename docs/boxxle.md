# Boxxle (USA/Europe)

This profile supports exactly one ROM:

- Title: `BOXXLE`
- Cartridge: 32 KiB ROM only
- SHA-256: `c859503342db1f86dadeb7e6f3d8a8a2918e9b6a7c8756311b7cc7bb0a7e892f`

GamePilot refuses other hashes rather than assuming the same tiles and OAM layout apply.

Boxxle’s title-to-menu transition uses a “nitro copy” (`LD (HL), A` / `CP (HL)` / `JR NZ`) into echo RAM. That loop hangs on Gomeboy 1.0.0 because WRAM writes to `EExx` updated `CExx` but not `EExx`. GamePilot therefore builds against the local `../gomeboy` checkout where echo RAM mirrors `C000–DDFF` onto `E000–FDFF` without clearing extra address bits.

## Startup

`StartRoom1` boots Type-A-style into set 1 room 1:

1. Wait 240 frames for `PRESS START BUTTON`
2. Start
3. A on `MENU / PLAY`
4. A on `WHICH? / PLAY`
5. Wait until the warehouse is ready (wall tile at cell 0,0 and an idle Willy sprite)

Do not press Start after the warehouse appears; that opens the in-game `KEEP GOING / RETRY` overlay.

## Established state mapping

Warehouse cells are 16×16 pixels (2×2 background tiles) starting at tile column 1 of map `$9800`.

| State | Where | Notes |
| --- | --- | --- |
| Wall | BG tile `$A8` (and other non-floor tiles) | Top-left of a 2×2 brick cell; unknown tiles default to wall so search cannot walk through `$AC` bricks |
| Box | BG tile `$A4` | Cross-braced crate |
| Box on goal | BG tile `$AC` | Settled crate covering a goal |
| Goal | BG tile `$A0` | Floor dot |
| Floor | BG tile `$D4` | Empty warehouse tile |
| Player | OAM sprite 0 | `FE00` Y, `FE01` X, `FE02` tile `$80–$87` |
| Move count | `C0A1` | Increments once per completed cell |
| Set number | `C2D0` | `1` on room 1-1 |

Player cell:

```text
cell_x = (OAM_X - 16) / 16
cell_y = (OAM_Y - 16) / 16
```

Pushed crates leave the background map and are drawn as OAM sprites. Observe overlays sprites whose tiles are `$A4–$A7` (crate) or `$AC–$AF` (crate on a goal). Settled crates on goals can also appear as background tile `$AC`.

The planner searches for a shortest solution and emits the next `Move{Direction}` (`up`, `down`, `left`, `right`). If the warehouse is too large to finish searching, it falls back to a one-ply greedy. The controller holds that D-pad until the cell changes, then waits until idle. If Willy is still in a push pose from the previous step, the controller waits that animation out before the next press. Planners never own frame timing.

Live `-planner serve` with this ROM starts a Boxxle session runner. The public spectator shows the Game Boy frame plus the decoded warehouse; the operator console launches the search planner.
