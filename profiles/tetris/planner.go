package tetris

import (
	"errors"
	"fmt"
	"math"
)

var ErrPlacementTopOut = errors.New("tetris: placement tops out")

// HeuristicWeights keeps the first planner deliberately small and inspectable.
// These weights reward line clears while penalizing tall, holey, jagged boards.
type HeuristicWeights struct {
	AggregateHeight float64
	CompleteLines   float64
	Holes           float64
	Bumpiness       float64
}

var DefaultHeuristicWeights = HeuristicWeights{
	AggregateHeight: -0.510066,
	CompleteLines:   0.760666,
	Holes:           -0.35663,
	Bumpiness:       -0.184483,
}

// Simulation is the deterministic result of dropping one placement straight
// down onto a settled board, followed by the ROM's line-clear semantics.
type Simulation struct {
	Placement       Placement                        `json:"placement"`
	Board           [BoardRows][BoardColumns]Cell   `json:"board"`
	LandingRow      int                              `json:"landing_row"`
	LinesCleared    int                              `json:"lines_cleared"`
	AggregateHeight int                              `json:"aggregate_height"`
	Holes           int                              `json:"holes"`
	Bumpiness       int                              `json:"bumpiness"`
	Score           float64                          `json:"score"`
}

// SimulatePlacement applies exact ROM-derived tetromino geometry to the settled
// board. The piece is assumed to have already been rotated and shifted at the
// top of the field, matching ExecutePlacement's input order, then falls without
// further horizontal movement.
func SimulatePlacement(board [BoardRows][BoardColumns]Cell, kind PieceKind, placement Placement) (Simulation, error) {
	cells, err := PieceCells(kind, placement.Rotation)
	if err != nil {
		return Simulation{}, err
	}

	minX, maxX := cells[0].X, cells[0].X
	minY, maxY := cells[0].Y, cells[0].Y
	for _, cell := range cells[1:] {
		if cell.X < minX {
			minX = cell.X
		}
		if cell.X > maxX {
			maxX = cell.X
		}
		if cell.Y < minY {
			minY = cell.Y
		}
		if cell.Y > maxY {
			maxY = cell.Y
		}
	}

	width := maxX - minX + 1
	if placement.TargetColumn < 0 || placement.TargetColumn+width > BoardColumns {
		return Simulation{}, fmt.Errorf(
			"tetris: target column %d is illegal for %s rotation %d (occupied width %d; valid leftmost columns 0..%d)",
			placement.TargetColumn, kind, placement.Rotation, width, BoardColumns-width,
		)
	}

	normalized := make([]CellOffset, len(cells))
	for i, cell := range cells {
		normalized[i] = CellOffset{X: cell.X - minX, Y: cell.Y - minY}
	}
	height := maxY - minY + 1

	// Start with the complete piece above row 0, then move down until the next
	// gravity step would collide. This also models top-out when the stack blocks
	// the piece before all four cells can enter the visible playfield.
	landingRow := -height
	for {
		next := landingRow + 1
		if placementCollides(board, normalized, placement.TargetColumn, next) {
			break
		}
		landingRow = next
	}

	resultBoard := board
	for _, cell := range normalized {
		x := placement.TargetColumn + cell.X
		y := landingRow + cell.Y
		if y < 0 {
			return Simulation{}, ErrPlacementTopOut
		}
		resultBoard[y][x] = Filled
	}

	resultBoard, lines := clearCompleteLines(resultBoard)
	aggregateHeight, holes, bumpiness := boardMetrics(resultBoard)
	score := DefaultHeuristicWeights.AggregateHeight*float64(aggregateHeight) +
		DefaultHeuristicWeights.CompleteLines*float64(lines) +
		DefaultHeuristicWeights.Holes*float64(holes) +
		DefaultHeuristicWeights.Bumpiness*float64(bumpiness)

	return Simulation{
		Placement:       placement,
		Board:           resultBoard,
		LandingRow:      landingRow,
		LinesCleared:    lines,
		AggregateHeight: aggregateHeight,
		Holes:           holes,
		Bumpiness:       bumpiness,
		Score:           score,
	}, nil
}

