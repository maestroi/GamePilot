package tetris

import (
	"errors"
	"fmt"
	"sort"
)

const (
	// DefaultLLMShortlistSize bounds prompt size while still giving the model a
	// useful set of strategically distinct choices.
	DefaultLLMShortlistSize = 10

	// A first move that leaves no legal placement for the known preview piece is
	// effectively a forced next-piece top-out and must rank below every survivable
	// first move. Keep the value finite so scores remain JSON-safe and printable.
	lookaheadTopOutPenalty = 1_000_000.0
)

// LookaheadSimulation evaluates one current-piece placement plus the best
// deterministic reply available to the known preview piece. Score evaluates the
// leaf board with the normal heuristic weights and credits line clears from both
// placements, avoiding double-counting the intermediate board metrics.
type LookaheadSimulation struct {
	First      Simulation  `json:"first"`
	Reply      *Simulation `json:"reply,omitempty"`
	TotalLines int         `json:"total_lines"`
	Score      float64     `json:"score"`
}

// UniqueLegalSimulations collapses raw rotations/placements that produce the
// exact same settled board. When multiple inputs are equivalent, the normal
// deterministic heuristic tie-break chooses the cheaper/stabler representation.
func UniqueLegalSimulations(obs Observation) ([]Simulation, error) {
	candidates, err := LegalSimulations(obs)
	if err != nil {
		return nil, err
	}

	unique := make([]Simulation, 0, len(candidates))
	byBoard := make(map[[BoardRows][BoardColumns]Cell]int, len(candidates))
	for _, candidate := range candidates {
		if index, ok := byBoard[candidate.Board]; ok {
			if betterSimulation(obs, candidate, unique[index]) {
				unique[index] = candidate
			}
			continue
		}
		byBoard[candidate.Board] = len(unique)
		unique = append(unique, candidate)
	}
	return unique, nil
}

// LookaheadSimulations returns strategically distinct current-piece placements
// ordered best-first by a deterministic two-ply evaluation using the known next
// piece. The next piece is not guessed: it comes directly from the ROM preview.
func LookaheadSimulations(obs Observation) ([]LookaheadSimulation, error) {
	if obs.NextPiece.Kind == PieceUnknown {
		return nil, fmt.Errorf("tetris: cannot look ahead with unknown next piece")
	}

	firstMoves, err := UniqueLegalSimulations(obs)
	if err != nil {
		return nil, err
	}

	results := make([]LookaheadSimulation, 0, len(firstMoves))
	for _, first := range firstMoves {
		replyObs := Observation{
			Board: first.Board,
			CurrentPiece: Piece{
				Kind:     obs.NextPiece.Kind,
				Rotation: obs.NextPiece.Rotation,
				AnchorX:  63,
				AnchorY:  24,
				Visible:  true,
			},
			Ready: true,
		}

		replies, replyErr := LegalSimulations(replyObs)
		if replyErr != nil {
			if !errors.Is(replyErr, ErrNoPlacement) {
				return nil, replyErr
			}
			results = append(results, LookaheadSimulation{
				First:      first,
				TotalLines: first.LinesCleared,
				Score:      first.Score - lookaheadTopOutPenalty,
			})
			continue
		}

		bestReply := replies[0]
		for _, reply := range replies[1:] {
			if betterSimulation(replyObs, reply, bestReply) {
				bestReply = reply
			}
		}
		totalLines := first.LinesCleared + bestReply.LinesCleared
		leafScore := DefaultHeuristicWeights.AggregateHeight*float64(bestReply.AggregateHeight) +
			DefaultHeuristicWeights.CompleteLines*float64(totalLines) +
			DefaultHeuristicWeights.Holes*float64(bestReply.Holes) +
			DefaultHeuristicWeights.Bumpiness*float64(bestReply.Bumpiness)
		replyCopy := bestReply
		results = append(results, LookaheadSimulation{
			First:      first,
			Reply:      &replyCopy,
			TotalLines: totalLines,
			Score:      leafScore,
		})
	}

	sort.SliceStable(results, func(i, j int) bool {
		return betterLookahead(obs, results[i], results[j])
	})
	return results, nil
}

// ChooseLookaheadPlacement selects the best deterministic two-ply candidate.
func ChooseLookaheadPlacement(obs Observation) (LookaheadSimulation, error) {
	candidates, err := LookaheadSimulations(obs)
	if err != nil {
		return LookaheadSimulation{}, err
	}
	return candidates[0], nil
}

// ShortlistLookaheadSimulations returns at most limit best-first candidates plus
// the total number of strategically distinct first-move outcomes considered.
func ShortlistLookaheadSimulations(obs Observation, limit int) ([]LookaheadSimulation, int, error) {
	if limit < 1 {
		return nil, 0, fmt.Errorf("tetris: lookahead shortlist limit must be at least 1")
	}
	candidates, err := LookaheadSimulations(obs)
	if err != nil {
		return nil, 0, err
	}
	total := len(candidates)
	if len(candidates) > limit {
		candidates = candidates[:limit]
	}
	return candidates, total, nil
}

func betterLookahead(obs Observation, candidate, current LookaheadSimulation) bool {
	if candidate.Score != current.Score {
		return candidate.Score > current.Score
	}
	if candidate.TotalLines != current.TotalLines {
		return candidate.TotalLines > current.TotalLines
	}
	if candidate.First.Score != current.First.Score {
		return candidate.First.Score > current.First.Score
	}
	candidateCost := placementInputCost(obs, candidate.First.Placement)
	currentCost := placementInputCost(obs, current.First.Placement)
	if candidateCost != currentCost {
		return candidateCost < currentCost
	}
	if candidate.First.Placement.Rotation != current.First.Placement.Rotation {
		return candidate.First.Placement.Rotation < current.First.Placement.Rotation
	}
	return candidate.First.Placement.TargetColumn < current.First.Placement.TargetColumn
}
