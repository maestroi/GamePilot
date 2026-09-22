package tetris

import (
	"context"
	"fmt"

	"github.com/maestroi/GamePilot/emulator/ramclassify"
)

const DefaultJevCandidateLimit = 12

// JevPlacementConfig bounds the amount of real-emulator shadow search exposed
// to a Jev-compatible decision engine on each piece.
type JevPlacementConfig struct {
	Model         string `json:"model"`
	MaxCandidates int    `json:"max_candidates"`
}

func DefaultJevPlacementConfig() JevPlacementConfig {
	return JevPlacementConfig{
		Model:         "openjev-latest",
		MaxCandidates: DefaultJevCandidateLimit,
	}
}

// ShadowPlacementOutcome is one placement executed from the exact live
// checkpoint, then observed after the lock pipeline reaches the next piece.
type ShadowPlacementOutcome struct {
	ID              string      `json:"id"`
	Placement       Placement   `json:"placement"`
	After           Observation `json:"after"`
	ScoreDelta      int         `json:"score_delta"`
	LinesDelta      int         `json:"lines_delta"`
	AggregateHeight int         `json:"aggregate_height"`
	Holes           int         `json:"holes"`
	Bumpiness       int         `json:"bumpiness"`
}

// JevPlacementDecision preserves the bounded distribution returned by Jev so
// gameplay decisions remain inspectable and replay/debug tooling can see why a
// particular placement was selected.
type JevPlacementDecision struct {
	Placement       Placement                     `json:"placement"`
	Choice          string                        `json:"choice"`
	Probability     float64                       `json:"probability"`
	Confidence      float64                       `json:"confidence"`
	Probabilities   map[string]float64             `json:"probabilities"`
	Candidates      int                           `json:"candidates"`
	TotalCandidates int                           `json:"total_candidates"`
	Outcomes        []ShadowPlacementOutcome      `json:"outcomes"`
}

type shadowPlacementMachine interface {
	controllerEmulator
	SaveCheckpoint() ([]byte, error)
	LoadCheckpoint([]byte) error
}

