package sessions

import (
	"fmt"

	"github.com/maestroi/GamePilot/profiles/boxxle"
	"github.com/maestroi/GamePilot/profiles/tetris"
)

// NewLiveRunnerFactory dispatches Tetris and Boxxle from the launch profile.
func NewLiveRunnerFactory(extra map[string]TetrisPlannerFactory) RunnerFactory {
	tetrisFactory := NewTetrisRunnerFactory(extra)
	boxxleFactory := NewBoxxleRunnerFactory()
	return RunnerFactoryFunc(func(config LaunchConfig) (Runner, error) {
		switch config.Profile {
		case tetris.ProfileID:
			return tetrisFactory.New(config)
		case boxxle.ProfileID:
			return boxxleFactory.New(config)
		default:
			return nil, fmt.Errorf("sessions: unsupported profile %q", config.Profile)
		}
	})
}

// NewLiveManager hosts whichever built-in profile the operator launches.
func NewLiveManager(extra map[string]TetrisPlannerFactory) *Manager {
	return NewManager(NewLiveRunnerFactory(extra))
}
