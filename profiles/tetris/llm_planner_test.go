package tetris

import (
	"context"
	"fmt"
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

func TestChooseLLMPlacementAcceptsShortlistedCandidate(t *testing.T) {
	obs := emptyPlannerObservation(PieceT)
	shortlist, total, err := ShortlistLookaheadSimulations(obs, DefaultLLMShortlistSize)
	if err != nil {
		t.Fatal(err)
	}
	placement := shortlist[0].First.Placement
	model := &scriptedCompleter{responses: []string{placementJSON(placement)}}

	decision, err := ChooseLLMPlacement(context.Background(), model, obs)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Placement != placement {
		t.Fatalf("placement = %+v, want %+v", decision.Placement, placement)
	}
	if decision.Attempts != 1 {
		t.Fatalf("attempts = %d, want 1", decision.Attempts)
	}
	if decision.Candidates != len(shortlist) || decision.TotalCandidates != total {
		t.Fatalf("candidate counts = %d/%d, want %d/%d", decision.Candidates, decision.TotalCandidates, len(shortlist), total)
	}
	if len(model.prompts) != 1 || !strings.Contains(model.prompts[0], "Legal candidates") || !strings.Contains(model.prompts[0], "two-ply") {
		t.Fatalf("prompt missing shortlist/lookahead context: %q", model.prompts)
	}
}

func TestChooseLLMPlacementRetriesMalformedOrUnshortlistedOutput(t *testing.T) {
	obs := emptyPlannerObservation(PieceT)
	shortlist, _, err := ShortlistLookaheadSimulations(obs, 3)
	if err != nil {
		t.Fatal(err)
	}
	valid := shortlist[0].First.Placement
	model := &scriptedCompleter{responses: []string{
		`{"rotation":0,"target_column":0,"reason":"extra"}`,
		`{"rotation":0,"target_column":9}`,
		placementJSON(valid),
	}}

	decision, err := ChooseLLMPlacementWithShortlist(context.Background(), model, obs, 3)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Attempts != 3 {
		t.Fatalf("attempts = %d, want 3", decision.Attempts)
	}
	if decision.Candidates != 3 {
		t.Fatalf("candidates = %d, want 3", decision.Candidates)
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

func TestChooseLLMPlacementWithShortlistBoundsPrompt(t *testing.T) {
	obs := emptyPlannerObservation(PieceO)
	shortlist, total, err := ShortlistLookaheadSimulations(obs, 4)
	if err != nil {
		t.Fatal(err)
	}
	model := &scriptedCompleter{responses: []string{placementJSON(shortlist[0].First.Placement)}}

	decision, err := ChooseLLMPlacementWithShortlist(context.Background(), model, obs, 4)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Candidates != 4 || decision.TotalCandidates != total {
		t.Fatalf("candidate counts = %d/%d, want 4/%d", decision.Candidates, decision.TotalCandidates, total)
	}
	wantSummary := fmt.Sprintf("shortlisted 4 of %d", total)
	if !strings.Contains(model.prompts[0], wantSummary) {
		t.Fatalf("prompt = %q, want %q", model.prompts[0], wantSummary)
	}
}

func placementJSON(placement Placement) string {
	return fmt.Sprintf(`{"rotation":%d,"target_column":%d}`, placement.Rotation, placement.TargetColumn)
}

func emptyPlannerObservation(kind PieceKind) Observation {
	return Observation{
		CurrentPiece: Piece{Kind: kind, Rotation: 0, AnchorX: 63, AnchorY: 24, Visible: true},
		NextPiece:    Piece{Kind: PieceI, Rotation: 0},
		Ready:        true,
	}
}