// ChooseHeuristicPlacement enumerates every raw ROM rotation and legal leftmost
// column, simulates the resulting drop, and selects the highest-scoring board.
// Ties prefer fewer controller inputs, then lower raw rotation and column for a
// stable deterministic result.
func ChooseHeuristicPlacement(obs Observation) (Simulation, error) {
	if obs.GameOver {
		return Simulation{}, fmt.Errorf("tetris: cannot plan after game over")
	}
	if !obs.Ready {
		return Simulation{}, fmt.Errorf("tetris: cannot plan at frame %d: game is not ready", obs.Frame)
	}
	if obs.CurrentPiece.Kind == PieceUnknown {
		return Simulation{}, fmt.Errorf("tetris: cannot plan unknown current piece")
	}

	var best Simulation
	haveBest := false
	for rotation := 0; rotation < 4; rotation++ {
		minX, maxX, err := horizontalBounds(obs.CurrentPiece.Kind, rotation)
		if err != nil {
			return Simulation{}, err
		}
		width := maxX - minX + 1
		for column := 0; column <= BoardColumns-width; column++ {
			candidate, err := SimulatePlacement(obs.Board, obs.CurrentPiece.Kind, Placement{
				Rotation:     rotation,
				TargetColumn: column,
			})
			if errors.Is(err, ErrPlacementTopOut) {
				continue
			}
			if err != nil {
				return Simulation{}, err
			}
			if !haveBest || betterSimulation(obs, candidate, best) {
				best = candidate
				haveBest = true
			}
		}
	}
	if !haveBest {
		return Simulation{}, fmt.Errorf("tetris: no non-top-out placement for %s", obs.CurrentPiece.Kind)
	}
	return best, nil
}

func placementCollides(board [BoardRows][BoardColumns]Cell, cells []CellOffset, targetColumn, baseRow int) bool {
	for _, cell := range cells {
		x := targetColumn + cell.X
		y := baseRow + cell.Y
		if y >= BoardRows {
			return true
		}
		if y >= 0 && board[y][x] == Filled {
			return true
		}
	}
	return false
}

func clearCompleteLines(board [BoardRows][BoardColumns]Cell) ([BoardRows][BoardColumns]Cell, int) {
	var out [BoardRows][BoardColumns]Cell
	dst := BoardRows - 1
	cleared := 0
	for src := BoardRows - 1; src >= 0; src-- {
		full := true
		for column := 0; column < BoardColumns; column++ {
			if board[src][column] != Filled {
				full = false
				break
			}
		}
		if full {
			cleared++
			continue
		}
		out[dst] = board[src]
		dst--
	}
	return out, cleared
}

func boardMetrics(board [BoardRows][BoardColumns]Cell) (aggregateHeight, holes, bumpiness int) {
	var heights [BoardColumns]int
	for column := 0; column < BoardColumns; column++ {
		seenBlock := false
		for row := 0; row < BoardRows; row++ {
			if board[row][column] == Filled {
				if !seenBlock {
					heights[column] = BoardRows - row
					seenBlock = true
				}
				continue
			}
			if seenBlock {
				holes++
			}
		}
		aggregateHeight += heights[column]
	}
	for column := 0; column < BoardColumns-1; column++ {
		bumpiness += int(math.Abs(float64(heights[column] - heights[column+1])))
	}
	return aggregateHeight, holes, bumpiness
}

func betterSimulation(obs Observation, candidate, current Simulation) bool {
	if candidate.Score != current.Score {
		return candidate.Score > current.Score
	}
	candidateCost := placementInputCost(obs, candidate.Placement)
	currentCost := placementInputCost(obs, current.Placement)
	if candidateCost != currentCost {
		return candidateCost < currentCost
	}
	if candidate.Placement.Rotation != current.Placement.Rotation {
		return candidate.Placement.Rotation < current.Placement.Rotation
	}
	return candidate.Placement.TargetColumn < current.Placement.TargetColumn
}

func placementInputCost(obs Observation, placement Placement) int {
	currentRotation := obs.CurrentPiece.Rotation
	clockwise := (currentRotation - placement.Rotation + 4) % 4
	counterClockwise := (placement.Rotation - currentRotation + 4) % 4
	rotationInputs := clockwise
	if counterClockwise < rotationInputs {
		rotationInputs = counterClockwise
	}

	minX, _, err := horizontalBounds(obs.CurrentPiece.Kind, placement.Rotation)
	if err != nil {
		return math.MaxInt
	}
	matrixBaseColumn := placement.TargetColumn - minX
	desiredAnchorX := anchorXMatrixColumnZeroAtBoardZero + matrixBaseColumn*cellPixels
	delta := desiredAnchorX - int(obs.CurrentPiece.AnchorX)
	if delta < 0 {
		delta = -delta
	}
	return rotationInputs + delta/cellPixels
}
