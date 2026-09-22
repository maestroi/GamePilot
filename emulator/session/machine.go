package session

import "github.com/maestroi/gomeboy/pkg/gomeboy"

// The methods in this file intentionally delegate to the underlying emulator.
// They let checkpoint-based planners treat Session as one atomic machine while
// keeping all emulation behavior inside Gomeboy.

func (s *Session) Peek8(addr uint16) byte {
	return s.emu.Peek8(addr)
}

func (s *Session) PeekInto(addr uint16, dst []byte) {
	s.emu.PeekInto(addr, dst)
}

func (s *Session) FrameCount() uint64 {
	return s.emu.FrameCount()
}

func (s *Session) Press(button gomeboy.Button) {
	s.emu.Press(button)
}

func (s *Session) Release(button gomeboy.Button) {
	s.emu.Release(button)
}

func (s *Session) StepFrame() {
	s.emu.StepFrame()
}
