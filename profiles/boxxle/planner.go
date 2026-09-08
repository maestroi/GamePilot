package boxxle

import (
	"errors"
	"fmt"
	"math"
)

// ErrNoMove is returned when the warehouse has no legal step.
var ErrNoMove = errors.New("boxxle: no legal move")

const maxSearchNodes = 500_000

var searchOrder = []Direction{DirUp, DirLeft, DirDown, DirRight}

// Decision is one legal step plus the search/heuristic used to pick it.
type Decision struct {
	Move           Move    `json:"move"`
	Score          float64 `json:"score"`
	Boxes          int     `json:"boxes"`
	GoalsRemaining int     `json:"goals_remaining"`
	PathLength     int     `json:"path_length,omitempty"`
}

// ChooseHeuristicMove returns the first step of a shortest solution when one
// exists. If the warehouse is too large to finish searching, it falls back to
// the one-ply distance greedy used before search landed.
func ChooseHeuristicMove(obs Observation) (Decision, error) {
	if obs.Solved {
		return Decision{}, fmt.Errorf("boxxle: room is already solved")
	}
	if !obs.Ready {
		return Decision{}, fmt.Errorf("boxxle: warehouse is not ready to move")
	}

	if dir, remaining, ok := searchFirstMove(obs); ok {
		next, applied := ApplyMove(obs, dir)
		if !applied {
			return Decision{}, fmt.Errorf("boxxle: search returned illegal %s", dir)
		}
		decision := evaluate(next, dir)
		decision.PathLength = remaining
		return decision, nil
	}
	return greedyMove(obs)
}

func greedyMove(obs Observation) (Decision, error) {
	var best Decision
	found := false
	for _, dir := range searchOrder {
		next, ok := ApplyMove(obs, dir)
		if !ok {
			continue
		}
		decision := evaluate(next, dir)
		if !found || decision.Score > best.Score {
			best = decision
			found = true
		}
	}
	if !found {
		return Decision{}, ErrNoMove
	}
	return best, nil
}

func evaluate(obs Observation, dir Direction) Decision {
	boxes, goals, onGoal := boxStats(obs.Grid)
	score := 10*float64(onGoal) - float64(goals) - 0.01*boxGoalDistance(obs.Grid) - 0.001*playerBoxDistance(obs)
	return Decision{
		Move:           Move{Direction: dir},
		Score:          score,
		Boxes:          boxes,
		GoalsRemaining: goals,
	}
}

type packedState struct {
	player uint8
	boxes  [2]uint64
}

type searchItem struct {
	state packedState
	first Direction
	depth int
}

func searchFirstMove(obs Observation) (Direction, int, bool) {
	walls, goals := staticMap(obs.Grid)
	start := packState(obs)
	if start.solved(goals) {
		return "", 0, false
	}

	seen := map[packedState]struct{}{start: {}}
	queue := []searchItem{{state: start}}
	for len(queue) > 0 && len(seen) < maxSearchNodes {
		cur := queue[0]
		queue = queue[1:]
		for _, dir := range searchOrder {
			next, ok := stepPacked(cur.state, dir, walls)
			if !ok {
				continue
			}
			if _, dup := seen[next]; dup {
				continue
			}
			seen[next] = struct{}{}
			first := cur.first
			if first == "" {
				first = dir
			}
			if next.solved(goals) {
				return first, cur.depth + 1, true
			}
			queue = append(queue, searchItem{state: next, first: first, depth: cur.depth + 1})
		}
	}
	return "", 0, false
}

func staticMap(grid [GridRows][GridColumns]Cell) (walls, goals [GridRows][GridColumns]bool) {
	for row := 0; row < GridRows; row++ {
		for col := 0; col < GridColumns; col++ {
			switch grid[row][col] {
			case Wall:
				walls[row][col] = true
			case Goal, PlayerOnGoal, BoxOnGoal:
				goals[row][col] = true
			}
		}
	}
	return walls, goals
}

func packState(obs Observation) packedState {
	var state packedState
	state.player = cellIndex(obs.PlayerX, obs.PlayerY)
	for row := 0; row < GridRows; row++ {
		for col := 0; col < GridColumns; col++ {
			switch obs.Grid[row][col] {
			case Box, BoxOnGoal:
				state.setBox(cellIndex(col, row))
			}
		}
	}
	return state
}

func (s packedState) solved(goals [GridRows][GridColumns]bool) bool {
	const cells = GridRows * GridColumns
	for i := 0; i < cells; i++ {
		if !s.hasBox(uint8(i)) {
			continue
		}
		x, y := cellXY(uint8(i))
		if !goals[y][x] {
			return false
		}
	}
	return true
}

