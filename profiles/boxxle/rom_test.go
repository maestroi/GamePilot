package boxxle

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/maestroi/GamePilot/emulator/session"
)

func TestStartRoom1AndWalkRight(t *testing.T) {
	rom := os.Getenv("GAMEPILOT_BOXXLE_ROM")
	if rom == "" {
		rom = filepath.Join("..", "..", "roms", "boxxle.gb")
	}
	if _, err := os.Stat(rom); err != nil {
		t.Skip("set GAMEPILOT_BOXXLE_ROM or place roms/boxxle.gb to run the live Boxxle test")
	}

	sess, err := session.OpenROM(rom)
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()

	hash := sess.ROMHash()
	if err := (Profile{}).RequireROM(hash); err != nil {
		t.Skip(err.Error())
	}

	if err := StartRoom1(context.Background(), sess.Emulator()); err != nil {
		t.Fatal(err)
	}
	obs := Observe(sess.Emulator())
	if !obs.Ready {
		t.Fatalf("expected ready warehouse, got playing=%v ready=%v", obs.Playing, obs.Ready)
	}
	if obs.PlayerX != 1 || obs.PlayerY != 1 {
		t.Fatalf("start player = (%d,%d), want (1,1)", obs.PlayerX, obs.PlayerY)
	}
	if obs.Grid[0][0] != Wall || obs.Grid[2][2] != Box {
		t.Fatalf("expected wall at (0,0) and box at (2,2), grid:\n%s", RenderGrid(obs))
	}

	after, err := ExecuteMove(context.Background(), sess.Emulator(), Move{Direction: DirRight})
	if err != nil {
		t.Fatal(err)
	}
	if after.PlayerX != 2 || after.PlayerY != 1 {
		t.Fatalf("after right player = (%d,%d), want (2,1)", after.PlayerX, after.PlayerY)
	}
	if after.Moves != 1 {
		t.Fatalf("Moves = %d, want 1", after.Moves)
	}
}

func TestStartRoom1AndSolveWithSearch(t *testing.T) {
	rom := os.Getenv("GAMEPILOT_BOXXLE_ROM")
	if rom == "" {
		rom = filepath.Join("..", "..", "roms", "boxxle.gb")
	}
	if _, err := os.Stat(rom); err != nil {
		t.Skip("set GAMEPILOT_BOXXLE_ROM or place roms/boxxle.gb to run the live Boxxle test")
	}

	sess, err := session.OpenROM(rom)
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()

	if err := (Profile{}).RequireROM(sess.ROMHash()); err != nil {
		t.Skip(err.Error())
	}
	if err := StartRoom1(context.Background(), sess.Emulator()); err != nil {
		t.Fatal(err)
	}

	obs := Observe(sess.Emulator())
	for i := 0; i < 250 && !obs.Solved; i++ {
		decision, err := ChooseHeuristicMove(obs)
		if err != nil {
			t.Fatalf("move %d: %v\n%s", i+1, err, RenderGrid(obs))
		}
		before := obs
		obs, err = ExecuteMove(context.Background(), sess.Emulator(), decision.Move)
		if err != nil {
			t.Fatalf("execute %d %s: %v\n%s", i+1, decision.Move.Direction, err, RenderGrid(before))
		}
	}
	if !obs.Solved {
		t.Fatalf("expected room 1-1 solved, got:\n%s", RenderGrid(obs))
	}
}
