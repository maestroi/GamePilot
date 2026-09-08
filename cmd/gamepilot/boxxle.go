package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/maestroi/GamePilot/emulator/session"
	"github.com/maestroi/GamePilot/profiles/boxxle"
)

func runBoxxle(ctx context.Context, sess *session.Session, planner, direction string, pieces int) error {
	profile := boxxle.Profile{}
	hash := sess.ROMHash()
	if err := profile.RequireROM(hash); err != nil {
		return err
	}

	cart := sess.Cartridge()
	fmt.Printf("ROM: %s\n", cart.Title)
	fmt.Printf("ROM SHA-256: %s\n", hash)
	fmt.Printf("Profile: %s\n", profile.ID())
	fmt.Printf("Planner: %s\n", planner)

	if err := boxxle.StartRoom1(ctx, sess.Emulator()); err != nil {
		return err
	}

	obs := boxxle.Observe(sess.Emulator())
	var err error
	switch planner {
	case "observe":
	case "place":
		move := boxxle.Move{Direction: boxxle.Direction(direction)}
		fmt.Printf("Move: %s\n", move.Direction)
		obs, err = boxxle.ExecuteMove(ctx, sess.Emulator(), move)
	case "heuristic":
		for n := 1; n <= pieces && !obs.Solved; n++ {
			before := obs
			decision, planErr := boxxle.ChooseHeuristicMove(before)
			if planErr != nil {
				return fmt.Errorf("heuristic move %d: %w", n, planErr)
			}
			fmt.Printf(
				"Move %d: %s remaining=%d score=%.4f boxes=%d goals=%d player=(%d,%d)\n",
				n,
				decision.Move.Direction,
				decision.PathLength,
				decision.Score,
				decision.Boxes,
				decision.GoalsRemaining,
				before.PlayerX,
				before.PlayerY,
			)
			obs, err = boxxle.ExecuteMove(ctx, sess.Emulator(), decision.Move)
			if err != nil {
				return fmt.Errorf("heuristic move %d execute: %w", n, err)
			}
		}
	default:
		return fmt.Errorf("boxxle planner %q is not implemented; use observe, place, or heuristic", planner)
	}
	if err != nil {
		return err
	}

	fmt.Printf("\n%s\n\n", boxxle.RenderGrid(obs))
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(obs)
}
