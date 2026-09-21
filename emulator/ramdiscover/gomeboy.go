package ramdiscover

import (
	"fmt"

	"github.com/maestroi/GamePilot/emulator/session"
	"github.com/maestroi/gomeboy/pkg/gomeboy"
)

// GomeboyMachine adapts a GamePilot session to the generic discovery Machine.
// Keeping this adapter here means the analyzer itself has no ROM-specific code.
type GomeboyMachine struct {
	session *session.Session
}

func NewGomeboyMachine(sess *session.Session) *GomeboyMachine {
	return &GomeboyMachine{session: sess}
}

func (m *GomeboyMachine) SaveCheckpoint() ([]byte, error) {
	return m.session.SaveCheckpoint()
}

func (m *GomeboyMachine) LoadCheckpoint(state []byte) error {
	return m.session.LoadCheckpoint(state)
}

func (m *GomeboyMachine) PeekInto(addr uint16, dst []byte) {
	m.session.Emulator().PeekInto(addr, dst)
}

func (m *GomeboyMachine) FrameCount() uint64 {
	return m.session.Emulator().FrameCount()
}

func (m *GomeboyMachine) Press(input Input) error {
	button, err := gomeboyButton(input)
	if err != nil {
		return err
	}
	m.session.Emulator().Press(button)
	return nil
}

func (m *GomeboyMachine) Release(input Input) error {
	button, err := gomeboyButton(input)
	if err != nil {
		return err
	}
	m.session.Emulator().Release(button)
	return nil
}

func (m *GomeboyMachine) StepFrame() {
	m.session.Emulator().StepFrame()
}

func gomeboyButton(input Input) (gomeboy.Button, error) {
	switch input {
	case InputLeft:
		return gomeboy.ButtonLeft, nil
	case InputRight:
		return gomeboy.ButtonRight, nil
	case InputUp:
		return gomeboy.ButtonUp, nil
	case InputDown:
		return gomeboy.ButtonDown, nil
	case InputA:
		return gomeboy.ButtonA, nil
	case InputB:
		return gomeboy.ButtonB, nil
	default:
		return 0, fmt.Errorf("ramdiscover: unsupported input %q", input)
	}
}
