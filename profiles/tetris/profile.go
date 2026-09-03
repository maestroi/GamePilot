package tetris

import (
	"fmt"
	"strings"
)

const (
	// ProfileID is the stable identifier for the first GamePilot game profile.
	ProfileID = "tetris"

	// Rev1SHA256 identifies Tetris (World) (Rev 1), also documented by the
	// disassembly as Tetris (JUE) (V1.1) [!]. No other ROM revision is accepted
	// by this profile because the state mapping is ROM-specific.
	Rev1SHA256 = "0d6535aef23969c7e5af2b077acaddb4a445b3d0df7bf34c8acef07b51b015c3"
)

// Profile contains the ROM-specific Tetris interpretation. It is stateless;
// all addresses and semantics live in this package.
type Profile struct{}

func (Profile) ID() string { return ProfileID }

func (Profile) SupportsROM(hash string) bool {
	return strings.EqualFold(strings.TrimSpace(hash), Rev1SHA256)
}

// RequireROM returns an explanatory error for unsupported revisions.
func (p Profile) RequireROM(hash string) error {
	if p.SupportsROM(hash) {
		return nil
	}
	return fmt.Errorf("tetris: unsupported ROM SHA-256 %q; supported Rev 1 hash is %s", hash, Rev1SHA256)
}
