package tetris

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestObservationStateHashIgnoresFrame(t *testing.T) {
	emu := newControllerFake()
	obs, err := Observe(emu)
	if err != nil {
		t.Fatal(err)
	}

	other := obs
	other.Frame += 123
	if got, want := ObservationStateHash(other), ObservationStateHash(obs); got != want {
		t.Fatalf("hash with changed frame = %s, want %s", got, want)
	}

	other.Board[BoardRows-1][0] = Filled
	if got, want := ObservationStateHash(other), ObservationStateHash(obs); got == want {
		t.Fatalf("hash with changed board = %s, want a different hash", got)
	}
}

func TestReplayJSONRoundTrip(t *testing.T) {
	replay := recordOneFakePlacement(t)

	var buf bytes.Buffer
	if err := WriteReplay(&buf, replay); err != nil {
		t.Fatal(err)
	}
	decoded, err := ReadReplay(&buf)
	if err != nil {
		t.Fatal(err)
	}

	if decoded.FormatVersion != ReplayFormatVersion {
		t.Fatalf("format version = %d, want %d", decoded.FormatVersion, ReplayFormatVersion)
	}
	if decoded.ROMSHA256 != Rev1SHA256 {
		t.Fatalf("ROM hash = %q, want %q", decoded.ROMSHA256, Rev1SHA256)
	}
	if len(decoded.Moves) != 1 {
		t.Fatalf("moves = %d, want 1", len(decoded.Moves))
	}
	if decoded.Moves[0].Placement != replay.Moves[0].Placement {
		t.Fatalf("placement = %+v, want %+v", decoded.Moves[0].Placement, replay.Moves[0].Placement)
	}
}

func TestVerifyReplayReexecutesPlacement(t *testing.T) {
	replay := recordOneFakePlacement(t)

	verifyEmu := newControllerFake()
	obs, err := VerifyReplay(context.Background(), verifyEmu, Rev1SHA256, replay)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := ObservationStateHash(obs), replay.Moves[0].After.StateHash; got != want {
		t.Fatalf("final hash = %s, want %s", got, want)
	}
}

func TestVerifyReplayReportsFirstStateMismatch(t *testing.T) {
	replay := recordOneFakePlacement(t)
	replay.Moves[0].After.Observation.Score = 999
	replay.Moves[0].After.StateHash = ObservationStateHash(replay.Moves[0].After.Observation)

	verifyEmu := newControllerFake()
	_, err := VerifyReplay(context.Background(), verifyEmu, Rev1SHA256, replay)
	if err == nil {
		t.Fatal("VerifyReplay returned nil error for divergent replay")
	}
	if !strings.Contains(err.Error(), "move 1 after") || !strings.Contains(err.Error(), "score expected=999 actual=0") {
		t.Fatalf("error = %v, want move/score mismatch details", err)
	}
}

func TestReplayAppendRejectsDiscontinuousState(t *testing.T) {
	emu := newControllerFake()
	initial, err := Observe(emu)
	if err != nil {
		t.Fatal(err)
	}
	replay := NewReplay(Rev1SHA256, initial)

	before := initial
	before.Frame++
	if err := replay.Append(before, Placement{Rotation: 0, TargetColumn: 0}, initial); err == nil {
		t.Fatal("Append returned nil error for discontinuous frame")
	}
}

func recordOneFakePlacement(t *testing.T) Replay {
	t.Helper()
	emu := newControllerFake()
	before, err := Observe(emu)
	if err != nil {
		t.Fatal(err)
	}
	replay := NewReplay(Rev1SHA256, before)
	placement := Placement{Rotation: 1, TargetColumn: 6}
	after, err := ExecutePlacement(context.Background(), emu, placement)
	if err != nil {
		t.Fatal(err)
	}
	if err := replay.Append(before, placement, after); err != nil {
		t.Fatal(err)
	}
	return replay
}
