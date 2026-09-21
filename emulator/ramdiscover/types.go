package ramdiscover

import "fmt"

// Input is a semantic Game Boy control used by deterministic RAM probes.
type Input string

const (
	InputLeft  Input = "left"
	InputRight Input = "right"
	InputUp    Input = "up"
	InputDown  Input = "down"
	InputA     Input = "a"
	InputB     Input = "b"
)

// DefaultInputs are the controls most useful for discovering gameplay state.
var DefaultInputs = []Input{InputLeft, InputRight, InputUp, InputDown, InputA, InputB}

// MemoryRange describes an inclusive address range captured on every trace frame.
type MemoryRange struct {
	Name  string `json:"name"`
	Start uint16 `json:"start"`
	End   uint16 `json:"end"`
}

// DefaultRanges intentionally focus on software-owned memory rather than volatile
// hardware registers: 8 KiB WRAM and 127 bytes of HRAM.
var DefaultRanges = []MemoryRange{
	{Name: "wram", Start: 0xC000, End: 0xDFFF},
	{Name: "hram", Start: 0xFF80, End: 0xFFFE},
}

func (r MemoryRange) validate() error {
	if r.Name == "" {
		return fmt.Errorf("ramdiscover: memory range name is required")
	}
	if r.End < r.Start {
		return fmt.Errorf("ramdiscover: memory range %q ends before it starts", r.Name)
	}
	return nil
}

// Action configures one controlled input experiment.
type Action struct {
	Name          string `json:"name"`
	Input         Input  `json:"input"`
	HoldFrames    int    `json:"hold_frames"`
	ReleaseFrames int    `json:"release_frames"`
}

// ActionPair asks the analyzer to look for addresses with opposite signed deltas
// under two actions, a strong signal for coordinates and modulo counters.
type ActionPair struct {
	First  string `json:"first"`
	Second string `json:"second"`
}

// Config controls deterministic probing and ranking.
type Config struct {
	Ranges             []MemoryRange `json:"ranges"`
	Actions            []Action      `json:"actions"`
	Pairs              []ActionPair  `json:"pairs"`
	Trials             int           `json:"trials"`
	TrialAdvanceFrames int           `json:"trial_advance_frames"`
	TopCandidates      int           `json:"top_candidates"`
}

// DefaultConfig is deliberately small enough for interactive use while still
// sampling more than one falling-piece frame.
func DefaultConfig() Config {
	actions := make([]Action, 0, len(DefaultInputs))
	for _, input := range DefaultInputs {
		actions = append(actions, Action{Name: string(input), Input: input, HoldFrames: 1, ReleaseFrames: 2})
	}
	return Config{
		Ranges:  append([]MemoryRange(nil), DefaultRanges...),
		Actions: actions,
		Pairs: []ActionPair{
			{First: "left", Second: "right"},
			{First: "up", Second: "down"},
			{First: "a", Second: "b"},
		},
		Trials:             3,
		TrialAdvanceFrames: 2,
		TopCandidates:      32,
	}
}

// Machine is the minimal deterministic emulator surface needed by the RAM investigator.
type Machine interface {
	SaveCheckpoint() ([]byte, error)
	LoadCheckpoint([]byte) error
	PeekInto(addr uint16, dst []byte)
	FrameCount() uint64
	Press(Input) error
	Release(Input) error
	StepFrame()
}

// Candidate summarizes one address whose action trace diverged from an idle
// control trace captured from the exact same checkpoint.
type Candidate struct {
	Address          string  `json:"address"`
	AddressDecimal   uint16  `json:"address_decimal"`
	Region           string  `json:"region"`
	DivergentFrames  int     `json:"divergent_frames"`
	Samples          int     `json:"samples"`
	DivergenceRate   float64 `json:"divergence_rate"`
	DominantDelta    int     `json:"dominant_delta"`
	DeltaConsistency float64 `json:"delta_consistency"`
	Score            float64 `json:"score"`
}

// ActionResult contains ranked candidates for one input.
type ActionResult struct {
	Action     Action      `json:"action"`
	Candidates []Candidate `json:"candidates"`
}

// Relation is cross-action evidence. OppositeDelta is especially useful for
// finding X/Y coordinates and increment/decrement state.
type Relation struct {
	Kind        string    `json:"kind"`
	Actions     [2]string `json:"actions"`
	Address     string    `json:"address"`
	Region      string    `json:"region"`
	FirstDelta  int       `json:"first_delta"`
	SecondDelta int       `json:"second_delta"`
	Score       float64   `json:"score"`
}

// Report is intentionally model-friendly JSON. A later semantic classifier can
// consume this without touching emulator memory directly.
type Report struct {
	SchemaVersion int            `json:"schema_version"`
	ROMHash       string         `json:"rom_sha256,omitempty"`
	Profile       string         `json:"profile,omitempty"`
	StartFrame    uint64         `json:"start_frame"`
	Config        Config         `json:"config"`
	Actions       []ActionResult `json:"actions"`
	Relations     []Relation     `json:"relations"`
}
