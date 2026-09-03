package tetris

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"
)

const BenchmarkFormatVersion = 1

type BenchmarkPlanner string

const (
	BenchmarkHeuristic BenchmarkPlanner = "heuristic"
	BenchmarkLookahead BenchmarkPlanner = "lookahead"
	BenchmarkLLM       BenchmarkPlanner = "llm"
)

// BenchmarkPiece keeps only the piece identity that is fixed by a replay-backed
// scenario. Runtime anchor/visibility fields are reconstructed at spawn.
type BenchmarkPiece struct {
	Kind     PieceKind `json:"kind"`
	Rotation int       `json:"rotation"`
}

// BenchmarkScenario freezes the initial board plus a preview-consistent piece
// sequence from a validated replay. Every benchmarked planner sees this same
// sequence, avoiding live-ROM RNG/timing differences between planner policies.
type BenchmarkScenario struct {
	Hash         string                         `json:"hash"`
	InitialBoard [BoardRows][BoardColumns]Cell `json:"initial_board"`
	InitialLines int                            `json:"initial_lines"`
	InitialLevel int                            `json:"initial_level"`
	InitialScore int                            `json:"initial_score"`
	Pieces       []BenchmarkPiece               `json:"pieces"`
	Moves        int                            `json:"moves"`
}

type BenchmarkConfig struct {
	Planners         []BenchmarkPlanner
	PieceLimit       int
	LLM              JSONCompleter
	LLMShortlistSize int
}

type BenchmarkResult struct {
	Planner            BenchmarkPlanner                `json:"planner"`
	Pieces             int                             `json:"pieces"`
	LinesCleared       int                             `json:"lines_cleared"`
	TopOut             bool                            `json:"top_out"`
	AggregateHeight    int                             `json:"aggregate_height"`
	Holes              int                             `json:"holes"`
	Bumpiness          int                             `json:"bumpiness"`
	PlanCalls          int                             `json:"plan_calls"`
	PlanNanoseconds    int64                           `json:"plan_nanoseconds"`
	MaxPlanNanoseconds int64                           `json:"max_plan_nanoseconds"`
	LLMAttempts        int                             `json:"llm_attempts,omitempty"`
	LLMRetries         int                             `json:"llm_retries,omitempty"`
	CandidatesShown    int                             `json:"candidates_shown,omitempty"`
	TotalCandidates    int                             `json:"total_candidates,omitempty"`
	Placements         []Placement                     `json:"placements"`
	FinalBoard         [BoardRows][BoardColumns]Cell  `json:"final_board"`
}

type BenchmarkReport struct {
	FormatVersion int               `json:"format_version"`
	Profile       string            `json:"profile"`
	ROMSHA256     string            `json:"rom_sha256"`
	SourceMoves   int               `json:"source_moves"`
	Scenario      BenchmarkScenario `json:"scenario"`
	Results       []BenchmarkResult `json:"results"`
}

// BenchmarkScenarioFromReplay converts an already validated replay into a fair
// planner scenario. The replay supplies the piece stream; benchmarked planners
// do not reuse its placements or resulting boards.
func BenchmarkScenarioFromReplay(replay Replay, pieceLimit int) (BenchmarkScenario, error) {
	if err := replay.Validate(); err != nil {
		return BenchmarkScenario{}, err
	}
	if len(replay.Moves) == 0 {
		return BenchmarkScenario{}, fmt.Errorf("tetris: benchmark replay contains no moves")
	}

	moves := len(replay.Moves)
	if pieceLimit > 0 && pieceLimit < moves {
		moves = pieceLimit
	}
	initial := replay.Initial.Observation
	if !initial.Ready {
		return BenchmarkScenario{}, fmt.Errorf("tetris: benchmark replay initial state is not ready")
	}
	if initial.CurrentPiece.Kind == PieceUnknown || initial.NextPiece.Kind == PieceUnknown {
		return BenchmarkScenario{}, fmt.Errorf("tetris: benchmark replay initial piece sequence is unknown")
	}

	pieces := make([]BenchmarkPiece, 0, moves+1)
	pieces = append(pieces, benchmarkPiece(initial.CurrentPiece))
	for i := 0; i < moves; i++ {
		before := replay.Moves[i].Before.Observation
		wantCurrent := pieces[len(pieces)-1]
		gotCurrent := benchmarkPiece(before.CurrentPiece)
		if gotCurrent != wantCurrent {
			return BenchmarkScenario{}, fmt.Errorf(
				"tetris: benchmark replay piece sequence diverges at move %d: expected current %+v, got %+v",
				i+1, wantCurrent, gotCurrent,
			)
		}
		next := benchmarkPiece(before.NextPiece)
		if next.Kind == PieceUnknown {
			return BenchmarkScenario{}, fmt.Errorf("tetris: benchmark replay next piece is unknown at move %d", i+1)
		}
		pieces = append(pieces, next)
	}

	scenario := BenchmarkScenario{
		InitialBoard: initial.Board,
		InitialLines: initial.Lines,
		InitialLevel: initial.Level,
		InitialScore: initial.Score,
		Pieces:       pieces,
		Moves:        moves,
	}
	scenario.Hash = benchmarkScenarioHash(scenario)
	return scenario, nil
}

