package tetris

import "fmt"

// CellOffset is one occupied tetromino cell in the ROM's 4x4 sprite matrix.
// X and Y are zero-based matrix coordinates, not playfield coordinates.
type CellOffset struct {
	X int
	Y int
}

// pieceCells is transcribed from the Rev 1 ROM sprite matrices. Rotation is the
// raw low-two-bit sprite orientation used by the ROM, matching Piece.Rotation.
var pieceCells = map[PieceKind][4][]CellOffset{
	PieceL: {
		{{0, 2}, {1, 2}, {2, 2}, {0, 3}},
		{{1, 1}, {1, 2}, {1, 3}, {2, 3}},
		{{2, 1}, {0, 2}, {1, 2}, {2, 2}},
		{{0, 1}, {1, 1}, {1, 2}, {1, 3}},
	},
	PieceJ: {
		{{0, 2}, {1, 2}, {2, 2}, {2, 3}},
		{{1, 1}, {2, 1}, {1, 2}, {1, 3}},
		{{0, 1}, {0, 2}, {1, 2}, {2, 2}},
		{{1, 1}, {1, 2}, {0, 3}, {1, 3}},
	},
	PieceI: {
		{{0, 2}, {1, 2}, {2, 2}, {3, 2}},
		{{1, 0}, {1, 1}, {1, 2}, {1, 3}},
		{{0, 2}, {1, 2}, {2, 2}, {3, 2}},
		{{1, 0}, {1, 1}, {1, 2}, {1, 3}},
	},
	PieceO: {
		{{1, 2}, {2, 2}, {1, 3}, {2, 3}},
		{{1, 2}, {2, 2}, {1, 3}, {2, 3}},
		{{1, 2}, {2, 2}, {1, 3}, {2, 3}},
		{{1, 2}, {2, 2}, {1, 3}, {2, 3}},
	},
	PieceS: {
		{{0, 2}, {1, 2}, {1, 3}, {2, 3}},
		{{1, 1}, {0, 2}, {1, 2}, {0, 3}},
		{{0, 2}, {1, 2}, {1, 3}, {2, 3}},
		{{1, 1}, {0, 2}, {1, 2}, {0, 3}},
	},
	PieceZ: {
		{{1, 2}, {2, 2}, {0, 3}, {1, 3}},
		{{0, 1}, {0, 2}, {1, 2}, {1, 3}},
		{{1, 2}, {2, 2}, {0, 3}, {1, 3}},
		{{0, 1}, {0, 2}, {1, 2}, {1, 3}},
	},
	PieceT: {
		{{0, 2}, {1, 2}, {2, 2}, {1, 3}},
		{{1, 1}, {1, 2}, {2, 2}, {1, 3}},
		{{1, 1}, {0, 2}, {1, 2}, {2, 2}},
		{{1, 1}, {0, 2}, {1, 2}, {1, 3}},
	},
}

// PieceCells returns a copy of the exact occupied cells for one raw ROM
// orientation. Keeping this public gives later heuristic planners one shared
// geometry source instead of duplicating tetromino assumptions.
func PieceCells(kind PieceKind, rotation int) ([]CellOffset, error) {
	if rotation < 0 || rotation > 3 {
		return nil, fmt.Errorf("tetris: rotation %d is outside raw ROM range 0..3", rotation)
	}
	rotations, ok := pieceCells[kind]
	if !ok {
		return nil, fmt.Errorf("tetris: no geometry for piece %q", kind)
	}
	cells := rotations[rotation]
	out := make([]CellOffset, len(cells))
	copy(out, cells)
	return out, nil
}

func horizontalBounds(kind PieceKind, rotation int) (minX, maxX int, err error) {
	cells, err := PieceCells(kind, rotation)
	if err != nil {
		return 0, 0, err
	}
	minX, maxX = cells[0].X, cells[0].X
	for _, cell := range cells[1:] {
		if cell.X < minX {
			minX = cell.X
		}
		if cell.X > maxX {
			maxX = cell.X
		}
	}
	return minX, maxX, nil
}
