package boxxle

import "testing"

func TestChooseHeuristicMoveWalksAroundBlockedBox(t *testing.T) {
	obs := warehouseObservation(`
#####     
#@  #     
# $$# ### 
# $ # #.# 
### ###.# 
 ##    .# 
 #   #  # 
 #   #### 
 #####    
`)
	decision, err := ChooseHeuristicMove(obs)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Move.Direction != DirRight {
		t.Fatalf("direction = %s, want right (down walks into a dead end and oscillates)", decision.Move.Direction)
	}
}

func TestChooseHeuristicMoveSolvesTinyRoom(t *testing.T) {
	obs := warehouseObservation(`
#####
#@#.#
# $ #
#   #
#####
`)
	seen := map[string]bool{}
	for i := 0; i < 16 && !obs.Solved; i++ {
		key := RenderGrid(obs)
		if seen[key] {
			t.Fatalf("planner looped after %d moves:\n%s", i, key)
		}
		seen[key] = true
		decision, err := ChooseHeuristicMove(obs)
		if err != nil {
			t.Fatalf("move %d: %v\n%s", i+1, err, key)
		}
		next, ok := ApplyMove(obs, decision.Move.Direction)
		if !ok {
			t.Fatalf("move %d %s was illegal\n%s", i+1, decision.Move.Direction, key)
		}
		obs = next
	}
	if !obs.Solved {
		t.Fatalf("expected solved warehouse, got:\n%s", RenderGrid(obs))
	}
}

func TestChooseHeuristicMoveSolvesRoom11(t *testing.T) {
	obs := warehouseObservation(`
#####     
#@  #     
# $$# ### 
# $ # #.# 
### ###.# 
 ##    .# 
 #   #  # 
 #   #### 
 #####    
`)
	for i := 0; i < 250 && !obs.Solved; i++ {
		decision, err := ChooseHeuristicMove(obs)
		if err != nil {
			t.Fatalf("move %d: %v\n%s", i+1, err, RenderGrid(obs))
		}
		next, ok := ApplyMove(obs, decision.Move.Direction)
		if !ok {
			t.Fatalf("move %d %s was illegal\n%s", i+1, decision.Move.Direction, RenderGrid(obs))
		}
		obs = next
	}
	if !obs.Solved {
		t.Fatalf("expected room 1-1 solved, got:\n%s", RenderGrid(obs))
	}
}

func warehouseObservation(dump string) Observation {
	var obs Observation
	obs.Ready = true
	obs.Playing = true
	row, col := 0, 0
	for _, r := range dump {
		if r == '\n' {
			if col > 0 {
				row++
				col = 0
			}
			continue
		}
		if row >= GridRows || col >= GridColumns {
			continue
		}
		switch r {
		case '#':
			obs.Grid[row][col] = Wall
		case '$':
			obs.Grid[row][col] = Box
		case '.':
			obs.Grid[row][col] = Goal
		case '*':
			obs.Grid[row][col] = BoxOnGoal
		case '@':
			obs.Grid[row][col] = Player
			obs.PlayerX, obs.PlayerY = col, row
		case '+':
			obs.Grid[row][col] = PlayerOnGoal
			obs.PlayerX, obs.PlayerY = col, row
		default:
			obs.Grid[row][col] = Empty
		}
		col++
	}
	obs.Solved = solved(obs.Grid)
	return obs
}
