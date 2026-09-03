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
	planner := flag.String("planner", "observe", "mode: observe, place, or heuristic")
	rotation := flag.Int("rotation", 0, "raw Tetris rotation 0..3 for -planner place")
	column := flag.Int("column", 0, "leftmost occupied board column for -planner place")
	pieces := flag.Int("pieces", 25, "number of placements to execute for -planner heuristic")
	flag.Parse()

	if *romPath == "" {
		return errors.New("-rom is required")
	}
	if *planner != "observe" && *planner != "place" && *planner != "heuristic" {
		return fmt.Errorf("planner %q is not implemented; use -planner observe, place, or heuristic", *planner)
	}
	if *planner == "heuristic" && *pieces < 1 {
		return fmt.Errorf("-pieces must be at least 1 for -planner heuristic")
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
	case "heuristic":
		obs, err = tetris.Observe(sess.Emulator())
		if err != nil {
			break
		}
		for move := 1; move <= *pieces && !obs.GameOver; move++ {
			decision, planErr := tetris.ChooseHeuristicPlacement(obs)
			if planErr != nil {
				return fmt.Errorf("heuristic move %d: %w", move, planErr)
			}
			fmt.Printf(
				"Move %d: piece=%s rotation=%d target_column=%d heuristic=%.6f lines=%d height=%d holes=%d bumpiness=%d\n",
				move,
				obs.CurrentPiece.Kind,
				decision.Placement.Rotation,
				decision.Placement.TargetColumn,
				decision.Score,
				decision.LinesCleared,
				decision.AggregateHeight,
				decision.Holes,
				decision.Bumpiness,
			)
			obs, err = tetris.ExecutePlacement(ctx, sess.Emulator(), decision.Placement)
			if err != nil {
				return fmt.Errorf("heuristic move %d execute: %w", move, err)
			}
		}
	}
	if err != nil {
		return err
	}
	fmt.Println()

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(obs)
}
