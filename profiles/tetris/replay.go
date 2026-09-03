package tetris

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

const (
	ReplayFormatVersion = 1
	ReplayStartupTypeA0 = "type_a_level_0"
	stateHashVersion    = 1
)

// Replay is a planner-independent record of deterministic game-level actions.
// It intentionally records decoded Tetris state rather than emulator internals,
// so the same format can be produced by heuristic, LLM, or future planners.
type Replay struct {
	FormatVersion int          `json:"format_version"`
	Profile       string       `json:"profile"`
	ROMSHA256     string       `json:"rom_sha256"`
	Startup       string       `json:"startup"`
	Initial       ReplayState  `json:"initial"`
	Moves         []ReplayMove `json:"moves"`
}

type ReplayMove struct {
	Index     int         `json:"index"`
	Placement Placement   `json:"placement"`
	Before    ReplayState `json:"before"`
	After     ReplayState `json:"after"`
}

// ReplayState keeps both the canonical hash and the decoded observation. The
// observation makes failures inspectable while the hash gives a compact stable
// equality check. Frame is stored in Observation but deliberately excluded from
// the state hash and verified separately.
type ReplayState struct {
	StateHash   string      `json:"state_hash"`
	Observation Observation `json:"observation"`
}

type canonicalObservation struct {
	Version      int                           `json:"version"`
	Board        [BoardRows][BoardColumns]Cell `json:"board"`
	CurrentPiece Piece                         `json:"current_piece"`
	NextPiece    Piece                         `json:"next_piece"`
	Score        int                           `json:"score"`
	Level        int                           `json:"level"`
	Lines        int                           `json:"lines"`
	Ready        bool                          `json:"ready"`
	GameOver     bool                          `json:"game_over"`
	GameState    uint8                         `json:"game_state"`
}