func stepPacked(state packedState, dir Direction, walls [GridRows][GridColumns]bool) (packedState, bool) {
	dx, dy, ok := dir.Delta()
	if !ok {
		return packedState{}, false
	}
	x, y := cellXY(state.player)
	nx, ny := x+dx, y+dy
	if !inBounds(nx, ny) || walls[ny][nx] {
		return packedState{}, false
	}
	next := state
	nc := cellIndex(nx, ny)
	if state.hasBox(nc) {
		bx, by := nx+dx, ny+dy
		if !inBounds(bx, by) || walls[by][bx] {
			return packedState{}, false
		}
		bc := cellIndex(bx, by)
		if state.hasBox(bc) {
			return packedState{}, false
		}
		next.clearBox(nc)
		next.setBox(bc)
	}
	next.player = nc
	return next, true
}

func cellIndex(x, y int) uint8 {
	return uint8(y*GridColumns + x)
}

func cellXY(index uint8) (x, y int) {
	return int(index) % GridColumns, int(index) / GridColumns
}

func (s packedState) hasBox(index uint8) bool {
	word, bit := index/64, index%64
	return s.boxes[word]&(uint64(1)<<bit) != 0
}

func (s *packedState) setBox(index uint8) {
	word, bit := index/64, index%64
	s.boxes[word] |= uint64(1) << bit
}

func (s *packedState) clearBox(index uint8) {
	word, bit := index/64, index%64
	s.boxes[word] &^= uint64(1) << bit
}

// ApplyMove returns the warehouse after one legal sokoban step.
func ApplyMove(obs Observation, dir Direction) (Observation, bool) {
	dx, dy, ok := dir.Delta()
	if !ok {
		return Observation{}, false
	}
	grid := stripPlayer(obs.Grid)
	x, y := obs.PlayerX, obs.PlayerY
	nx, ny := x+dx, y+dy
	if !inBounds(nx, ny) {
		return Observation{}, false
	}
	switch grid[ny][nx] {
	case Wall:
		return Observation{}, false
	case Box, BoxOnGoal:
		bx, by := nx+dx, ny+dy
		if !inBounds(bx, by) {
			return Observation{}, false
		}
		switch grid[by][bx] {
		case Empty, Goal:
			grid[by][bx] = placeBox(grid[by][bx])
			grid[ny][nx] = leaveBox(grid[ny][nx])
		default:
			return Observation{}, false
		}
	}
	next := obs
	next.Grid = overlayPlayer(grid, nx, ny)
	next.PlayerX = nx
	next.PlayerY = ny
	next.Moves = obs.Moves + 1
	next.Solved = solved(next.Grid)
	return next, true
}

func stripPlayer(grid [GridRows][GridColumns]Cell) [GridRows][GridColumns]Cell {
	for row := 0; row < GridRows; row++ {
		for col := 0; col < GridColumns; col++ {
			switch grid[row][col] {
			case Player:
				grid[row][col] = Empty
			case PlayerOnGoal:
				grid[row][col] = Goal
			}
		}
	}
	return grid
}

func overlayPlayer(grid [GridRows][GridColumns]Cell, x, y int) [GridRows][GridColumns]Cell {
	if !inBounds(x, y) {
		return grid
	}
	if grid[y][x] == Goal {
		grid[y][x] = PlayerOnGoal
	} else {
		grid[y][x] = Player
	}
	return grid
}

func placeBox(cell Cell) Cell {
	if cell == Goal {
		return BoxOnGoal
	}
	return Box
}

func leaveBox(cell Cell) Cell {
	if cell == BoxOnGoal {
		return Goal
	}
	return Empty
}

func boxStats(grid [GridRows][GridColumns]Cell) (boxes, goals, onGoal int) {
	for row := 0; row < GridRows; row++ {
		for col := 0; col < GridColumns; col++ {
			switch grid[row][col] {
			case Box:
				boxes++
			case Goal, PlayerOnGoal:
				goals++
			case BoxOnGoal:
				onGoal++
			}
		}
	}
	return boxes + onGoal, goals, onGoal
}

func boxGoalDistance(grid [GridRows][GridColumns]Cell) float64 {
	var boxes, goals [][2]int
	for row := 0; row < GridRows; row++ {
		for col := 0; col < GridColumns; col++ {
			switch grid[row][col] {
			case Box:
				boxes = append(boxes, [2]int{col, row})
			case Goal, PlayerOnGoal:
				goals = append(goals, [2]int{col, row})
			}
		}
	}
	total := 0.0
	for _, box := range boxes {
		best := math.MaxFloat64
		for _, goal := range goals {
			d := math.Abs(float64(box[0]-goal[0])) + math.Abs(float64(box[1]-goal[1]))
			if d < best {
				best = d
			}
		}
		if best < math.MaxFloat64 {
			total += best
		}
	}
	return total
}

func playerBoxDistance(obs Observation) float64 {
	best := math.MaxFloat64
	for row := 0; row < GridRows; row++ {
		for col := 0; col < GridColumns; col++ {
			if obs.Grid[row][col] != Box {
				continue
			}
			d := math.Abs(float64(obs.PlayerX-col)) + math.Abs(float64(obs.PlayerY-row))
			if d < best {
				best = d
			}
		}
	}
	if best == math.MaxFloat64 {
		return 0
	}
	return best
}
