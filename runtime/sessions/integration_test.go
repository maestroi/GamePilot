package sessions

import (
	"bytes"
	"context"
	"encoding/json"
	"image/png"
	"os"
	"testing"
	"time"

	"github.com/maestroi/GamePilot/profiles/tetris"
)

// TestTetrisManagerRealROM is opt-in because the repository never ships or
// downloads copyrighted ROM data. It exercises the actual managed-session path
// end to end with a locally supplied exact Rev 1 ROM.
//
// Run with:
//
//   GAMEPILOT_TETRIS_ROM=./roms/tetris.gb go test ./runtime/sessions -run TestTetrisManagerRealROM -v
func TestTetrisManagerRealROM(t *testing.T) {
	rom := os.Getenv("GAMEPILOT_TETRIS_ROM")
	if rom == "" {
		t.Skip("set GAMEPILOT_TETRIS_ROM to an exact Tetris Rev 1 ROM to run the live-session integration test")
	}

	manager := NewTetrisManager(nil)
	id, err := manager.Start(LaunchConfig{
		ROMPath:      rom,
		Profile:      tetris.ProfileID,
		Planner:      "lookahead",
		MoveLimit:    3,
		RecordReplay: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	snap, err := manager.Wait(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if snap.Status != StatusDone {
		t.Fatalf("status = %s, want done (error=%q reason=%q)", snap.Status, snap.Error, snap.Reason)
	}
	if snap.Reason != "move_limit" {
		t.Fatalf("reason = %q, want move_limit", snap.Reason)
	}
	if snap.Moves != 3 {
		t.Fatalf("moves = %d, want 3", snap.Moves)
	}
	if snap.ROMSHA256 != tetris.Rev1SHA256 {
		t.Fatalf("ROM SHA-256 = %s, want %s", snap.ROMSHA256, tetris.Rev1SHA256)
	}
	if snap.Frame == 0 || len(snap.Observation) == 0 || len(snap.Decision) == 0 {
		t.Fatalf("terminal snapshot missing live data: frame=%d observation=%d decision=%d", snap.Frame, len(snap.Observation), len(snap.Decision))
	}
	if !snap.FrameAvailable || snap.FrameSequence == 0 {
		t.Fatalf("terminal snapshot missing live frame metadata: available=%v sequence=%d", snap.FrameAvailable, snap.FrameSequence)
	}

	frame, err := manager.Frame(id)
	if err != nil {
		t.Fatal(err)
	}
	if frame.ContentType != "image/png" || frame.EmulatorFrame != snap.Frame {
		t.Fatalf("frame metadata = type=%q emulator_frame=%d, want image/png frame=%d", frame.ContentType, frame.EmulatorFrame, snap.Frame)
	}
	img, err := png.Decode(bytes.NewReader(frame.Data))
	if err != nil {
		t.Fatalf("decode live PNG: %v", err)
	}
	if bounds := img.Bounds(); bounds.Dx() != 160 || bounds.Dy() != 144 {
		t.Fatalf("live PNG bounds = %v, want 160x144", bounds)
	}

	replayBytes, err := manager.Replay(id)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := tetris.ReadReplay(bytes.NewReader(replayBytes))
	if err != nil {
		t.Fatal(err)
	}
	if len(replay.Moves) != 3 {
		t.Fatalf("replay moves = %d, want 3", len(replay.Moves))
	}
	if replay.Moves[2].After.StateHash != tetris.ObservationStateHash(snapObservation(t, snap)) {
		t.Fatal("terminal snapshot does not match finalized replay tail")
	}
}

func snapObservation(t *testing.T, snap Snapshot) tetris.Observation {
	t.Helper()
	var obs tetris.Observation
	if err := json.Unmarshal(snap.Observation, &obs); err != nil {
		t.Fatalf("decode snapshot observation: %v", err)
	}
	return obs
}
