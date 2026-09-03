package tetris

import "testing"

func TestProfileSupportsOnlyRev1(t *testing.T) {
	profile := Profile{}
	if !profile.SupportsROM(Rev1SHA256) {
		t.Fatal("expected Rev 1 ROM to be supported")
	}
	if !profile.SupportsROM("0D6535AEF23969C7E5AF2B077ACADDB4A445B3D0DF7BF34C8ACEF07B51B015C3") {
		t.Fatal("expected hash matching to be case-insensitive")
	}
	if profile.SupportsROM("deadbeef") {
		t.Fatal("unexpected support for an unknown ROM")
	}
}
