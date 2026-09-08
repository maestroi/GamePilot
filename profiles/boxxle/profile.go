package boxxle

import (
	"fmt"
	"strings"
)

const (
	// ProfileID is the stable identifier for the Boxxle GamePilot profile.
	ProfileID = "boxxle"

	// USAEuropeSHA256 identifies the 32 KiB ROM-only dump titled BOXXLE that
	// ships as roms/boxxle.gb in this workspace. Other revisions are refused
	// because the tile and OAM mapping is ROM-specific.
	USAEuropeSHA256 = "c859503342db1f86dadeb7e6f3d8a8a2918e9b6a7c8756311b7cc7bb0a7e892f"
)

// Profile contains the ROM-specific Boxxle interpretation. It is stateless.
type Profile struct{}

func (Profile) ID() string { return ProfileID }

func (Profile) SupportsROM(hash string) bool {
	return strings.EqualFold(strings.TrimSpace(hash), USAEuropeSHA256)
}

// RequireROM returns an explanatory error for unsupported revisions.
func (p Profile) RequireROM(hash string) error {
	if p.SupportsROM(hash) {
		return nil
	}
	return fmt.Errorf("boxxle: unsupported ROM SHA-256 %q; supported BOXXLE hash is %s", hash, USAEuropeSHA256)
}
