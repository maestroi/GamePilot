package tetris

import (
	"context"
	"fmt"

	"github.com/maestroi/gomeboy/pkg/gomeboy"
)

const (
	// For the Rev 1 renderer, anchor X 47 places matrix column 0 at playfield
	// column 0. Each horizontal input changes anchor X by exactly 8 pixels.
	anchorXMatrixColumnZeroAtBoardZero = 47
	cellPixels                          = 8
	maxPlacementFrames                  = 2000
)

// Placement is the first game-level action contract. Rotation is the ROM's raw
// orientation index (0..3). TargetColumn is the leftmost occupied playfield
// column after that rotation, not the sprite anchor or matrix origin.
type Placement struct {
	Rotation     int `json:"rotation"`
	TargetColumn int `json:"target_column"`
}

type controllerEmulator interface {
	memoryReader
	Press(gomeboy.Button)
	Release(gomeboy.Button)
	StepFrame()
}

// ExecutePlacement deterministically rotates, shifts, and soft-drops the
// current piece, then returns after the lock pipeline has completed and the
// next piece is ready. Every rotate/shift pulse is verified from ROM memory.
func ExecutePlacement(ctx context.Context, emu controllerEmulator, placement Placement) (Observation, error) {
	obs, err := Observe(emu)
	if err != nil {
		return Observation{}, err
	}
	if obs.GameOver {
		return obs, nil
	}
	if !obs.Ready {
		return Observation{}, fmt.Errorf("tetris: cannot place at frame %d: game is not ready for input", obs.Frame)
	}
	if obs.CurrentPiece.Kind == PieceUnknown {
		return Observation{}, fmt.Errorf("tetris: cannot place unknown current piece")
	}

	minX, maxX, err := horizontalBounds(obs.CurrentPiece.Kind, placement.Rotation)
	if err != nil {
		return Observation{}, err
	}
	width := maxX - minX + 1
	if placement.TargetColumn < 0 || placement.TargetColumn+width > BoardColumns {
		return Observation{}, fmt.Errorf(
			"tetris: target column %d is illegal for %s rotation %d (occupied width %d; valid leftmost columns 0..%d)",
			placement.TargetColumn, obs.CurrentPiece.Kind, placement.Rotation, width, BoardColumns-width,
		)
	}

	originalKind := obs.CurrentPiece.Kind
	obs, err = rotateTo(ctx, emu, obs, placement.Rotation)
	if err != nil {
		return Observation{}, err
	}
	if obs.CurrentPiece.Kind != originalKind {
		return Observation{}, fmt.Errorf("tetris: current piece changed while rotating")
	}

	matrixBaseColumn := placement.TargetColumn - minX
	desiredAnchorX := anchorXMatrixColumnZeroAtBoardZero + matrixBaseColumn*cellPixels
	obs, err = shiftToAnchorX(ctx, emu, obs, desiredAnchorX)
	if err != nil {
		return Observation{}, err
	}
	if obs.CurrentPiece.Kind != originalKind {
		return Observation{}, fmt.Errorf("tetris: current piece changed while shifting")
	}

	return softDropUntilNextPiece(ctx, emu)
}

func rotateTo(ctx context.Context, emu controllerEmulator, obs Observation, target int) (Observation, error) {
	if target < 0 || target > 3 {
		return Observation{}, fmt.Errorf("tetris: rotation %d is outside raw ROM range 0..3", target)
	}
	current := obs.CurrentPiece.Rotation
	incrementSteps := (target - current + 4) % 4 // B: ROM orientation +1
	decrementSteps := (current - target + 4) % 4 // A: ROM orientation -1

	button := gomeboy.ButtonB
	step := 1
	steps := incrementSteps
	if decrementSteps < incrementSteps {
		button = gomeboy.ButtonA
		step = -1
		steps = decrementSteps
	}

	for i := 0; i < steps; i++ {
		before := obs.CurrentPiece.Rotation
		if err := pulseButton(ctx, emu, button); err != nil {
			return Observation{}, err
		}
		var err error
		obs, err = Observe(emu)
		if err != nil {
			return Observation{}, err
		}
		expected := (before + step + 4) % 4
		if !obs.Ready {
			return Observation{}, fmt.Errorf("tetris: piece left ready state while rotating at frame %d", obs.Frame)
		}
		if obs.CurrentPiece.Rotation != expected {
			return Observation{}, fmt.Errorf(
				"tetris: rotation blocked at frame %d: expected raw orientation %d, still %d",
				obs.Frame, expected, obs.CurrentPiece.Rotation,
			)
		}
	}
	return obs, nil
}

func shiftToAnchorX(ctx context.Context, emu controllerEmulator, obs Observation, desiredAnchorX int) (Observation, error) {
	current := int(obs.CurrentPiece.AnchorX)
	delta := desiredAnchorX - current
	if delta%cellPixels != 0 {
		return Observation{}, fmt.Errorf("tetris: anchor X %d cannot reach target anchor X %d in 8-pixel moves", current, desiredAnchorX)
	}

	button := gomeboy.ButtonRight
	step := cellPixels
	moves := delta / cellPixels
	if moves < 0 {
		button = gomeboy.ButtonLeft
		step = -cellPixels
		moves = -moves
	}

	for i := 0; i < moves; i++ {
		before := int(obs.CurrentPiece.AnchorX)
		if err := pulseButton(ctx, emu, button); err != nil {
			return Observation{}, err
		}
		var err error
		obs, err = Observe(emu)
		if err != nil {
			return Observation{}, err
		}
		if !obs.Ready {
			return Observation{}, fmt.Errorf("tetris: piece left ready state while shifting at frame %d", obs.Frame)
		}
		expected := before + step
		if int(obs.CurrentPiece.AnchorX) != expected {
			return Observation{}, fmt.Errorf(
				"tetris: horizontal move blocked at frame %d: expected anchor X %d, got %d",
				obs.Frame, expected, obs.CurrentPiece.AnchorX,
			)
		}
	}
	return obs, nil
}

func softDropUntilNextPiece(ctx context.Context, emu controllerEmulator) (Observation, error) {
	emu.Press(gomeboy.ButtonDown)
	downHeld := true
	defer func() {
		if downHeld {
			emu.Release(gomeboy.ButtonDown)
		}
	}()

	sawLockTransition := false
	for i := 0; i < maxPlacementFrames; i++ {
		if err := ctx.Err(); err != nil {
			return Observation{}, err
		}
		emu.StepFrame()
		obs, err := Observe(emu)
		if err != nil {
			return Observation{}, err
		}
		if obs.GameOver {
			if downHeld {
				emu.Release(gomeboy.ButtonDown)
				downHeld = false
			}
			return obs, nil
		}

		if !obs.Ready || !obs.CurrentPiece.Visible || emu.Peek8(lockPhaseAddr) != 0 {
			sawLockTransition = true
			if downHeld {
				emu.Release(gomeboy.ButtonDown)
				downHeld = false
			}
		}
		if sawLockTransition && obs.Ready {
			return obs, nil
		}
	}
	return Observation{}, fmt.Errorf("tetris: placement did not reach the next ready piece within %d frames", maxPlacementFrames)
}

func pulseButton(ctx context.Context, emu controllerEmulator, button gomeboy.Button) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	emu.Press(button)
	emu.StepFrame()
	emu.Release(button)
	if err := ctx.Err(); err != nil {
		return err
	}
	emu.StepFrame()
	return ctx.Err()
}
