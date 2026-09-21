package ramclassify

import (
	"context"
	"fmt"
	"sort"

	"github.com/maestroi/GamePilot/emulator/ramdiscover"
)

// Classify converts deterministic RAM evidence into bounded semantic labels using OpenJev.
func Classify(ctx context.Context, engine DecisionEngine, report ramdiscover.Report, cfg Config) (Schema, error) {
	cfg, err := normalizeConfig(cfg)
	if err != nil {
		return Schema{}, err
	}
	evidence := collectEvidence(report, cfg.MaxAddresses)
	if len(evidence) == 0 {
		return Schema{}, fmt.Errorf("ramclassify: discovery report contains no candidate addresses")
	}
	options := vocabulary(report.Profile)
	optionByID := optionMap(options)

	classifications := make([]Classification, 0, len(evidence))
	for start := 0; start < len(evidence); start += cfg.BatchSize {
		end := start + cfg.BatchSize
		if end > len(evidence) {
			end = len(evidence)
		}
		batch := evidence[start:end]
		batchClassifications, err := classifyBatch(ctx, engine, report, cfg, options, batch)
		if err != nil {
			return Schema{}, err
		}
		classifications = append(classifications, batchClassifications...)
	}

	schema := Schema{
		SchemaVersion: 1,
		ROMHash:       report.ROMHash,
		Profile:       report.Profile,
		SourceFrame:   report.StartFrame,
		Classifier: ClassifierMetadata{
			Provider:       "openjev",
			Model:          cfg.Model,
			MinProbability: cfg.MinProbability,
			MinConfidence:  cfg.MinConfidence,
			MaxAddresses:   cfg.MaxAddresses,
			BatchSize:      cfg.BatchSize,
		},
		State:           map[string]Field{},
		Classifications: classifications,
		SourceConfig:    report.Config,
	}

	// Scalar semantics get one winner. Non-scalar/composite labels remain useful
	// in classifications but are not emitted as misleading u8 state fields.
	best := map[string]int{}
	for i := range schema.Classifications {
		classification := &schema.Classifications[i]
		option, known := optionByID[classification.Selected]
		if !known || classification.Selected == insufficientEvidence {
			classification.Accepted = false
			if classification.RejectionReason == "" {
				classification.RejectionReason = "insufficient_evidence"
			}
			continue
		}
		if classification.Probability < cfg.MinProbability {
			classification.Accepted = false
			classification.RejectionReason = "probability_below_threshold"
			continue
		}
		if classification.Confidence < cfg.MinConfidence {
			classification.Accepted = false
			classification.RejectionReason = "confidence_below_threshold"
			continue
		}
		if !option.Scalar {
			classification.Accepted = false
			classification.RejectionReason = "non_scalar_semantic"
			continue
		}
		classification.Accepted = true
		if previous, ok := best[classification.Selected]; ok {
			if classificationQuality(*classification) > classificationQuality(schema.Classifications[previous]) {
				schema.Classifications[previous].Accepted = false
				schema.Classifications[previous].RejectionReason = "lower_ranked_duplicate"
				best[classification.Selected] = i
			} else {
				classification.Accepted = false
				classification.RejectionReason = "lower_ranked_duplicate"
			}
		} else {
			best[classification.Selected] = i
		}
	}

	keys := make([]string, 0, len(best))
	for semantic := range best {
		keys = append(keys, semantic)
	}
	sort.Strings(keys)
	for _, semantic := range keys {
		classification := schema.Classifications[best[semantic]]
		if !classification.Accepted {
			continue
		}
		schema.State[semantic] = Field{
			Semantic:       semantic,
			Address:        classification.Address,
			AddressDecimal: classification.AddressDecimal,
			Region:         classification.Region,
			Encoding:       "u8",
			Probability:    classification.Probability,
			Confidence:     classification.Confidence,
			EvidenceScore:  classification.EvidenceScore,
		}
	}
	return schema, nil
}

