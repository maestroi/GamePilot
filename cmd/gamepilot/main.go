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
	planner := flag.String("planner", "observe", "mode: observe, place, heuristic, or replay")
	rotation := flag.Int("rotation", 0, "raw Tetris rotation 0..3 for -planner place")
	column := flag.Int("column", 0, "leftmost occupied board column for -planner place")
	pieces := flag.Int("pieces", 25, "number of placements to execute for -planner heuristic")
	replayOut := flag.String("replay-out", "", "write a deterministic replay JSON file for place or heuristic mode")
	replayIn := flag.String("replay-in", "", "replay JSON file to verify with -planner replay")
	flag.Parse()

	if *romPath == "" {
		return errors.New("-rom is required")
	}
	if *planner != "observe" && *planner != "place" && *planner != "heuristic" && *planner != "replay" {
		return fmt.Errorf("planner %q is not implemented; use -planner observe, place, heuristic, or replay", *planner)
	}
	if *planner == "heuristic" && *pieces < 1 {
		return fmt.Errorf("-pieces must be at least 1 for -planner heuristic")
	}
	if *planner == "replay" && *replayIn == "" {
		return fmt.Errorf("-replay-in is required for -planner replay")
	}
	if *planner != "replay" && *replayIn != "" {
		return fmt.Errorf("-replay-in is only valid with -planner replay")
	}
	if *replayOut != "" && *planner != "place" && *planner != "heuristic" {
		return fmt.Errorf("-replay-out is only valid with -planner place or -planner heuristic")
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

	initial, err := tetris.Observe(sess.Emulator())
	if err != nil {
		return err
	}

	var record *tetris.Replay
	if *replayOut != "" {
		replay := tetris.NewReplay(hash, initial)
		record = &replay
	}

	var obs tetris.Observation
	switch *planner {
	case "observe":
		obs = initial
	case "place":
		placement := tetris.Placement{Rotation: *rotation, TargetColumn: *column}
		fmt.Printf("Placement: rotation=%d target_column=%d\n", placement.Rotation, placement.TargetColumn)
		obs, err = tetris.ExecutePlacement(ctx, sess.Emulator(), placement)
		if err == nil && record != nil {
			err = record.Append(initial, placement, obs)
		}
	case "heuristic":
		obs = initial
		for move := 1; move <= *pieces && !obs.GameOver; move++ {
			before := obs
			decision, planErr := tetris.ChooseHeuristicPlacement(before)
			if planErr != nil {
				return fmt.Errorf("heuristic move %d: %w", move, planErr)
			}
			fmt.Printf(
				"Move %d: piece=%s rotation=%d target_column=%d heuristic=%.6f lines=%d height=%d holes=%d bumpiness=%d\n",
				move,
				before.CurrentPiece.Kind,
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
			if record != nil {
				if err := record.Append(before, decision.Placement, obs); err != nil {
					return fmt.Errorf("heuristic move %d record: %w", move, err)
				}
			}
		}
	case "replay":
		replay, readErr := readReplayFile(*replayIn)
		if readErr != nil {
			return readErr
		}
		fmt.Printf("Replay: %s (%d moves)\n", *replayIn, len(replay.Moves))
		obs, err = tetris.VerifyReplay(ctx, sess.Emulator(), hash, replay)
		if err == nil {
			fmt.Printf("Replay verified: %d moves\n", len(replay.Moves))
		}
	}
	if err != nil {
		return err
	}

	if record != nil {
		if err := writeReplayFile(*replayOut, *record); err != nil {
			return err
		}
		fmt.Printf("Replay written: %s (%d moves)\n", *replayOut, len(record.Moves))
	}

	fmt.Println()
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(obs)
}

func readReplayFile(path string) (tetris.Replay, error) {
	file, err := os.Open(path)
	if err != nil {
		return tetris.Replay{}, fmt.Errorf("open replay %q: %w", path, err)
	}
	defer file.Close()

	replay, err := tetris.ReadReplay(file)
	if err != nil {
		return tetris.Replay{}, fmt.Errorf("read replay %q: %w", path, err)
	}
	return replay, nil
}

func writeReplayFile(path string, replay tetris.Replay) error {
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create replay %q: %w", path, err)
	}
	if err := tetris.WriteReplay(file, replay); err != nil {
		file.Close()
		return fmt.Errorf("write replay %q: %w", path, err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close replay %q: %w", path, err)
	}
	return nil
}
