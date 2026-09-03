package tetris

import (
	"context"
	"strings"
	"testing"
)

type scriptedCompleter struct {
	responses []string
	calls     int
	prompts   []string
}

func (s *scriptedCompleter) CompleteJSON(_ context.Context, _ string, userPrompt string) (string, error) {
	s.prompts = append(s.prompts, userPrompt)
	if s.calls >= len(s.responses) {
		return s.responses[len(s.responses)-1], nil
	}
	response := s.responses[s.calls]
	s.calls++
	return response, nil
}

func TestChooseLLMPlacementAcceptsLegalCandidate(t *testing.T) {
	model := &scriptedCompleter{responses: []string{`{"rotation":2,"target_column":0}`}}
	obs := emptyPlannerObservation(PieceT)

	decision, err := ChooseLLMPlacement(context.Background(), model, obs)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Placement != (Placement{Rotation: 2, TargetColumn: 0}) {
		t.Fatalf("placement = %+v", decision.Placement)
	}
	if decision.Attempts != 1 {
		t.Fatalf("attempts = %d, want 1", decision.Attempts)
	}
	if len(model.prompts) != 1 || !strings.Contains(model.prompts[0], "Legal candidates") {
		t.Fatalf("prompt missing legal candidates: %q", model.prompts)
	}
}

func TestChooseLLMPlacementRetriesMalformedOrIllegalOutput(t *testing.T) {
	model := &scriptedCompleter{responses: []string{
		`{"rotation":0,"target_column":0,"reason":"extra"}`,
		`{"rotation":0,"target_column":9}`,
		`{"rotation":2,"target_column":0}`,
	}}
	obs := emptyPlannerObservation(PieceT)

	decision, err := ChooseLLMPlacement(context.Background(), model, obs)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Attempts != 3 {
		t.Fatalf("attempts = %d, want 3", decision.Attempts)
	}
	if len(model.prompts) != 3 || !strings.Contains(model.prompts[1], "previous response was invalid") {
		t.Fatalf("retry prompt did not explain validation error")
	}
}

func TestChooseLLMPlacementFailsAfterRetryBudget(t *testing.T) {
	model := &scriptedCompleter{responses: []string{`not json`, `not json`, `not json`}}
	obs := emptyPlannerObservation(PieceT)

	_, err := ChooseLLMPlacement(context.Background(), model, obs)
	if err == nil || !strings.Contains(err.Error(), "no valid placement") {
		t.Fatalf("error = %v", err)
	}
}

func emptyPlannerObservation(kind PieceKind) Observation {
	return Observation{
		CurrentPiece: Piece{Kind: kind, Rotation: 0, AnchorX: 63, AnchorY: 24, Visible: true},
		NextPiece:    Piece{Kind: PieceI, Rotation: 0},
		Ready:        true,
	}
}
