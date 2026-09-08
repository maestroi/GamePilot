package boxxle

import "testing"

func TestProfileSupportsOnlyKnownDump(t *testing.T) {
	profile := Profile{}
	if !profile.SupportsROM(USAEuropeSHA256) {
		t.Fatal("expected known Boxxle dump to be supported")
	}
	if !profile.SupportsROM("C859503342DB1F86DADEB7E6F3D8A8A2918E9B6A7C8756311B7CC7BB0A7E892F") {
		t.Fatal("expected hash matching to be case-insensitive")
	}
	if profile.SupportsROM("deadbeef") {
		t.Fatal("unexpected support for an unknown ROM")
	}
}
