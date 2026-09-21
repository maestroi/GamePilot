package ramclassify

import (
	"math"
	"sort"

	"github.com/maestroi/GamePilot/emulator/ramdiscover"
)

func collectEvidence(report ramdiscover.Report, limit int) []AddressEvidence {
	type aggregate struct {
		address uint16
		text    string
		region  string
		actions []ActionEvidence
		rels    []RelationEvidence
		score   float64
	}
	byAddress := map[uint16]*aggregate{}

	for _, action := range report.Actions {
		for _, candidate := range action.Candidates {
			a := byAddress[candidate.AddressDecimal]
			if a == nil {
				a = &aggregate{address: candidate.AddressDecimal, text: candidate.Address, region: candidate.Region}
				byAddress[candidate.AddressDecimal] = a
			}
			a.actions = append(a.actions, ActionEvidence{
				Action:           action.Action.Name,
				DominantDelta:    candidate.DominantDelta,
				DivergenceRate:   candidate.DivergenceRate,
				DeltaConsistency: candidate.DeltaConsistency,
				Score:            candidate.Score,
			})
			if candidate.Score > a.score {
				a.score = candidate.Score
			}
		}
	}

	for _, relation := range report.Relations {
		var address uint16
		for candidateAddress, a := range byAddress {
			if a.text == relation.Address {
				address = candidateAddress
				break
			}
		}
		a := byAddress[address]
		if a == nil || a.text != relation.Address {
			continue
		}
		a.rels = append(a.rels, RelationEvidence{
			Kind:        relation.Kind,
			Actions:     relation.Actions,
			FirstDelta:  relation.FirstDelta,
			SecondDelta: relation.SecondDelta,
			Score:       relation.Score,
		})
		// A relation is stronger semantic evidence than one action in isolation.
		a.score = math.Max(a.score, math.Min(1, relation.Score+0.05))
	}

	out := make([]AddressEvidence, 0, len(byAddress))
	for _, a := range byAddress {
		sort.Slice(a.actions, func(i, j int) bool { return a.actions[i].Action < a.actions[j].Action })
		sort.Slice(a.rels, func(i, j int) bool {
			if a.rels[i].Score == a.rels[j].Score {
				return a.rels[i].Actions[0] < a.rels[j].Actions[0]
			}
			return a.rels[i].Score > a.rels[j].Score
		})
		out = append(out, AddressEvidence{
			Address:        a.text,
			AddressDecimal: a.address,
			Region:         a.region,
			EvidenceScore:  round6(a.score),
			Actions:        a.actions,
			Relations:      a.rels,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].EvidenceScore == out[j].EvidenceScore {
			return out[i].AddressDecimal < out[j].AddressDecimal
		}
		return out[i].EvidenceScore > out[j].EvidenceScore
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

func round6(v float64) float64 {
	return math.Round(v*1_000_000) / 1_000_000
}
