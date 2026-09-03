// Package session owns the lifetime of a Gomeboy emulator used by GamePilot.
// It intentionally stays thin: emulation, input, stepping, memory inspection,
// and save-state serialization remain Gomeboy responsibilities.
package session

import (
	"encoding/hex"
	"fmt"

	"github.com/maestroi/gomeboy/pkg/gomeboy"
)

// Session is a thin lifetime/checkpoint wrapper around a Gomeboy emulator.
type Session struct {
	emu *gomeboy.Emulator
}

// OpenROM creates a deterministic headless emulator for a ROM. Video output is
// disabled because the initial GamePilot Tetris profile is memory-driven.
func OpenROM(path string) (*Session, error) {
	emu, err := gomeboy.New(
		gomeboy.WithROM(path),
		gomeboy.Headless(),
		gomeboy.WithoutVideo(),
	)
	if err != nil {
		return nil, fmt.Errorf("session: open ROM: %w", err)
	}
	return &Session{emu: emu}, nil
}

// Emulator returns the underlying public Gomeboy API. GamePilot profiles and
// controllers should use that API directly rather than reimplementing it here.
func (s *Session) Emulator() *gomeboy.Emulator {
	return s.emu
}

// ROMHash returns the loaded ROM SHA-256 as lowercase hexadecimal.
func (s *Session) ROMHash() string {
	hash := s.emu.ROMSHA256()
	return hex.EncodeToString(hash[:])
}

// Cartridge returns Gomeboy's cartridge metadata.
func (s *Session) Cartridge() gomeboy.CartInfo {
	return s.emu.Cartridge()
}

// SaveCheckpoint returns a complete emulator checkpoint.
func (s *Session) SaveCheckpoint() ([]byte, error) {
	state, err := s.emu.SaveState()
	if err != nil {
		return nil, fmt.Errorf("session: save checkpoint: %w", err)
	}
	return state, nil
}

// LoadCheckpoint restores a checkpoint previously returned by SaveCheckpoint.
func (s *Session) LoadCheckpoint(state []byte) error {
	if err := s.emu.LoadState(state); err != nil {
		return fmt.Errorf("session: load checkpoint: %w", err)
	}
	return nil
}

// Close flushes any persistence configured on the Gomeboy emulator.
func (s *Session) Close() error {
	return s.emu.Close()
}
