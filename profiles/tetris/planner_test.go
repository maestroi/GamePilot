package tetris

import (
	"errors"
	"testing"
)

func TestSimulatePlacementDropsIToBottom(t *testing.T) {
	sim, err := SimulatePlacement([BoardRows][BoardColumns]Cell{}, PieceI, Placement{
		Rotation:     0,
		TargetColumn: 0,
	})
	if err != nil {
		t.Fatal(err)
	}
	if sim.LandingRow != BoardRows-1 {
		t.Fatalf("landing row = %d, want %d", sim.LandingRow, BoardRows-1)
	}
	for column := 0; column < 4; column++ {
		if sim.Board[BoardRows-1][column] != Filled {
			t.Fatalf("bottom row column %d is not filled", column)
		}
	}
}

func TestSimulatePlacementClearsCompletedLine(t *testing.T) {
	var board [BoardRows][BoardColumns]Cell
	for column := 4; column < BoardColumns; column++ {
		board[BoardRows-1][column] = Filled
	}

	sim, err := SimulatePlacement(board, PieceI, Placement{
		Rotation:     0,
		TargetColumn: 0,
	})
	if err != nil {
		t.Fatal(err)
	}
	if sim.LinesCleared != 1 {
		t.Fatalf("lines cleared = %d, want 1", sim.LinesCleared)
	}
	for column := 0; column < BoardColumns; column++ {
		if sim.Board[BoardRows-1][column] != Empty {
			t.Fatalf("bottom row column %d = %d after clear, want empty", column, sim.Board[BoardRows-1][column])
		}
	}
}

func TestSimulatePlacementDetectsTopOut(t *testing.T) {
	var board [BoardRows][BoardColumns]Cell
	for column := 0; column < BoardColumns; column++ {
		board[0][column] = Filled
	}

	_, err := SimulatePlacement(board, PieceO, Placement{Rotation: 0, TargetColumn: 0})
	if !errors.Is(err, ErrPlacementTopOut) {
		t.Fatalf("error = %v, want ErrPlacementTopOut", err)
	}
}

func TestChooseHeuristicPlacementPrefersLineClear(t *testing.T) {
	obs := readyPlannerObservation(PieceI)
	for column := 4; column < BoardColumns; column++ {
		obs.Board[BoardRows-1][column] = Filled
	}

	decision, err := ChooseHeuristicPlacement(obs)
	if err != nil {
		t.Fatal(err)
	}
	if decision.LinesCleared != 1 {
		t.Fatalf("lines cleared = %d, want 1", decision.LinesCleared)
	}
	if decision.Placement.Rotation != 0 {
		t.Fatalf("rotation = %d, want 0", decision.Placement.Rotation)
	}
	if decision.Placement.TargetColumn != 0 {
		t.Fatalf("target column = %d, want 0", decision.Placement.TargetColumn)
	}
}

func TestChooseHeuristicPlacementRejectsFullBoard(t *testing.T) {
	obs := readyPlannerObservation(PieceT)
	for row := 0; row < BoardRows; row++ {
		for column := 0; column < BoardColumns; column++ {
			obs.Board[row][column] = Filled
		}
	}

	_, err := ChooseHeuristicPlacement(obs)
	if err == nil {
		t.Fatal("expected no-placement error")
	}
}

func readyPlannerObservation(kind PieceKind) Observation {
	return Observation{
		CurrentPiece: Piece{
			Kind:     kind,
			Rotation: 0,
			AnchorX:  63,
			AnchorY:  24,
			Visible:  true,
		},
		Ready: true,
	}
}
