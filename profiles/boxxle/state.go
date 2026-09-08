package boxxle

const (
	GridRows    = 9
	GridColumns = 10

	bgMapBase   uint16 = 0x9800
	bgMapStride uint16 = 32
	gridTileX          = 1
	cellTiles          = 2
	cellPixels         = 16
	oamXOrigin         = 16
	oamYOrigin         = 16

	tileWall      = 0xA8
	tileBox       = 0xA4
	tileGoal      = 0xA0
	tileBoxOnGoal = 0xAC
	tileFloor     = 0xD4

	playerOAMY    uint16 = 0xFE00
	playerOAMX    uint16 = 0xFE01
	playerOAMTile uint16 = 0xFE02

	moveCountAddr uint16 = 0xC0A1
	setNumberAddr uint16 = 0xC2D0
)

// Cell is one 16x16 warehouse square.
type Cell uint8

const (
	Empty Cell = iota
	Wall
	Box
	Goal
	BoxOnGoal
	Player
	PlayerOnGoal
)

func (c Cell) String() string {
	switch c {
	case Wall:
		return "#"
	case Box:
		return "$"
	case Goal:
		return "."
	case BoxOnGoal:
		return "*"
	case Player:
		return "@"
	case PlayerOnGoal:
		return "+"
	default:
		return " "
	}
}

// Direction is one grid step. Boxxle only accepts cardinal walking.
type Direction string

const (
	DirUp    Direction = "up"
	DirDown  Direction = "down"
	DirLeft  Direction = "left"
	DirRight Direction = "right"
)

func (d Direction) Delta() (dx, dy int, ok bool) {
	switch d {
	case DirUp:
		return 0, -1, true
	case DirDown:
		return 0, 1, true
	case DirLeft:
		return -1, 0, true
	case DirRight:
		return 1, 0, true
	default:
		return 0, 0, false
	}
}

// Move is the Boxxle action contract: one warehouse step.
type Move struct {
	Direction Direction `json:"direction"`
}

// Observation is the structured, image-free warehouse state. Walls, boxes, and
// goals come from the background tilemap's 16x16 cells; the walker comes from
// OAM sprite 0, matching how the ROM draws a moving Willy.
type Observation struct {
	Frame   uint64                      `json:"frame"`
	Grid    [GridRows][GridColumns]Cell `json:"grid"`
	PlayerX int                         `json:"player_x"`
	PlayerY int                         `json:"player_y"`
	Moves   int                         `json:"moves"`
	Set     int                         `json:"set"`
	Ready   bool                        `json:"ready"`
	Solved  bool                        `json:"solved"`
	Playing bool                        `json:"playing"`
}

type memoryReader interface {
	Peek8(addr uint16) byte
	PeekInto(addr uint16, dst []byte)
	FrameCount() uint64
}

// Observe decodes one warehouse snapshot using side-effect-free inspection.
func Observe(mem memoryReader) Observation {
	var obs Observation
	obs.Frame = mem.FrameCount()
	obs.Moves = int(mem.Peek8(moveCountAddr))
	obs.Set = int(mem.Peek8(setNumberAddr))

	obs.PlayerX, obs.PlayerY = playerCell(mem)
	playing := isPlaying(mem)
	obs.Playing = playing
	obs.Ready = playing && playerAligned(mem)

	for row := 0; row < GridRows; row++ {
		for col := 0; col < GridColumns; col++ {
			obs.Grid[row][col] = decodeCell(mem, col, row)
		}
	}
	overlayOAMBoxes(mem, &obs.Grid)

	if playing && inBounds(obs.PlayerX, obs.PlayerY) {
		switch obs.Grid[obs.PlayerY][obs.PlayerX] {
		case Goal:
			obs.Grid[obs.PlayerY][obs.PlayerX] = PlayerOnGoal
		case Empty:
			obs.Grid[obs.PlayerY][obs.PlayerX] = Player
		}
	}

	obs.Solved = playing && solved(obs.Grid)
	return obs
}

func overlayOAMBoxes(mem memoryReader, grid *[GridRows][GridColumns]Cell) {
	for i := 0; i < 40; i++ {
		base := uint16(0xFE00 + i*4)
		if !isBoxSprite(mem.Peek8(base + 2)) {
			continue
		}
		x := (int(mem.Peek8(base+1)) - oamXOrigin) / cellPixels
		y := (int(mem.Peek8(base)) - oamYOrigin) / cellPixels
		if !inBounds(x, y) {
			continue
		}
		if grid[y][x] == Goal || isBoxOnGoalSprite(mem.Peek8(base+2)) {
			grid[y][x] = BoxOnGoal
		} else if grid[y][x] != Wall {
			grid[y][x] = Box
		}
	}
}

func decodeCell(mem memoryReader, col, row int) Cell {
	switch mem.Peek8(cellTileAddr(col, row)) {
	case tileBox:
		return Box
	case tileGoal:
		return Goal
	case tileBoxOnGoal:
		return BoxOnGoal
	case tileFloor:
		return Empty
	default:
		return Wall
	}
}

func cellTileAddr(col, row int) uint16 {
	tx := gridTileX + col*cellTiles
	ty := row * cellTiles
	return bgMapBase + uint16(ty)*bgMapStride + uint16(tx)
}

func playerCell(mem memoryReader) (x, y int) {
	return (int(mem.Peek8(playerOAMX)) - oamXOrigin) / cellPixels,
		(int(mem.Peek8(playerOAMY)) - oamYOrigin) / cellPixels
}

func playerAligned(mem memoryReader) bool {
	x := int(mem.Peek8(playerOAMX)) - oamXOrigin
	y := int(mem.Peek8(playerOAMY)) - oamYOrigin
	return x >= 0 && y >= 0 && x%cellPixels == 0 && y%cellPixels == 0
}

func isBoxSprite(tile byte) bool {
	return tile >= tileBox && tile <= tileBox+3 || tile >= tileBoxOnGoal && tile <= tileBoxOnGoal+3
}

func isBoxOnGoalSprite(tile byte) bool {
	return tile >= tileBoxOnGoal && tile <= tileBoxOnGoal+3
}

func playerPushing(mem memoryReader) bool {
	tile := mem.Peek8(playerOAMTile)
	return tile >= 0x94 && tile <= 0x97
}

func isPlaying(mem memoryReader) bool {
	return mem.Peek8(cellTileAddr(0, 0)) == tileWall && mem.Peek8(playerOAMTile)&0xE0 == 0x80
}

func inBounds(x, y int) bool {
	return x >= 0 && y >= 0 && x < GridColumns && y < GridRows
}

func solved(grid [GridRows][GridColumns]Cell) bool {
	boxes := 0
	goals := 0
	for row := 0; row < GridRows; row++ {
		for col := 0; col < GridColumns; col++ {
			switch grid[row][col] {
			case Box:
				boxes++
			case Goal, PlayerOnGoal:
				goals++
			}
		}
	}
	return boxes == 0 && goals == 0
}

// RenderGrid is a compact warehouse dump for CLI output and tests.
func RenderGrid(obs Observation) string {
	out := make([]byte, 0, GridRows*(GridColumns+1))
	for row := 0; row < GridRows; row++ {
		for col := 0; col < GridColumns; col++ {
			out = append(out, obs.Grid[row][col].String()[0])
		}
		if row+1 < GridRows {
			out = append(out, '\n')
		}
	}
	return string(out)
}
