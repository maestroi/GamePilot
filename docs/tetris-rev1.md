# Tetris Rev 1 memory mapping

This profile supports exactly one ROM revision:

- Title: `Tetris (World) (Rev 1)` / `Tetris (JUE) (V1.1) [!]`
- SHA-256: `0d6535aef23969c7e5af2b077acaddb4a445b3d0df7bf34c8acef07b51b015c3`
- SHA-1: `74591cc9501af93873f9a5d3eb12da12c0723bbc`

The SHA-1 is the revision named by the complete disassembly at <https://github.com/kaspermeerts/tetris>. The SHA-256 is the corresponding Rev 1 dump listed by the Game Boy Hardware Database at <https://gbhwdb.gekkio.fi/cartridges/DMG-TRA-1/gekkio-1.html>.

GamePilot refuses other hashes rather than assuming the same addresses apply.

## Established state mapping

The addresses below come from the disassembly's actual gameplay routines, not framebuffer heuristics.

| State | Address | Evidence in the disassembly |
| --- | ---: | --- |
| Settled playfield | `C802`, 18 rows x 10 columns, stride `0x20` | `FillPlayingFieldAndWipe` initializes exactly this region; collision and line-clear routines read the same WRAM buffer. |
| Empty cell tile | `0x2F` | Gameplay initialization fills the field with the charmap's space tile and collision compares against space. |
| Active visibility | `C200` | Active sprite struct; `0x80` hides the piece during lock/game over. |
| Active Y anchor | `C201` | `DropPiece` advances it by 8 pixels. |
| Active X anchor | `C202` | `RotateAndShiftPiece` moves it by 8 pixels. |
| Active sprite/piece ID | `C203` | `NextPiece` installs the preview ID here; low two bits are changed by rotation. |
| Preview piece ID | `C213` | `NextPiece` reads the preview ID from this byte. |
| Score | `C0A0..C0A2` | `wScore`; three little-endian packed-BCD digit pairs. |
| Lock phase | `FF98` | Gameplay lock pipeline uses values 0 through 3. |
| Lines | `FF9E..FF9F` | `hLines`; little-endian packed BCD. |
| Level | `FFA9` | `hLevel`. |
| Game state | `FFE1` | `hGameState`; state 0 is normal gameplay, state 1 begins game over. |
| Wipe/line-clear phase | `FFE3` | `hWipeCounter`; zero when normal piece control is active. |

## Tetromino IDs

The ROM's `SpriteList` stores four consecutive orientations for each tetromino. Therefore the low two bits are rotation and the upper bits identify the piece family:

| Base ID | Piece |
| ---: | --- |
| `0x00` | L |
| `0x04` | J |
| `0x08` | I |
| `0x0C` | O |
| `0x10` | S |
| `0x14` | Z |
| `0x18` | T |

The board buffer contains settled blocks only. The currently falling tetromino is represented separately by the active sprite struct, so GamePilot keeps it separate in `Observation.CurrentPiece` rather than merging it into the board.

The original Game Boy Tetris playfield for this revision is 18x10 in memory. GamePilot uses those exact dimensions instead of forcing the profile into a generic 20x10 model.

## Placement coordinates

`Placement.Rotation` is the ROM's raw low-two-bit orientation index (`0..3`). `Placement.TargetColumn` is the **leftmost occupied playfield column** after that rotation. This is intentionally a game-level coordinate rather than the sprite anchor stored at `C202`.

The mapping is derived from the Rev 1 renderer and collision lookup: tetromino sprite definitions use an X offset of `-16`, Game Boy OAM X coordinates have an 8-pixel bias, the playfield starts at tilemap column 2, and each matrix cell is 8 pixels wide. As a result, anchor X `47` places tetromino matrix column 0 at playfield column 0, and every left/right move changes the anchor by exactly 8 pixels.

The controller uses the exact occupied 4x4 sprite matrices from the ROM to convert the requested leftmost occupied column into the required anchor X. This matters for orientations whose first occupied matrix column is not column 0 (for example the O piece and vertical I piece).

Rotation is also ROM-specific: A decrements the raw orientation modulo 4 and B increments it modulo 4. The controller verifies the expected `C203` orientation after every rotation pulse and verifies `C202` after every horizontal pulse; a blocked move is returned as an error instead of being assumed successful.
