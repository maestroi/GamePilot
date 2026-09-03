package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/maestroi/GamePilot/emulator/session"
	"github.com/maestroi/GamePilot/profiles/tetris"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "gamepilot:", err)
		os.Exit(1)
	}
}

func run() error {
	romPath := flag.String("rom", "", "path to the supported Tetris Rev 1 ROM")
	planner := flag.String("planner", "observe", "mode: observe or place")
	rotation := flag.Int("rotation", 0, "raw Tetris rotation 0..3 for -planner place")
	column := flag.Int("column", 0, "leftmost occupied board column for -planner place")
	flag.Parse()

	if *romPath == "" {
		return errors.New("-rom is required")
	}
	if *planner != "observe" && *planner != "place" {
		return fmt.Errorf("planner %q is not implemented; use -planner observe or -planner place", *planner)
	}

	sess, err := session.OpenROM(*romPath)
	if err != nil {
		return err
	}
	defer sess.Close()

	profile := tetris.Profile{}
	hash := sess.ROMHash()
	if err := profile.RequireROM(hash); err != nil {
		return err
	}

	cart := sess.Cartridge()
	fmt.Printf("ROM: %s\n", cart.Title)
	fmt.Printf("ROM SHA-256: %s\n", hash)
	fmt.Printf("Profile: %s\n", profile.ID())
	fmt.Printf("Planner: %s\n", *planner)

	ctx := context.Background()
	if err := tetris.StartTypeAZero(ctx, sess.Emulator()); err != nil {
		return err
	}

	var obs tetris.Observation
	switch *planner {
	case "observe":
		obs, err = tetris.Observe(sess.Emulator())
	case "place":
		placement := tetris.Placement{Rotation: *rotation, TargetColumn: *column}
		fmt.Printf("Placement: rotation=%d target_column=%d\n", placement.Rotation, placement.TargetColumn)
		obs, err = tetris.ExecutePlacement(ctx, sess.Emulator(), placement)
	}
	if err != nil {
		return err
	}
	fmt.Println()

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(obs)
}
