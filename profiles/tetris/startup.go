package tetris

import (
	"context"
	"fmt"

	"github.com/maestroi/gomeboy/pkg/gomeboy"
)

const maxBootFrames = 2000

type startupEmulator interface {
	Peek8(addr uint16) byte
	Press(button gomeboy.Button)
	Release(button gomeboy.Button)
	StepFrame()
}

// StartTypeAZero deterministically navigates a freshly loaded Rev 1 ROM to a
// Type A level-0 game. It exists only to make the first vertical slice runnable;
// richer menu/profile configuration can be added when it is actually needed.
func StartTypeAZero(ctx context.Context, emu startupEmulator) error {
	// Fresh Gomeboy WRAM is zero before the ROM has initialized. Since normal
	// gameplay is also game state 0, require seeing one real non-gameplay state
	// before treating state 0 as a successfully started game.
	sawFrontendState := false
	frames := 0

	for frames < maxBootFrames {
		if err := ctx.Err(); err != nil {
			return err
		}

		state := emu.Peek8(gameStateAddr)
		if state != 0 {
			sawFrontendState = true
		}
		if state == 0 && sawFrontendState {
			return nil
		}

		switch state {
		case 0x35, // skippable copyright screen
			0x07, // title screen
			0x0E, // game type selection (defaults to Type A)
			0x11: // Type A level selection (defaults to level 0)
			if frames+2 > maxBootFrames {
				break
			}
			if err := pulse(ctx, emu, gomeboy.ButtonStart); err != nil {
				return err
			}
			frames += 2
		default:
			emu.StepFrame()
			frames++
		}
	}

	return fmt.Errorf("tetris: did not reach Type A gameplay within %d deterministic frames (state=0x%02x)", maxBootFrames, emu.Peek8(gameStateAddr))
}

func pulse(ctx context.Context, emu startupEmulator, button gomeboy.Button) error {
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
