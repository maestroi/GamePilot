package tetris

import "fmt"

const (
	BoardRows    = 18
	BoardColumns = 10

	playfieldBase   uint16 = 0xC802
	playfieldStride uint16 = 0x20
	emptyTile        byte   = 0x2F

	activeVisibleAddr uint16 = 0xC200
	activeYAddr       uint16 = 0xC201
	activeXAddr       uint16 = 0xC202
	activePieceAddr   uint16 = 0xC203
	previewPieceAddr  uint16 = 0xC213

	scoreAddr       uint16 = 0xC0A0
	lockPhaseAddr   uint16 = 0xFF98
	linesAddr       uint16 = 0xFF9E
	levelAddr       uint16 = 0xFFA9
	wipeCounterAddr uint16 = 0xFFE3
	gameStateAddr   uint16 = 0xFFE1
)

// Cell is intentionally compact for planner payloads.
type Cell uint8

const (
	Empty Cell = iota
	Filled
)

// PieceKind is the tetromino family encoded by the original Tetris sprite ID.
type PieceKind string

const (
	PieceL       PieceKind = "L"
	PieceJ       PieceKind = "J"
	PieceI       PieceKind = "I"
	PieceO       PieceKind = "O"
	PieceS       PieceKind = "S"
	PieceZ       PieceKind = "Z"
	PieceT       PieceKind = "T"
	PieceUnknown PieceKind = "unknown"
)

// Piece describes one Tetris piece. AnchorX/AnchorY are the ROM's sprite anchor
// coordinates in pixels; they are deliberately not mislabeled as board cells.
type Piece struct {
	Kind     PieceKind `json:"kind"`
	Rotation int       `json:"rotation"`
	AnchorX  uint8     `json:"anchor_x,omitempty"`
	AnchorY  uint8     `json:"anchor_y,omitempty"`
	Visible  bool      `json:"visible,omitempty"`
}

// Observation is the structured, image-free state exposed by the Tetris
// profile. Board contains settled playfield cells; the falling piece is kept
// separately in CurrentPiece, matching the ROM's own representation.
type Observation struct {
	Frame        uint64                        `json:"frame"`
	Board        [BoardRows][BoardColumns]Cell `json:"board"`
	CurrentPiece Piece                         `json:"current_piece"`
	NextPiece    Piece                         `json:"next_piece"`
	Score        int                           `json:"score"`
	Level        int                           `json:"level"`
	Lines        int                           `json:"lines"`
	Ready        bool                          `json:"ready"`
	GameOver     bool                          `json:"game_over"`
	GameState    uint8                         `json:"game_state"`
}

type memoryReader interface {
	Peek8(addr uint16) byte
	PeekInto(addr uint16, dst []byte)
	FrameCount() uint64
}

// Observe decodes one stable Tetris observation using side-effect-free memory
// inspection. The caller must not step the emulator concurrently.
func Observe(mem memoryReader) (Observation, error) {
	var obs Observation
	obs.Frame = mem.FrameCount()

	for row := 0; row < BoardRows; row++ {
		var cells [BoardColumns]byte
		mem.PeekInto(playfieldBase+uint16(row)*playfieldStride, cells[:])
		for column, tile := range cells {
			if tile != emptyTile {
				obs.Board[row][column] = Filled
			}
		}
	}

	obs.CurrentPiece = decodePiece(mem.Peek8(activePieceAddr))
	obs.CurrentPiece.AnchorX = mem.Peek8(activeXAddr)
	obs.CurrentPiece.AnchorY = mem.Peek8(activeYAddr)
	obs.CurrentPiece.Visible = mem.Peek8(activeVisibleAddr) == 0

	obs.NextPiece = decodePiece(mem.Peek8(previewPieceAddr))

	var score [3]byte
	mem.PeekInto(scoreAddr, score[:])
	value, err := decodePackedBCD(score[:])
	if err != nil {
		return Observation{}, fmt.Errorf("tetris: decode score: %w", err)
	}
	obs.Score = value

	var lines [2]byte
	mem.PeekInto(linesAddr, lines[:])
	value, err = decodePackedBCD(lines[:])
	if err != nil {
		return Observation{}, fmt.Errorf("tetris: decode lines: %w", err)
	}
	obs.Lines = value
	obs.Level = int(mem.Peek8(levelAddr))

	obs.GameState = mem.Peek8(gameStateAddr)
	lockPhase := mem.Peek8(lockPhaseAddr)
	wipeCounter := mem.Peek8(wipeCounterAddr)
	obs.Ready = obs.GameState == 0 && obs.CurrentPiece.Visible && lockPhase == 0 && wipeCounter == 0
	obs.GameOver = isGameOverState(obs.GameState)

	return obs, nil
}

func decodePiece(spriteIndex byte) Piece {
	piece := Piece{Rotation: int(spriteIndex & 0x03)}
	switch spriteIndex &^ 0x03 {
	case 0x00:
		piece.Kind = PieceL
	case 0x04:
		piece.Kind = PieceJ
	case 0x08:
		piece.Kind = PieceI
	case 0x0C:
		piece.Kind = PieceO
	case 0x10:
		piece.Kind = PieceS
	case 0x14:
		piece.Kind = PieceZ
	case 0x18:
		piece.Kind = PieceT
	default:
		piece.Kind = PieceUnknown
	}
	return piece
}

// The ROM stores decimal values as little-endian pairs of packed BCD digits:
// byte 0 contains ones/tens, byte 1 hundreds/thousands, and so on.
func decodePackedBCD(data []byte) (int, error) {
	value := 0
	place := 1
	for _, pair := range data {
		lo := int(pair & 0x0F)
		hi := int(pair >> 4)
		if lo > 9 || hi > 9 {
			return 0, fmt.Errorf("invalid packed BCD byte 0x%02x", pair)
		}
		value += lo * place
		value += hi * place * 10
		place *= 100
	}
	return value, nil
}

func isGameOverState(state byte) bool {
	switch state {
	case 0x01, // initialize game over
		0x0D, // game-over curtain
		0x04, // game-over screen
		0x34: // game-over screen before a bonus ending
		return true
	default:
		return false
	}
}
