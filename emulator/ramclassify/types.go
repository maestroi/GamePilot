package ramclassify

import (
	"context"

	"github.com/maestroi/GamePilot/emulator/ramdiscover"
)

// Question is a bounded semantic choice presented to a decision engine.
type Question struct {
	Type         string            `json:"type"`
	Instructions string            `json:"instructions"`
	Criteria     map[string]string `json:"criteria"`
}

// Answer is one bounded choice distribution from a decision engine.
type Answer struct {
	Type          string             `json:"type"`
	Choice        string             `json:"choice"`
	Probabilities map[string]float64 `json:"probabilities"`
	Confidence    float64            `json:"confidence"`
}

// DecisionResponse is the subset of the Jev/OpenJev response used by classification.
type DecisionResponse struct {
	Model   string            `json:"model"`
	Answers map[string]Answer `json:"answers"`
}

// DecisionEngine keeps semantic classification independent of a particular
// Jev-compatible server. OpenJevClient is the default implementation.
type DecisionEngine interface {
	Decide(ctx context.Context, model string, state any, questions map[string]Question) (DecisionResponse, error)
}

// SemanticOption is one bounded meaning OpenJev may assign to an address.
type SemanticOption struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	Scalar      bool   `json:"scalar"`
}

// ActionEvidence is the compact causal evidence for one address under one input.
type ActionEvidence struct {
	Action           string  `json:"action"`
	DominantDelta    int     `json:"dominant_delta"`
	DivergenceRate   float64 `json:"divergence_rate"`
	DeltaConsistency float64 `json:"delta_consistency"`
	Score            float64 `json:"score"`
}

// RelationEvidence captures cross-input evidence such as left/right opposite deltas.
type RelationEvidence struct {
	Kind        string    `json:"kind"`
	Actions     [2]string `json:"actions"`
	FirstDelta  int       `json:"first_delta"`
	SecondDelta int       `json:"second_delta"`
	Score       float64   `json:"score"`
}

// AddressEvidence is the model-facing representation of one candidate byte.
type AddressEvidence struct {
	Address        string             `json:"address"`
	AddressDecimal uint16             `json:"address_decimal"`
	Region         string             `json:"region"`
	EvidenceScore  float64            `json:"evidence_score"`
	Actions        []ActionEvidence   `json:"actions"`
	Relations      []RelationEvidence `json:"relations,omitempty"`
}

// Config controls semantic classification. It deliberately separates model
// uncertainty thresholds from the deterministic RAM-discovery score.
type Config struct {
	Model          string  `json:"model"`
	MinProbability float64 `json:"min_probability"`
	MinConfidence  float64 `json:"min_confidence"`
	MaxAddresses   int     `json:"max_addresses"`
	BatchSize      int     `json:"batch_size"`
}

func DefaultConfig() Config {
	return Config{
		Model:          "openjev-latest",
		MinProbability: 0.55,
		MinConfidence:  0.10,
		MaxAddresses:   48,
		BatchSize:      16,
	}
}

// Classification preserves the full bounded distribution returned by OpenJev.
type Classification struct {
	Address         string             `json:"address"`
	AddressDecimal  uint16             `json:"address_decimal"`
	Region          string             `json:"region"`
	Selected        string             `json:"selected"`
	Probability     float64            `json:"probability"`
	Confidence      float64            `json:"confidence"`
	Probabilities   map[string]float64 `json:"probabilities"`
	EvidenceScore   float64            `json:"evidence_score"`
	Accepted        bool               `json:"accepted"`
	RejectionReason string             `json:"rejection_reason,omitempty"`
}

// Field is one accepted scalar game-state mapping.
type Field struct {
	Semantic       string  `json:"semantic"`
	Address        string  `json:"address"`
	AddressDecimal uint16  `json:"address_decimal"`
	Region         string  `json:"region"`
	Encoding       string  `json:"encoding"`
	Probability    float64 `json:"probability"`
	Confidence     float64 `json:"confidence"`
	EvidenceScore  float64 `json:"evidence_score"`
}

// Schema is the classifier output consumed by later GamePilot profile generation.
type Schema struct {
	SchemaVersion   int                `json:"schema_version"`
	ROMHash         string             `json:"rom_sha256,omitempty"`
	Profile         string             `json:"profile,omitempty"`
	SourceFrame     uint64             `json:"source_frame"`
	Classifier      ClassifierMetadata `json:"classifier"`
	State           map[string]Field   `json:"state"`
	Classifications []Classification   `json:"classifications"`
	SourceConfig    ramdiscover.Config `json:"source_discovery_config"`
}

// ClassifierMetadata makes generated schemas reproducible and auditable.
type ClassifierMetadata struct {
	Provider       string  `json:"provider"`
	Model          string  `json:"model"`
	MinProbability float64 `json:"min_probability"`
	MinConfidence  float64 `json:"min_confidence"`
	MaxAddresses   int     `json:"max_addresses"`
	BatchSize      int     `json:"batch_size"`
}