func benchmarkPiece(piece Piece) BenchmarkPiece {
	return BenchmarkPiece{Kind: piece.Kind, Rotation: piece.Rotation}
}

func benchmarkScenarioHash(scenario BenchmarkScenario) string {
	payload, err := json.Marshal(struct {
		Version      int                           `json:"version"`
		InitialBoard [BoardRows][BoardColumns]Cell `json:"initial_board"`
		InitialLines int                           `json:"initial_lines"`
		InitialLevel int                           `json:"initial_level"`
		InitialScore int                           `json:"initial_score"`
		Pieces       []BenchmarkPiece              `json:"pieces"`
		Moves        int                           `json:"moves"`
	}{
		Version:      BenchmarkFormatVersion,
		InitialBoard: scenario.InitialBoard,
		InitialLines: scenario.InitialLines,
		InitialLevel: scenario.InitialLevel,
		InitialScore: scenario.InitialScore,
		Pieces:       scenario.Pieces,
		Moves:        scenario.Moves,
	})
	if err != nil {
		panic(fmt.Sprintf("tetris: marshal benchmark scenario: %v", err))
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

// BenchmarkReplay runs each requested planner against the same fixed replay
// piece sequence using only deterministic board simulation. LLM timing therefore
// measures model planning, not emulator/controller execution.
func BenchmarkReplay(ctx context.Context, replay Replay, config BenchmarkConfig) (BenchmarkReport, error) {
	if len(config.Planners) == 0 {
		config.Planners = []BenchmarkPlanner{BenchmarkHeuristic, BenchmarkLookahead}
	}
	if config.LLMShortlistSize < 1 {
		config.LLMShortlistSize = DefaultLLMShortlistSize
	}

	scenario, err := BenchmarkScenarioFromReplay(replay, config.PieceLimit)
	if err != nil {
		return BenchmarkReport{}, err
	}

	seen := make(map[BenchmarkPlanner]bool, len(config.Planners))
	results := make([]BenchmarkResult, 0, len(config.Planners))
	for _, planner := range config.Planners {
		if seen[planner] {
			return BenchmarkReport{}, fmt.Errorf("tetris: benchmark planner %q is duplicated", planner)
		}
		seen[planner] = true
		if planner != BenchmarkHeuristic && planner != BenchmarkLookahead && planner != BenchmarkLLM {
			return BenchmarkReport{}, fmt.Errorf("tetris: unsupported benchmark planner %q", planner)
		}
		if planner == BenchmarkLLM && config.LLM == nil {
			return BenchmarkReport{}, fmt.Errorf("tetris: LLM benchmark requires a model client")
		}

		result, err := benchmarkPlanner(ctx, scenario, planner, config)
		if err != nil {
			return BenchmarkReport{}, err
		}
		results = append(results, result)
	}

	return BenchmarkReport{
		FormatVersion: BenchmarkFormatVersion,
		Profile:       replay.Profile,
		ROMSHA256:     replay.ROMSHA256,
		SourceMoves:   len(replay.Moves),
		Scenario:      scenario,
		Results:       results,
	}, nil
}

func benchmarkPlanner(ctx context.Context, scenario BenchmarkScenario, planner BenchmarkPlanner, config BenchmarkConfig) (BenchmarkResult, error) {
	board := scenario.InitialBoard
	result := BenchmarkResult{
		Planner:    planner,
		Placements: make([]Placement, 0, scenario.Moves),
	}
	cleared := 0

	for move := 0; move < scenario.Moves; move++ {
		if err := ctx.Err(); err != nil {
			return BenchmarkResult{}, err
		}
		current := scenario.Pieces[move]
		next := scenario.Pieces[move+1]
		obs := Observation{
			Board: board,
			CurrentPiece: Piece{
				Kind:     current.Kind,
				Rotation: current.Rotation,
				AnchorX:  63,
				AnchorY:  24,
				Visible:  true,
			},
			NextPiece: Piece{
				Kind:     next.Kind,
				Rotation: next.Rotation,
			},
			Score: scenario.InitialScore,
			Level: scenario.InitialLevel,
			Lines: scenario.InitialLines + cleared,
			Ready: true,
		}

		started := time.Now()
		placement, attempts, shown, total, err := benchmarkPlacement(ctx, planner, config, obs)
		elapsed := time.Since(started)
		result.PlanCalls++
		result.PlanNanoseconds += elapsed.Nanoseconds()
		if elapsed.Nanoseconds() > result.MaxPlanNanoseconds {
			result.MaxPlanNanoseconds = elapsed.Nanoseconds()
		}
		if err != nil {
			if errors.Is(err, ErrNoPlacement) {
				result.TopOut = true
				break
			}
			return BenchmarkResult{}, fmt.Errorf("tetris: benchmark %s move %d: %w", planner, move+1, err)
		}

		if planner == BenchmarkLLM {
			result.LLMAttempts += attempts
			if attempts > 1 {
				result.LLMRetries += attempts - 1
			}
			result.CandidatesShown += shown
			result.TotalCandidates += total
		}

		sim, err := SimulatePlacement(board, current.Kind, placement)
		if err != nil {
			if errors.Is(err, ErrPlacementTopOut) {
				result.TopOut = true
				break
			}
			return BenchmarkResult{}, fmt.Errorf("tetris: benchmark %s move %d simulate: %w", planner, move+1, err)
		}
		board = sim.Board
		cleared += sim.LinesCleared
		result.Pieces++
		result.Placements = append(result.Placements, placement)
	}

	result.LinesCleared = cleared
	result.AggregateHeight, result.Holes, result.Bumpiness = boardMetrics(board)
	result.FinalBoard = board
	return result, nil
}

func benchmarkPlacement(ctx context.Context, planner BenchmarkPlanner, config BenchmarkConfig, obs Observation) (Placement, int, int, int, error) {
	switch planner {
	case BenchmarkHeuristic:
		decision, err := ChooseHeuristicPlacement(obs)
		if err != nil {
			return Placement{}, 0, 0, 0, err
		}
		return decision.Placement, 0, 0, 0, nil
	case BenchmarkLookahead:
		decision, err := ChooseLookaheadPlacement(obs)
		if err != nil {
			return Placement{}, 0, 0, 0, err
		}
		return decision.First.Placement, 0, 0, 0, nil
	case BenchmarkLLM:
		decision, err := ChooseLLMPlacementWithShortlist(ctx, config.LLM, obs, config.LLMShortlistSize)
		if err != nil {
			return Placement{}, 0, 0, 0, err
		}
		return decision.Placement, decision.Attempts, decision.Candidates, decision.TotalCandidates, nil
	default:
		return Placement{}, 0, 0, 0, fmt.Errorf("tetris: unsupported benchmark planner %q", planner)
	}
}

func WriteBenchmarkReport(w io.Writer, report BenchmarkReport) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report); err != nil {
		return fmt.Errorf("tetris: write benchmark report: %w", err)
	}
	return nil
}
