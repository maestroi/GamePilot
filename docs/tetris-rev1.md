# Tetris Rev 1 memory mapping

This profile supports exactly one ROM revision:

- Title: `Tetris (World) (Rev 1)` / `Tetris (JUE) (V1.1) [!]`
- SHA-256: `0d6535aef23969c7e5af2b077acaddb4a445b3d0df7bf34c8acef07b51b015c3`
- SHA-1: `74591cc9501af93873f9a5d3eb12da12c0723bbc`

The SHA-1 is the revision named by the complete disassembly at <https://github.com/kaspermeerts/tetris>. The SHA-256 is the corresponding Rev 1 dump listed by the Game Boy Hardware Database at <https://gbhwdb.gekkio.fi/cartridges/DMG-TRA-1/ueda-04.html>.

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
