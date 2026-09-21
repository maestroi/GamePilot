package ramdiscover

import (
	"context"
	"fmt"
	"math"
	"sort"
)

type memoryImage struct {
	values map[uint16]byte
}

type candidateStats struct {
	region      string
	divergent   int
	samples     int
	deltaCounts map[int]int
}

// Run executes every action against an idle control trace from the exact same
// checkpoint. This differential design filters ordinary timers and animation
// state that would have changed even without input.
func Run(ctx context.Context, machine Machine, cfg Config) (Report, error) {
	cfg, err := normalizeConfig(cfg)
	if err != nil {
		return Report{}, err
	}
	root, err := machine.SaveCheckpoint()
	if err != nil {
		return Report{}, fmt.Errorf("ramdiscover: save root checkpoint: %w", err)
	}
	startFrame := machine.FrameCount()

	report := Report{SchemaVersion: 1, StartFrame: startFrame, Config: cfg}
	for _, action := range cfg.Actions {
		if err := machine.LoadCheckpoint(root); err != nil {
			return Report{}, fmt.Errorf("ramdiscover: restore root checkpoint before %s: %w", action.Name, err)
		}
		result, err := runAction(ctx, machine, cfg, action)
		if err != nil {
			return Report{}, err
		}
		report.Actions = append(report.Actions, result)
	}
	if err := machine.LoadCheckpoint(root); err != nil {
		return Report{}, fmt.Errorf("ramdiscover: restore root checkpoint after probes: %w", err)
	}
	report.Relations = inferRelations(report.Actions, cfg.Pairs)
	return report, nil
}

func normalizeConfig(cfg Config) (Config, error) {
	if len(cfg.Ranges) == 0 {
		cfg.Ranges = append([]MemoryRange(nil), DefaultRanges...)
	}
	for _, r := range cfg.Ranges {
		if err := r.validate(); err != nil {
			return Config{}, err
		}
	}
	if len(cfg.Actions) == 0 {
		cfg.Actions = DefaultConfig().Actions
	}
	if cfg.Trials == 0 {
		cfg.Trials = 1
	}
	if cfg.Trials < 1 {
		return Config{}, fmt.Errorf("ramdiscover: trials must be at least 1")
	}
	if cfg.TrialAdvanceFrames < 0 {
		return Config{}, fmt.Errorf("ramdiscover: trial advance frames cannot be negative")
	}
	if cfg.TopCandidates == 0 {
		cfg.TopCandidates = 32
	}
	if cfg.TopCandidates < 1 {
		return Config{}, fmt.Errorf("ramdiscover: top candidates must be at least 1")
	}
	seen := map[string]bool{}
	for i := range cfg.Actions {
		a := &cfg.Actions[i]
		if a.Name == "" {
			a.Name = string(a.Input)
		}
		if a.Name == "" {
			return Config{}, fmt.Errorf("ramdiscover: action %d has no name or input", i)
		}
		if seen[a.Name] {
			return Config{}, fmt.Errorf("ramdiscover: duplicate action name %q", a.Name)
		}
		seen[a.Name] = true
		if a.HoldFrames < 0 || a.ReleaseFrames < 0 || a.HoldFrames+a.ReleaseFrames < 1 {
			return Config{}, fmt.Errorf("ramdiscover: action %q must capture at least one non-negative frame", a.Name)
		}
	}
	return cfg, nil
}

func runAction(ctx context.Context, machine Machine, cfg Config, action Action) (ActionResult, error) {
	stats := map[uint16]*candidateStats{}
	for trial := 0; trial < cfg.Trials; trial++ {
		if err := ctx.Err(); err != nil {
			return ActionResult{}, err
		}
		base, err := machine.SaveCheckpoint()
		if err != nil {
			return ActionResult{}, fmt.Errorf("ramdiscover: %s trial %d save checkpoint: %w", action.Name, trial+1, err)
		}

		control, err := captureTrace(ctx, machine, cfg.Ranges, action, false)
		if err != nil {
			return ActionResult{}, fmt.Errorf("ramdiscover: %s trial %d control: %w", action.Name, trial+1, err)
		}
		if err := machine.LoadCheckpoint(base); err != nil {
			return ActionResult{}, fmt.Errorf("ramdiscover: %s trial %d restore for action: %w", action.Name, trial+1, err)
		}
		actionTrace, err := captureTrace(ctx, machine, cfg.Ranges, action, true)
		if err != nil {
			return ActionResult{}, fmt.Errorf("ramdiscover: %s trial %d action: %w", action.Name, trial+1, err)
		}
		compareTraces(cfg.Ranges, control, actionTrace, stats)

		if err := machine.LoadCheckpoint(base); err != nil {
			return ActionResult{}, fmt.Errorf("ramdiscover: %s trial %d restore after action: %w", action.Name, trial+1, err)
		}
		for i := 0; i < cfg.TrialAdvanceFrames; i++ {
			if err := ctx.Err(); err != nil {
				return ActionResult{}, err
			}
			machine.StepFrame()
		}
	}

	candidates := rankCandidates(stats)
	if len(candidates) > cfg.TopCandidates {
		candidates = candidates[:cfg.TopCandidates]
	}
	return ActionResult{Action: action, Candidates: candidates}, nil
}

