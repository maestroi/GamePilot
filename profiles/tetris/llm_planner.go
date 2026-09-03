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
// Candidate counts make prompt-shortlisting behavior visible in logs/benchmarks.
type LLMDecision struct {
	Placement       Placement `json:"placement"`
	Attempts        int       `json:"attempts"`
	Candidates      int       `json:"candidates"`
	TotalCandidates int       `json:"total_candidates"`
}

const llmSystemPrompt = `You are GamePilot's Game Boy Tetris placement planner.
This is a low-latency control task. Return the answer immediately and do not include reasoning in the response.
Choose exactly one placement from the legal candidates supplied by the user.
The candidates have already been ranked with deterministic two-ply simulation using the known preview piece.
Return ONLY a JSON object with exactly these integer fields: "rotation" and "target_column".
Do not use markdown, code fences, commentary, or additional fields.
rotation is the ROM's raw orientation 0..3. target_column is the leftmost occupied board column.
The deterministic controller will execute your choice exactly, so never invent a placement outside the supplied candidate list.`

// ChooseLLMPlacement uses the default bounded two-ply shortlist.
func ChooseLLMPlacement(ctx context.Context, model JSONCompleter, obs Observation) (LLMDecision, error) {
	return ChooseLLMPlacementWithShortlist(ctx, model, obs, DefaultLLMShortlistSize)
}

// ChooseLLMPlacementWithShortlist asks an external model to select one already-
// simulated two-ply candidate. Model output never reaches the emulator directly:
// it is strictly decoded and checked against the deterministic shortlist first.
func ChooseLLMPlacementWithShortlist(ctx context.Context, model JSONCompleter, obs Observation, shortlistSize int) (LLMDecision, error) {
	if model == nil {
		return LLMDecision{}, fmt.Errorf("tetris: LLM planner requires a model client")
	}
	candidates, totalCandidates, err := ShortlistLookaheadSimulations(obs, shortlistSize)
	if err != nil {
		return LLMDecision{}, err
	}
	prompt := formatLLMPlacementPrompt(obs, candidates, totalCandidates)

	var lastErr error
	for attempt := 1; attempt <= maxLLMPlacementAttempts; attempt++ {
		response, err := model.CompleteJSON(ctx, llmSystemPrompt, prompt)
		if err != nil {
			return LLMDecision{}, fmt.Errorf("tetris: LLM request attempt %d: %w", attempt, err)
		}
		placement, err := decodeLLMPlacement(response)
		if err == nil && placementInLookaheadCandidates(placement, candidates) {
			return LLMDecision{
				Placement:       placement,
				Attempts:        attempt,
				Candidates:      len(candidates),
				TotalCandidates: totalCandidates,
			}, nil
		}
		if err == nil {
			err = fmt.Errorf("placement rotation=%d target_column=%d is not in the shortlisted legal candidate list", placement.Rotation, placement.TargetColumn)
		}
		lastErr = err
		prompt = formatLLMPlacementPrompt(obs, candidates, totalCandidates) + fmt.Sprintf(
			"\n\nYour previous response was invalid: %s\nReturn only one shortlisted legal JSON object with rotation and target_column.",
			err,
		)
	}
	return LLMDecision{}, fmt.Errorf("tetris: LLM produced no valid placement after %d attempts: %w", maxLLMPlacementAttempts, lastErr)
}

func formatLLMPlacementPrompt(obs Observation, candidates []LookaheadSimulation, totalCandidates int) string {
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
	fmt.Fprintf(&b, "Legal candidates (shortlisted %d of %d strategically distinct current-piece outcomes), ranked by deterministic two-ply preview-piece evaluation:\n", len(candidates), totalCandidates)
	for rank, candidate := range candidates {
		first := candidate.First
		fmt.Fprintf(
			&b,
			"rank=%d rotation=%d target_column=%d immediate_lines=%d first_height=%d first_holes=%d first_bumpiness=%d ",
			rank+1,
			first.Placement.Rotation,
			first.Placement.TargetColumn,
			first.LinesCleared,
			first.AggregateHeight,
			first.Holes,
			first.Bumpiness,
		)
		if candidate.Reply == nil {
			fmt.Fprintf(&b, "preview_reply=topout total_lines=%d lookahead=%.6f\n", candidate.TotalLines, candidate.Score)
			continue
		}
		reply := candidate.Reply
		fmt.Fprintf(
			&b,
			"preview_rotation=%d preview_target_column=%d preview_lines=%d total_lines=%d leaf_height=%d leaf_holes=%d leaf_bumpiness=%d lookahead=%.6f\n",
			reply.Placement.Rotation,
			reply.Placement.TargetColumn,
			reply.LinesCleared,
			candidate.TotalLines,
			reply.AggregateHeight,
			reply.Holes,
			reply.Bumpiness,
			candidate.Score,
		)
	}
	b.WriteString("Choose one shortlisted candidate. Output JSON only.")
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

func placementInLookaheadCandidates(placement Placement, candidates []LookaheadSimulation) bool {
	for _, candidate := range candidates {
		if candidate.First.Placement == placement {
			return true
		}
	}
	return false
}