// ObservationStateHash returns a versioned SHA-256 over the planner-visible
// Tetris state. Frame is excluded so state equality and deterministic timing can
// be diagnosed independently.
func ObservationStateHash(obs Observation) string {
	payload, err := json.Marshal(canonicalObservation{
		Version:      stateHashVersion,
		Board:        obs.Board,
		CurrentPiece: obs.CurrentPiece,
		NextPiece:    obs.NextPiece,
		Score:        obs.Score,
		Level:        obs.Level,
		Lines:        obs.Lines,
		Ready:        obs.Ready,
		GameOver:     obs.GameOver,
		GameState:    obs.GameState,
	})
	if err != nil {
		panic(fmt.Sprintf("tetris: marshal canonical observation: %v", err))
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func CaptureReplayState(obs Observation) ReplayState {
	return ReplayState{StateHash: ObservationStateHash(obs), Observation: obs}
}

func NewReplay(romHash string, initial Observation) Replay {
	return Replay{
		FormatVersion: ReplayFormatVersion,
		Profile:       ProfileID,
		ROMSHA256:     strings.ToLower(strings.TrimSpace(romHash)),
		Startup:       ReplayStartupTypeA0,
		Initial:       CaptureReplayState(initial),
		Moves:         make([]ReplayMove, 0),
	}
}

// Append records one completed placement and enforces continuity with the
// previous replay state before mutating the replay.
func (r *Replay) Append(before Observation, placement Placement, after Observation) error {
	beforeState := CaptureReplayState(before)
	wantBefore := r.Initial
	if len(r.Moves) > 0 {
		wantBefore = r.Moves[len(r.Moves)-1].After
	}
	if beforeState.StateHash != wantBefore.StateHash || before.Frame != wantBefore.Observation.Frame {
		return fmt.Errorf("tetris: replay append is not continuous: expected prior frame/hash %d/%s, got %d/%s",
			wantBefore.Observation.Frame, wantBefore.StateHash, before.Frame, beforeState.StateHash)
	}

	r.Moves = append(r.Moves, ReplayMove{
		Index:     len(r.Moves) + 1,
		Placement: placement,
		Before:    beforeState,
		After:     CaptureReplayState(after),
	})
	return nil
}

func (r Replay) Validate() error {
	if r.FormatVersion != ReplayFormatVersion {
		return fmt.Errorf("tetris: unsupported replay format version %d; supported version is %d", r.FormatVersion, ReplayFormatVersion)
	}
	if r.Profile != ProfileID {
		return fmt.Errorf("tetris: replay profile %q does not match %q", r.Profile, ProfileID)
	}
	if strings.TrimSpace(r.ROMSHA256) == "" {
		return fmt.Errorf("tetris: replay ROM SHA-256 is empty")
	}
	if r.Startup != ReplayStartupTypeA0 {
		return fmt.Errorf("tetris: unsupported replay startup %q", r.Startup)
	}
	if err := validateReplayState("initial", r.Initial); err != nil {
		return err
	}

	prior := r.Initial
	for i, move := range r.Moves {
		wantIndex := i + 1
		if move.Index != wantIndex {
			return fmt.Errorf("tetris: replay move index %d at position %d; want %d", move.Index, i, wantIndex)
		}
		if err := validateReplayState(fmt.Sprintf("move %d before", move.Index), move.Before); err != nil {
			return err
		}
		if err := validateReplayState(fmt.Sprintf("move %d after", move.Index), move.After); err != nil {
			return err
		}
		if move.Before.StateHash != prior.StateHash || move.Before.Observation.Frame != prior.Observation.Frame {
			return fmt.Errorf("tetris: replay move %d does not continue from the previous recorded state", move.Index)
		}
		prior = move.After
	}
	return nil
}

func validateReplayState(label string, state ReplayState) error {
	want := ObservationStateHash(state.Observation)
	if state.StateHash != want {
		return fmt.Errorf("tetris: replay %s has invalid state hash: recorded %s, canonical %s", label, state.StateHash, want)
	}
	return nil
}

func WriteReplay(w io.Writer, replay Replay) error {
	if err := replay.Validate(); err != nil {
		return err
	}
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(replay); err != nil {
		return fmt.Errorf("tetris: write replay: %w", err)
	}
	return nil
}

func ReadReplay(r io.Reader) (Replay, error) {
	decoder := json.NewDecoder(r)
	decoder.DisallowUnknownFields()
	var replay Replay
	if err := decoder.Decode(&replay); err != nil {
		return Replay{}, fmt.Errorf("tetris: read replay: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return Replay{}, fmt.Errorf("tetris: read replay: unexpected trailing JSON value")
		}
		return Replay{}, fmt.Errorf("tetris: read replay trailing data: %w", err)
	}
	if err := replay.Validate(); err != nil {
		return Replay{}, err
	}
	return replay, nil
}

// VerifyReplay assumes the caller has already performed the replay's declared
// deterministic startup. It checks the initial state, re-executes every
// Placement through the strict controller, and fails at the first divergence.
func VerifyReplay(ctx context.Context, emu controllerEmulator, romHash string, replay Replay) (Observation, error) {
	if err := replay.Validate(); err != nil {
		return Observation{}, err
	}
	if !strings.EqualFold(strings.TrimSpace(romHash), replay.ROMSHA256) {
		return Observation{}, fmt.Errorf("tetris: replay ROM SHA-256 %s does not match loaded ROM %s", replay.ROMSHA256, romHash)
	}

	obs, err := Observe(emu)
	if err != nil {
		return Observation{}, err
	}
	if err := compareReplayState("initial", replay.Initial, obs); err != nil {
		return Observation{}, err
	}

	for _, move := range replay.Moves {
		if err := compareReplayState(fmt.Sprintf("move %d before", move.Index), move.Before, obs); err != nil {
			return Observation{}, err
		}
		obs, err = ExecutePlacement(ctx, emu, move.Placement)
		if err != nil {
			return Observation{}, fmt.Errorf("tetris: replay move %d execute: %w", move.Index, err)
		}
		if err := compareReplayState(fmt.Sprintf("move %d after", move.Index), move.After, obs); err != nil {
			return Observation{}, err
		}
	}
	return obs, nil
}

func compareReplayState(label string, expected ReplayState, actual Observation) error {
	actualState := CaptureReplayState(actual)
	var diffs []string
	if expected.StateHash != actualState.StateHash {
		diffs = append(diffs, fmt.Sprintf("state_hash expected=%s actual=%s", expected.StateHash, actualState.StateHash))
	}
	want := expected.Observation
	if want.Frame != actual.Frame {
		diffs = append(diffs, fmt.Sprintf("frame expected=%d actual=%d", want.Frame, actual.Frame))
	}
	if want.Score != actual.Score {
		diffs = append(diffs, fmt.Sprintf("score expected=%d actual=%d", want.Score, actual.Score))
	}
	if want.Lines != actual.Lines {
		diffs = append(diffs, fmt.Sprintf("lines expected=%d actual=%d", want.Lines, actual.Lines))
	}
	if want.Level != actual.Level {
		diffs = append(diffs, fmt.Sprintf("level expected=%d actual=%d", want.Level, actual.Level))
	}
	if want.CurrentPiece != actual.CurrentPiece {
		diffs = append(diffs, fmt.Sprintf("current_piece expected=%+v actual=%+v", want.CurrentPiece, actual.CurrentPiece))
	}
	if want.NextPiece != actual.NextPiece {
		diffs = append(diffs, fmt.Sprintf("next_piece expected=%+v actual=%+v", want.NextPiece, actual.NextPiece))
	}
	if want.Ready != actual.Ready {
		diffs = append(diffs, fmt.Sprintf("ready expected=%t actual=%t", want.Ready, actual.Ready))
	}
	if want.GameOver != actual.GameOver {
		diffs = append(diffs, fmt.Sprintf("game_over expected=%t actual=%t", want.GameOver, actual.GameOver))
	}
	if want.GameState != actual.GameState {
		diffs = append(diffs, fmt.Sprintf("game_state expected=%d actual=%d", want.GameState, actual.GameState))
	}
	if row, column, ok := firstBoardDifference(want.Board, actual.Board); ok {
		diffs = append(diffs, fmt.Sprintf("board[%d][%d] expected=%d actual=%d", row, column, want.Board[row][column], actual.Board[row][column]))
	}
	if len(diffs) == 0 {
		return nil
	}
	return fmt.Errorf("tetris: replay mismatch at %s: %s", label, strings.Join(diffs, "; "))
}

func firstBoardDifference(a, b [BoardRows][BoardColumns]Cell) (row, column int, ok bool) {
	for row := 0; row < BoardRows; row++ {
		for column := 0; column < BoardColumns; column++ {
			if a[row][column] != b[row][column] {
				return row, column, true
			}
		}
	}
	return 0, 0, false
}
