package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/maestroi/GamePilot/emulator/ramclassify"
	"github.com/maestroi/GamePilot/emulator/session"
	"github.com/maestroi/GamePilot/profiles/tetris"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "gamepilot-jev:", err)
		os.Exit(1)
	}
}

func run() error {
	romPath := flag.String("rom", "", "path to Tetris Rev 1 ROM")
	pieces := flag.Int("pieces", 25, "maximum number of pieces Jev should place")
	baseURL := flag.String("base-url", envOr("OPENJEV_BASE_URL", "http://localhost:8000/v1"), "Jev-compatible base URL ending in /v1")
	model := flag.String("model", envOr("OPENJEV_MODEL", "openjev-latest"), "Jev/OpenJev model name")
	apiKeyEnv := flag.String("api-key-env", "OPENJEV_API_KEY", "environment variable containing the bearer token; use an empty value for keyless local servers")
	timeout := flag.Duration("timeout", 60*time.Second, "timeout for each Jev decision")
	candidates := flag.Int("candidates", tetris.DefaultJevCandidateLimit, "maximum verified shadow placements exposed to Jev per piece")
	flag.Parse()

	if *romPath == "" {
		return errors.New("-rom is required")
	}
	if *pieces < 1 {
		return errors.New("-pieces must be at least 1")
	}
	if *baseURL == "" {
		return errors.New("-base-url is required (or set OPENJEV_BASE_URL)")
	}
	if *model == "" {
		return errors.New("-model is required (or set OPENJEV_MODEL)")
	}
	if *candidates < 1 {
		return errors.New("-candidates must be at least 1")
	}

	ctx := context.Background()
	sess, err := session.OpenROM(*romPath)
	if err != nil {
		return err
	}
	defer sess.Close()

	profile := tetris.Profile{}
	if err := profile.RequireROM(sess.ROMHash()); err != nil {
		return err
	}
	if err := tetris.StartTypeAZero(ctx, sess); err != nil {
		return err
	}
	obs, err := tetris.Observe(sess)
	if err != nil {
		return err
	}

	apiKey := ""
	if *apiKeyEnv != "" {
		apiKey = os.Getenv(*apiKeyEnv)
	}
	client := ramclassify.NewOpenJevClient(*baseURL, apiKey)
	client.HTTPClient.Timeout = *timeout
	cfg := tetris.JevPlacementConfig{
		Model:         *model,
		MaxCandidates: *candidates,
	}

	fmt.Printf("ROM SHA-256: %s\n", sess.ROMHash())
	fmt.Printf("Profile: %s\n", profile.ID())
	fmt.Printf("Player: Jev shadow placement\n")
	fmt.Printf("Jev: model=%s base_url=%s candidates=%d\n", *model, *baseURL, *candidates)

	for move := 1; move <= *pieces && !obs.GameOver; move++ {
		before := obs
		decision, err := tetris.ChooseJevPlacementWithShadow(ctx, sess, client, before, cfg)
		if err != nil {
			return fmt.Errorf("move %d decision: %w", move, err)
		}
		fmt.Printf(
			"Move %d: piece=%s rotation=%d target_column=%d choice=%s p=%.4f confidence=%.4f candidates=%d/%d\n",
			move,
			before.CurrentPiece.Kind,
			decision.Placement.Rotation,
			decision.Placement.TargetColumn,
			decision.Choice,
			decision.Probability,
			decision.Confidence,
			decision.Candidates,
			decision.TotalCandidates,
		)
		obs, err = tetris.ExecutePlacement(ctx, sess, decision.Placement)
		if err != nil {
			return fmt.Errorf("move %d execute: %w", move, err)
		}
	}

	fmt.Println()
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(obs)
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
