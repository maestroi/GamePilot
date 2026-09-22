package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/maestroi/GamePilot/emulator/ramdiscover"
	"github.com/maestroi/GamePilot/emulator/session"
	"github.com/maestroi/GamePilot/profiles/boxxle"
	"github.com/maestroi/GamePilot/profiles/tetris"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "gamepilot-discover:", err)
		os.Exit(1)
	}
}

func run() error {
	romPath := flag.String("rom", "", "path to a Game Boy ROM")
	startup := flag.String("startup", "auto", "startup adapter: auto, tetris, boxxle, or none")
	warmup := flag.Int("warmup", 0, "idle frames to run after startup before probing")
	inputs := flag.String("inputs", "left,right,up,down,a,b", "comma-separated controls to probe")
	holdFrames := flag.Int("hold-frames", 1, "frames to hold each input")
	releaseFrames := flag.Int("release-frames", 2, "frames to capture after releasing each input")
	trials := flag.Int("trials", 3, "number of checkpointed trials per input")
	trialAdvance := flag.Int("trial-advance", 2, "idle frames between trial checkpoints")
	top := flag.Int("top", 32, "maximum candidates retained per input")
	outPath := flag.String("out", "", "optional path to also write the JSON report")
	flag.Parse()

	if *romPath == "" {
		return errors.New("-rom is required")
	}
	if *warmup < 0 {
		return errors.New("-warmup cannot be negative")
	}
	if *holdFrames < 0 || *releaseFrames < 0 || *holdFrames+*releaseFrames < 1 {
		return errors.New("-hold-frames and -release-frames must capture at least one non-negative frame")
	}
	if *trials < 1 {
		return errors.New("-trials must be at least 1")
	}
	if *trialAdvance < 0 {
		return errors.New("-trial-advance cannot be negative")
	}
	if *top < 1 {
		return errors.New("-top must be at least 1")
	}

	probeInputs, err := parseInputs(*inputs)
	if err != nil {
		return err
	}

	ctx := context.Background()
	sess, err := session.OpenROM(*romPath)
	if err != nil {
		return err
	}
	defer sess.Close()

	hash := sess.ROMHash()
	profile, err := runStartup(ctx, sess, hash, *startup)
	if err != nil {
		return err
	}
	for i := 0; i < *warmup; i++ {
		sess.Emulator().StepFrame()
	}

	cfg := ramdiscover.DefaultConfig()
	cfg.Actions = make([]ramdiscover.Action, 0, len(probeInputs))
	for _, input := range probeInputs {
		cfg.Actions = append(cfg.Actions, ramdiscover.Action{
			Name:          string(input),
			Input:         input,
			HoldFrames:    *holdFrames,
			ReleaseFrames: *releaseFrames,
		})
	}
	cfg.Trials = *trials
	cfg.TrialAdvanceFrames = *trialAdvance
	cfg.TopCandidates = *top

	report, err := ramdiscover.Run(ctx, ramdiscover.NewGomeboyMachine(sess), cfg)
	if err != nil {
		return err
	}
	report.ROMHash = hash
	report.Profile = profile

	payload, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("encode report: %w", err)
	}
	payload = append(payload, '\n')
	if _, err := os.Stdout.Write(payload); err != nil {
		return fmt.Errorf("write stdout: %w", err)
	}
	if *outPath != "" {
		if err := os.WriteFile(*outPath, payload, 0o644); err != nil {
			return fmt.Errorf("write report %q: %w", *outPath, err)
		}
	}
	return nil
}

func runStartup(ctx context.Context, sess *session.Session, hash, mode string) (string, error) {
	tetrisProfile := tetris.Profile{}
	boxxleProfile := boxxle.Profile{}

	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "auto":
		switch {
		case tetrisProfile.SupportsROM(hash):
			if err := tetris.StartTypeAZero(ctx, sess.Emulator()); err != nil {
				return "", err
			}
			return tetrisProfile.ID(), nil
		case boxxleProfile.SupportsROM(hash):
			if err := boxxle.StartRoom1(ctx, sess.Emulator()); err != nil {
				return "", err
			}
			return boxxleProfile.ID(), nil
		default:
			return "", fmt.Errorf("no startup adapter for ROM %s; use -startup none (optionally with -warmup) or add a ROM-specific startup adapter", hash)
		}
	case "tetris":
		if err := tetrisProfile.RequireROM(hash); err != nil {
			return "", err
		}
		if err := tetris.StartTypeAZero(ctx, sess.Emulator()); err != nil {
			return "", err
		}
		return tetrisProfile.ID(), nil
	case "boxxle":
		if err := boxxleProfile.RequireROM(hash); err != nil {
			return "", err
		}
		if err := boxxle.StartRoom1(ctx, sess.Emulator()); err != nil {
			return "", err
		}
		return boxxleProfile.ID(), nil
	case "none":
		return "unprofiled", nil
	default:
		return "", fmt.Errorf("unsupported -startup %q; use auto, tetris, boxxle, or none", mode)
	}
}

func parseInputs(raw string) ([]ramdiscover.Input, error) {
	parts := strings.Split(raw, ",")
	inputs := make([]ramdiscover.Input, 0, len(parts))
	seen := map[ramdiscover.Input]bool{}
	for _, part := range parts {
		input := ramdiscover.Input(strings.ToLower(strings.TrimSpace(part)))
		switch input {
		case ramdiscover.InputLeft, ramdiscover.InputRight, ramdiscover.InputUp, ramdiscover.InputDown, ramdiscover.InputA, ramdiscover.InputB:
		default:
			return nil, fmt.Errorf("unsupported input %q; use left,right,up,down,a,b", part)
		}
		if seen[input] {
			continue
		}
		seen[input] = true
		inputs = append(inputs, input)
	}
	if len(inputs) == 0 {
		return nil, errors.New("-inputs must contain at least one input")
	}
	return inputs, nil
}
