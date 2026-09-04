package tetris

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestBenchmarkScenarioFromReplayUsesPreviewSequence(t *testing.T) {
	pieces := []BenchmarkPiece{
		{Kind: PieceT, Rotation: 0},
		{Kind: PieceJ, Rotation: 0},
		{Kind: PieceO, Rotation: 0},
		{Kind: PieceI, Rotation: 0},
	}
	replay := benchmarkTestReplay(t, [BoardRows][BoardColumns]Cell{}, pieces)

	scenario, err := BenchmarkScenarioFromReplay(replay, 3)
	if err != nil {
		t.Fatal(err)
	}
	if scenario.Moves != 3 {
		t.Fatalf("moves = %d, want 3", scenario.Moves)
	}
	if len(scenario.Pieces) != len(pieces) {
		t.Fatalf("piece sequence length = %d, want %d", len(scenario.Pieces), len(pieces))
	}
	for i := range pieces {
		if scenario.Pieces[i] != pieces[i] {
			t.Fatalf("piece[%d] = %+v, want %+v", i, scenario.Pieces[i], pieces[i])
		}
	}
	if scenario.Hash == "" {
		t.Fatal("scenario hash is empty")
	}
}

func TestBenchmarkReplayComparesOneAndTwoPlyOnSameSequence(t *testing.T) {
	var board [BoardRows][BoardColumns]Cell
	board[BoardRows-1][0] = Filled
	board[BoardRows-1][1] = Filled
	replay := benchmarkTestReplay(t, board, []BenchmarkPiece{
		{Kind: PieceI, Rotation: 0},
		{Kind: PieceJ, Rotation: 0},
	})

	report, err := BenchmarkReplay(context.Background(), replay, BenchmarkConfig{
		Planners:   []BenchmarkPlanner{BenchmarkHeuristic, BenchmarkLookahead},
		PieceLimit: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Results) != 2 {
		t.Fatalf("results = %d, want 2", len(report.Results))
	}
	if got := report.Results[0].Placements[0]; got != (Placement{Rotation: 0, TargetColumn: 2}) {
		t.Fatalf("heuristic placement = %+v, want r0/c2", got)
	}
	if got := report.Results[1].Placements[0]; got != (Placement{Rotation: 0, TargetColumn: 3}) {
		t.Fatalf("lookahead placement = %+v, want r0/c3", got)
	}
	if report.Results[0].Pieces != 1 || report.Results[1].Pieces != 1 {
		t.Fatalf("pieces = %d/%d, want 1/1", report.Results[0].Pieces, report.Results[1].Pieces)
	}
}

func TestBenchmarkReplayTracksLLMAttemptsAndCandidates(t *testing.T) {
	var board [BoardRows][BoardColumns]Cell
	board[BoardRows-1][0] = Filled
	board[BoardRows-1][1] = Filled
	replay := benchmarkTestReplay(t, board, []BenchmarkPiece{
		{Kind: PieceI, Rotation: 0},
		{Kind: PieceJ, Rotation: 0},
	})
	model := &scriptedCompleter{responses: []string{`{"rotation":0,"target_column":3}`}}

	report, err := BenchmarkReplay(context.Background(), replay, BenchmarkConfig{
		Planners:         []BenchmarkPlanner{BenchmarkLLM},
		PieceLimit:       1,
		LLM:              model,
		LLMShortlistSize: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	result := report.Results[0]
	if result.LLMAttempts != 1 || result.LLMRetries != 0 {
		t.Fatalf("attempts/retries = %d/%d, want 1/0", result.LLMAttempts, result.LLMRetries)
	}
	if result.CandidatesShown != 1 || result.TotalCandidates <= result.CandidatesShown {
		t.Fatalf("candidate counts = %d/%d, want 1/<larger total>", result.CandidatesShown, result.TotalCandidates)
	}
	if got := result.Placements[0]; got != (Placement{Rotation: 0, TargetColumn: 3}) {
		t.Fatalf("LLM placement = %+v, want r0/c3", got)
	}
}

func TestBenchmarkReplayTreatsNoPlacementAsTopOut(t *testing.T) {
	var board [BoardRows][BoardColumns]Cell
	for row := 0; row < BoardRows; row++ {
		for column := 0; column < BoardColumns; column++ {
			board[row][column] = Filled
		}
	}
	replay := benchmarkTestReplay(t, board, []BenchmarkPiece{
		{Kind: PieceT, Rotation: 0},
		{Kind: PieceI, Rotation: 0},
	})

	report, err := BenchmarkReplay(context.Background(), replay, BenchmarkConfig{
		Planners:   []BenchmarkPlanner{BenchmarkHeuristic},
		PieceLimit: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	result := report.Results[0]
	if !result.TopOut || result.Pieces != 0 {
		t.Fatalf("topout/pieces = %t/%d, want true/0", result.TopOut, result.Pieces)
	}
}

func TestWriteBenchmarkReport(t *testing.T) {
	replay := benchmarkTestReplay(t, [BoardRows][BoardColumns]Cell{}, []BenchmarkPiece{
		{Kind: PieceO, Rotation: 0},
		{Kind: PieceI, Rotation: 0},
	})
	report, err := BenchmarkReplay(context.Background(), replay, BenchmarkConfig{
		Planners:   []BenchmarkPlanner{BenchmarkHeuristic},
		PieceLimit: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := WriteBenchmarkReport(&out, report); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"format_version": 1`) || !strings.Contains(out.String(), `"planner": "heuristic"`) {
		t.Fatalf("unexpected report JSON: %s", out.String())
	}
}

func benchmarkTestReplay(t *testing.T, board [BoardRows][BoardColumns]Cell, pieces []BenchmarkPiece) Replay {
	t.Helper()
	if len(pieces) < 2 {
		t.Fatal("benchmark test replay requires at least two pieces")
	}
	obs := Observation{
		Frame: 100,
		Board: board,
		CurrentPiece: Piece{
			Kind:     pieces[0].Kind,
			Rotation: pieces[0].Rotation,
			AnchorX:  63,
			AnchorY:  24,
			Visible:  true,
		},
		NextPiece: Piece{Kind: pieces[1].Kind, Rotation: pieces[1].Rotation},
		Ready:     true,
	}
	replay := NewReplay(Rev1SHA256, obs)
	before := obs
	for i := 0; i < len(pieces)-1; i++ {
		after := before
		after.Frame++
		after.CurrentPiece = Piece{
			Kind:     pieces[i+1].Kind,
			Rotation: pieces[i+1].Rotation,
			AnchorX:  63,
			AnchorY:  24,
			Visible:  true,
		}
		if i+2 < len(pieces) {
			after.NextPiece = Piece{Kind: pieces[i+2].Kind, Rotation: pieces[i+2].Rotation}
		} else {
			after.NextPiece = Piece{Kind: PieceT, Rotation: 0}
		}
		if err := replay.Append(before, Placement{Rotation: 0, TargetColumn: 0}, after); err != nil {
			t.Fatal(err)
		}
		before = after
	}
	return replay
}
