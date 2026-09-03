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
	planner := flag.String("planner", "observe", "planner mode (first slice: observe)")
	flag.Parse()

	if *romPath == "" {
		return errors.New("-rom is required")
	}
	if *planner != "observe" {
		return fmt.Errorf("planner %q is not implemented in the first observation slice; use -planner observe", *planner)
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
	fmt.Printf("Planner: %s\n\n", *planner)

	if err := tetris.StartTypeAZero(context.Background(), sess.Emulator()); err != nil {
		return err
	}

	obs, err := tetris.Observe(sess.Emulator())
	if err != nil {
		return err
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(obs)
}