func captureTrace(ctx context.Context, machine Machine, ranges []MemoryRange, action Action, active bool) ([]memoryImage, error) {
	frames := action.HoldFrames + action.ReleaseFrames
	trace := make([]memoryImage, 0, frames)
	pressed := false
	if active && action.HoldFrames > 0 {
		if err := machine.Press(action.Input); err != nil {
			return nil, err
		}
		pressed = true
	}
	defer func() {
		if pressed {
			_ = machine.Release(action.Input)
		}
	}()

	for frame := 0; frame < frames; frame++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if active && pressed && frame == action.HoldFrames {
			if err := machine.Release(action.Input); err != nil {
				return nil, err
			}
			pressed = false
		}
		machine.StepFrame()
		trace = append(trace, capture(machine, ranges))
	}
	return trace, nil
}

func capture(machine Machine, ranges []MemoryRange) memoryImage {
	image := memoryImage{values: make(map[uint16]byte, totalRangeBytes(ranges))}
	for _, r := range ranges {
		buf := make([]byte, int(r.End-r.Start)+1)
		machine.PeekInto(r.Start, buf)
		for i, value := range buf {
			image.values[r.Start+uint16(i)] = value
		}
	}
	return image
}

func totalRangeBytes(ranges []MemoryRange) int {
	total := 0
	for _, r := range ranges {
		total += int(r.End-r.Start) + 1
	}
	return total
}

func compareTraces(ranges []MemoryRange, control, action []memoryImage, stats map[uint16]*candidateStats) {
	for frame := range control {
		for _, r := range ranges {
			for addr := r.Start; ; addr++ {
				c := control[frame].values[addr]
				a := action[frame].values[addr]
				stat := stats[addr]
				if stat == nil {
					stat = &candidateStats{region: r.Name, deltaCounts: map[int]int{}}
					stats[addr] = stat
				}
				stat.samples++
				if a != c {
					stat.divergent++
					stat.deltaCounts[signedByteDelta(a, c)]++
				}
				if addr == r.End {
					break
				}
			}
		}
	}
}

func signedByteDelta(action, control byte) int {
	delta := int(action) - int(control)
	if delta > 127 {
		delta -= 256
	} else if delta < -128 {
		delta += 256
	}
	return delta
}

func rankCandidates(stats map[uint16]*candidateStats) []Candidate {
	candidates := make([]Candidate, 0)
	for addr, stat := range stats {
		if stat.divergent == 0 {
			continue
		}
		dominantDelta, dominantCount := dominant(stat.deltaCounts)
		divergenceRate := float64(stat.divergent) / float64(stat.samples)
		consistency := float64(dominantCount) / float64(stat.divergent)
		// Divergence is primary; consistency breaks ties in favor of simple state
		// variables rather than noisy bitfields or transient composite bytes.
		score := divergenceRate * (0.5 + 0.5*consistency)
		candidates = append(candidates, Candidate{
			Address:          fmt.Sprintf("0x%04X", addr),
			AddressDecimal:   addr,
			Region:           stat.region,
			DivergentFrames:  stat.divergent,
			Samples:          stat.samples,
			DivergenceRate:   round6(divergenceRate),
			DominantDelta:    dominantDelta,
			DeltaConsistency: round6(consistency),
			Score:            round6(score),
		})
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Score == candidates[j].Score {
			return candidates[i].AddressDecimal < candidates[j].AddressDecimal
		}
		return candidates[i].Score > candidates[j].Score
	})
	return candidates
}

func dominant(counts map[int]int) (value, count int) {
	for candidate, n := range counts {
		if n > count || (n == count && abs(candidate) < abs(value)) {
			value, count = candidate, n
		}
	}
	return value, count
}

func inferRelations(actions []ActionResult, pairs []ActionPair) []Relation {
	byName := make(map[string]map[uint16]Candidate, len(actions))
	for _, result := range actions {
		candidates := make(map[uint16]Candidate, len(result.Candidates))
		for _, candidate := range result.Candidates {
			candidates[candidate.AddressDecimal] = candidate
		}
		byName[result.Action.Name] = candidates
	}

	var relations []Relation
	for _, pair := range pairs {
		first := byName[pair.First]
		second := byName[pair.Second]
		for addr, a := range first {
			b, ok := second[addr]
			if !ok || a.DominantDelta == 0 || b.DominantDelta == 0 || a.DominantDelta != -b.DominantDelta {
				continue
			}
			score := math.Sqrt(a.Score * b.Score)
			relations = append(relations, Relation{
				Kind:        "opposite_delta",
				Actions:     [2]string{pair.First, pair.Second},
				Address:     a.Address,
				Region:      a.Region,
				FirstDelta:  a.DominantDelta,
				SecondDelta: b.DominantDelta,
				Score:       round6(score),
			})
		}
	}
	sort.Slice(relations, func(i, j int) bool {
		if relations[i].Score == relations[j].Score {
			return relations[i].Address < relations[j].Address
		}
		return relations[i].Score > relations[j].Score
	})
	return relations
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func round6(v float64) float64 {
	return math.Round(v*1_000_000) / 1_000_000
}
