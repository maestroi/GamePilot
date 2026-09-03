// Package profiles defines the smallest generic boundary GamePilot needs to
// select game-specific knowledge for a loaded ROM.
package profiles

// Profile identifies a game-specific interpretation of emulator state.
// Concrete profiles own all ROM addresses and game semantics.
type Profile interface {
	ID() string
	SupportsROM(hash string) bool
}