// ChooseJevPlacementWithShadow executes each shortlisted placement in the real
// emulator from the same checkpoint, restores the live state, and asks a
// Jev-compatible engine to choose only among those verified outcomes.
//
// The model never controls buttons directly and cannot invent a placement:
// every option has already completed through ExecutePlacement in a shadow run.
func ChooseJevPlacementWithShadow(
	ctx context.Context,
	machine shadowPlacementMachine,
	engine ramclassify.DecisionEngine,
	obs Observation,
	cfg JevPlacementConfig,
) (decision JevPlacementDecision, err error) {
	if machine == nil {
		return JevPlacementDecision{}, fmt.Errorf("tetris: Jev shadow planner requires an emulator")
	}
	if engine == nil {
		return JevPlacementDecision{}, fmt.Errorf("tetris: Jev shadow planner requires a decision engine")
	}
	if cfg.Model == "" {
		cfg.Model = DefaultJevPlacementConfig().Model
	}
	if cfg.MaxCandidates == 0 {
		cfg.MaxCandidates = DefaultJevCandidateLimit
	}
	if cfg.MaxCandidates < 1 {
		return JevPlacementDecision{}, fmt.Errorf("tetris: Jev candidate limit must be at least 1")
	}
	if obs.GameOver {
		return JevPlacementDecision{}, fmt.Errorf("tetris: cannot ask Jev for a placement after game over")
	}
	if !obs.Ready {
		return JevPlacementDecision{}, fmt.Errorf("tetris: cannot ask Jev at frame %d: game is not ready", obs.Frame)
	}

	shortlist, totalCandidates, err := ShortlistLookaheadSimulations(obs, cfg.MaxCandidates)
	if err != nil {
		return JevPlacementDecision{}, err
	}

	root, err := machine.SaveCheckpoint()
	if err != nil {
		return JevPlacementDecision{}, fmt.Errorf("tetris: save Jev shadow checkpoint: %w", err)
	}
	defer func() {
		if restoreErr := machine.LoadCheckpoint(root); restoreErr != nil && err == nil {
			err = fmt.Errorf("tetris: restore Jev shadow checkpoint: %w", restoreErr)
		}
	}()

	outcomes := make([]ShadowPlacementOutcome, 0, len(shortlist))
	criteria := make(map[string]string, len(shortlist))
	byID := make(map[string]ShadowPlacementOutcome, len(shortlist))

	for i, candidate := range shortlist {
		if err := ctx.Err(); err != nil {
			return JevPlacementDecision{}, err
		}
		if err := machine.LoadCheckpoint(root); err != nil {
			return JevPlacementDecision{}, fmt.Errorf("tetris: restore checkpoint before shadow candidate %d: %w", i, err)
		}

		after, err := ExecutePlacement(ctx, machine, candidate.First.Placement)
		if err != nil {
			return JevPlacementDecision{}, fmt.Errorf(
				"tetris: shadow candidate %d rotation=%d target_column=%d: %w",
				i,
				candidate.First.Placement.Rotation,
				candidate.First.Placement.TargetColumn,
				err,
			)
		}
		aggregateHeight, holes, bumpiness := boardMetrics(after.Board)
		id := fmt.Sprintf("placement_%02d", i)
		outcome := ShadowPlacementOutcome{
			ID:              id,
			Placement:       candidate.First.Placement,
			After:           after,
			ScoreDelta:      after.Score - obs.Score,
			LinesDelta:      after.Lines - obs.Lines,
			AggregateHeight: aggregateHeight,
			Holes:           holes,
			Bumpiness:       bumpiness,
		}
		outcomes = append(outcomes, outcome)
		byID[id] = outcome
		criteria[id] = fmt.Sprintf(
			"rotation=%d target_column=%d; verified emulator outcome: lines_delta=%d score_delta=%d aggregate_height=%d holes=%d bumpiness=%d game_over=%t",
			outcome.Placement.Rotation,
			outcome.Placement.TargetColumn,
			outcome.LinesDelta,
			outcome.ScoreDelta,
			outcome.AggregateHeight,
			outcome.Holes,
			outcome.Bumpiness,
			outcome.After.GameOver,
		)
	}

	// Restore before the model call so the live emulator never remains on the
	// final speculative branch while an external/local decision engine runs.
	if err := machine.LoadCheckpoint(root); err != nil {
		return JevPlacementDecision{}, fmt.Errorf("tetris: restore checkpoint before Jev decision: %w", err)
	}

	state := map[string]any{
		"task": "Choose one verified Tetris placement. Prefer long-term survival and line clearing. Avoid holes, excessive stack height, rough surfaces, and any game-over outcome. Every option below was executed in the emulator from the exact same checkpoint.",
		"current": obs,
		"outcomes": outcomes,
	}
	response, err := engine.Decide(ctx, cfg.Model, state, map[string]ramclassify.Question{
		"placement": {
			Type:         "choice",
			Instructions: "Choose exactly one verified placement outcome to execute on the live emulator.",
			Criteria:     criteria,
		},
	})
	if err != nil {
		return JevPlacementDecision{}, fmt.Errorf("tetris: Jev placement decision: %w", err)
	}
	answer, ok := response.Answers["placement"]
	if !ok {
		return JevPlacementDecision{}, fmt.Errorf("tetris: Jev response missing placement answer")
	}
	if answer.Type != "choice" {
		return JevPlacementDecision{}, fmt.Errorf("tetris: Jev placement answer has type %q, want choice", answer.Type)
	}
	selected, ok := byID[answer.Choice]
	if !ok {
		return JevPlacementDecision{}, fmt.Errorf("tetris: Jev chose unknown placement option %q", answer.Choice)
	}
	probability, ok := answer.Probabilities[answer.Choice]
	if !ok {
		return JevPlacementDecision{}, fmt.Errorf("tetris: Jev chose %q without a probability", answer.Choice)
	}
	if probability < 0 || probability > 1 || answer.Confidence < 0 || answer.Confidence > 1 {
		return JevPlacementDecision{}, fmt.Errorf("tetris: Jev placement probability/confidence outside 0..1")
	}

	return JevPlacementDecision{
		Placement:       selected.Placement,
		Choice:          answer.Choice,
		Probability:     probability,
		Confidence:      answer.Confidence,
		Probabilities:   answer.Probabilities,
		Candidates:      len(outcomes),
		TotalCandidates: totalCandidates,
		Outcomes:        outcomes,
	}, nil
}