func classifyBatch(ctx context.Context, engine DecisionEngine, report ramdiscover.Report, cfg Config, options []SemanticOption, evidence []AddressEvidence) ([]Classification, error) {
	criteria := make(map[string]string, len(options))
	for _, option := range options {
		criteria[option.ID] = option.Description
	}
	questions := make(map[string]Question, len(evidence))
	for i, item := range evidence {
		questions[questionID(i)] = Question{
			Type:         "choice",
			Instructions: fmt.Sprintf("Which semantic role best explains evidence record %d for RAM address %s? Prefer insufficient_evidence unless the causal input pattern specifically supports another role.", i, item.Address),
			Criteria:     criteria,
		}
	}
	state := map[string]any{
		"task":       "Classify Game Boy RAM bytes from deterministic action-vs-idle causal traces. Deltas are action value minus idle-control value at the same frame. Opposite-delta relations are strong evidence for coordinates or small cyclic control state. Do not infer score, level, progress, or identity unless the evidence actually distinguishes those meanings.",
		"profile":    report.Profile,
		"rom_sha256": report.ROMHash,
		"evidence":   evidence,
	}
	if engine == nil {
		return nil, fmt.Errorf("ramclassify: decision engine is nil")
	}
	response, err := engine.Decide(ctx, cfg.Model, state, questions)
	if err != nil {
		return nil, err
	}
	out := make([]Classification, 0, len(evidence))
	for i, item := range evidence {
		id := questionID(i)
		answer, ok := response.Answers[id]
		if !ok {
			return nil, fmt.Errorf("ramclassify: OpenJev response missing answer %q", id)
		}
		if answer.Type != "choice" {
			return nil, fmt.Errorf("ramclassify: OpenJev answer %q has type %q, want choice", id, answer.Type)
		}
		if _, ok := criteria[answer.Choice]; !ok {
			return nil, fmt.Errorf("ramclassify: OpenJev answer %q chose unknown option %q", id, answer.Choice)
		}
		probability, ok := answer.Probabilities[answer.Choice]
		if !ok {
			return nil, fmt.Errorf("ramclassify: OpenJev answer %q chose %q without a probability", id, answer.Choice)
		}
		if probability < 0 || probability > 1 || answer.Confidence < 0 || answer.Confidence > 1 {
			return nil, fmt.Errorf("ramclassify: OpenJev answer %q contains probability/confidence outside 0..1", id)
		}
		out = append(out, Classification{
			Address:        item.Address,
			AddressDecimal: item.AddressDecimal,
			Region:         item.Region,
			Selected:       answer.Choice,
			Probability:    round6(probability),
			Confidence:     round6(answer.Confidence),
			Probabilities:  answer.Probabilities,
			EvidenceScore:  item.EvidenceScore,
		})
	}
	return out, nil
}

func normalizeConfig(cfg Config) (Config, error) {
	defaults := DefaultConfig()
	if cfg == (Config{}) {
		cfg = defaults
	}
	if cfg.Model == "" {
		cfg.Model = defaults.Model
	}
	if cfg.MaxAddresses == 0 {
		cfg.MaxAddresses = defaults.MaxAddresses
	}
	if cfg.BatchSize == 0 {
		cfg.BatchSize = defaults.BatchSize
	}
	if cfg.MinProbability < 0 || cfg.MinProbability > 1 {
		return Config{}, fmt.Errorf("ramclassify: minimum probability must be in 0..1")
	}
	if cfg.MinConfidence < 0 || cfg.MinConfidence > 1 {
		return Config{}, fmt.Errorf("ramclassify: minimum confidence must be in 0..1")
	}
	if cfg.MaxAddresses < 1 {
		return Config{}, fmt.Errorf("ramclassify: max addresses must be at least 1")
	}
	if cfg.BatchSize < 1 {
		return Config{}, fmt.Errorf("ramclassify: batch size must be at least 1")
	}
	return cfg, nil
}

func classificationQuality(c Classification) float64 {
	return c.Probability * (0.5 + 0.5*c.Confidence) * c.EvidenceScore
}

func questionID(i int) string {
	return fmt.Sprintf("address_%03d", i)
}
