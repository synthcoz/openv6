package icon

import (
	"bytes"
	"image/png"
	"testing"
)

func TestIcons(t *testing.T) {
	for _, s := range []State{StateOK, StateWarn, StateError, StatePaused, StateBrand} {
		img, err := png.Decode(bytes.NewReader(PNG(s, 64)))
		if err != nil || img.Bounds().Dx() != 64 {
			t.Fatalf("png %v", err)
		}
		// corner transparent, centre white, ring area coloured
		if _, _, _, a := img.At(0, 0).RGBA(); a != 0 {
			t.Fatal("corner not transparent")
		}
		if r, g, b, _ := img.At(32, 32).RGBA(); r>>8 < 250 || g>>8 < 250 || b>>8 < 250 {
			t.Fatal("centre not white")
		}
	}
	ico := ICO(StateOK)
	if ico[2] != 1 || ico[4] != 4 {
		t.Fatalf("ico header % x", ico[:6])
	}
}
