package types

import "testing"

func TestPaletteColorValid(t *testing.T) {
	for _, choice := range PaletteChoices {
		if !choice.Color.Valid() || choice.Label == "" {
			t.Fatalf("palette choice %q must be valid and named", choice.Color)
		}
	}
	if !PaletteNone.Valid() {
		t.Fatal("no colour must stay a valid choice")
	}
	for _, unknown := range []PaletteColor{"Red", "#ff0000", "crimson", " blue"} {
		if unknown.Valid() {
			t.Fatalf("%q must not be a palette colour", unknown)
		}
	}
}
