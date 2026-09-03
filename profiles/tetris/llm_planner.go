package tetris

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

const maxLLMPlacementAttempts = 3

// JSONCompleter is the small model boundary required by the Tetris planner.
// Transport/provider details live outside the profile; the model only receives
// text and returns a candidate JSON object.
type JSONCompleter interface {
	CompleteJSON(ctx context.Context, systemPrompt, userPrompt string) (string, error)
}

// LLMDecision records the validated game-level action selected by the model.
// Attempts is useful when diagnosing local models that needed format retries.
type LLMDecision struct {
	Placement Placement `json:"placement"`
	Attempts  int       `json:"attempts"`
}

const llmSystemPrompt = `You are GamePilot's Game Boy Tetris placement planner.
This is a low-latency control task. Do not reason out loud or produce a chain of thought. /no_think
Choose exactly one placement from the legal candidates supplied by the user.
Return ONLY a JSON object with exactly these integer fields: "rotation" and "target_column".
Do not use markdown, code fences, commentary, or additional fields.
rotation is the ROM's raw orientation 0..3. target_column is the leftmost occupied board column.
The deterministic controller will execute your choice exactly, so never invent a placement outside the supplied candidate list.`

// ChooseLLMPlacement asks an external model to select one already-simulated
// legal placement. Model output never reaches the emulator directly: it is
// strictly decoded and checked against the deterministic candidate set first.
func ChooseLLMPlacement(ctx context.Context, model JSONCompleter, obs Observation) (LLMDecision, error) {
	if model == nil {
		return LLMDecision{}, fmt.Errorf("tetris: LLM planner requires a model client")
	}
	candidates, err := LegalSimulations(obs)
	if err != nil {
		return LLMDecision{}, err
	}
	prompt := formatLLMPlacementPrompt(obs, candidates)

	var lastErr error
	for attempt := 1; attempt <= maxLLMPlacementAttempts; attempt++ {
		response, err := model.CompleteJSON(ctx, llmSystemPrompt, prompt)
		if err != nil {
			return LLMDecision{}, fmt.Errorf("tetris: LLM request attempt %d: %w", attempt, err)
		}
		placement, err := decodeLLMPlacement(response)
		if err == nil && placementInCandidates(placement, candidates) {
			return LLMDecision{Placement: placement, Attempts: attempt}, nil
		}
		if err == nil {
			err = fmt.Errorf("placement rotation=%d target_column=%d is not in the legal candidate list", placement.Rotation, placement.TargetColumn)
		}
		lastErr = err
		prompt = formatLLMPlacementPrompt(obs, candidates) + fmt.Sprintf(
			"\n\nYour previous response was invalid: %s\nReturn only one legal JSON object with rotation and target_column.",
			err,
		)
	}
	return LLMDecision{}, fmt.Errorf("tetris: LLM produced no valid placement after %d attempts: %w", maxLLMPlacementAttempts, lastErr)
}

func formatLLMPlacementPrompt(obs Observation, candidates []Simulation) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Current piece: %s raw_rotation=%d\n", obs.CurrentPiece.Kind, obs.CurrentPiece.Rotation)
	fmt.Fprintf(&b, "Next piece: %s raw_rotation=%d\n", obs.NextPiece.Kind, obs.NextPiece.Rotation)
	fmt.Fprintf(&b, "Score: %d  Lines: %d  Level: %d\n", obs.Score, obs.Lines, obs.Level)
	b.WriteString("Board: 18 rows x 10 columns, top to bottom. #=filled .=empty\n")
	for row := 0; row < BoardRows; row++ {
		for column := 0; column < BoardColumns; column++ {
			if obs.Board[row][column] == Filled {
				b.WriteByte('#')
			} else {
				b.WriteByte('.')
			}
		}
		b.WriteByte('\n')
	}
	b.WriteString("Legal candidates and deterministic one-piece simulation metrics:\n")
	for _, candidate := range candidates {
		fmt.Fprintf(
			&b,
			"rotation=%d target_column=%d lines=%d aggregate_height=%d holes=%d bumpiness=%d\n",
			candidate.Placement.Rotation,
			candidate.Placement.TargetColumn,
			candidate.LinesCleared,
			candidate.AggregateHeight,
			candidate.Holes,
			candidate.Bumpiness,
		)
	}
	b.WriteString("Choose one candidate. Output JSON only.")
	return b.String()
}

func decodeLLMPlacement(raw string) (Placement, error) {
	decoder := json.NewDecoder(strings.NewReader(strings.TrimSpace(raw)))
	decoder.DisallowUnknownFields()
	var placement Placement
	if err := decoder.Decode(&placement); err != nil {
		return Placement{}, fmt.Errorf("invalid placement JSON: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return Placement{}, fmt.Errorf("invalid placement JSON: trailing JSON value")
		}
		return Placement{}, fmt.Errorf("invalid placement JSON: trailing content: %w", err)
	}
	if placement.Rotation < 0 || placement.Rotation > 3 {
		return Placement{}, fmt.Errorf("rotation %d is outside 0..3", placement.Rotation)
	}
	return placement, nil
}

func placementInCandidates(placement Placement, candidates []Simulation) bool {
	for _, candidate := range candidates {
		if candidate.Placement == placement {
			return true
		}
	}
	return false
}
