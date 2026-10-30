package main

import "testing"

// v0.56: the geometry-memory clamp. A rectangle remembered on
// one machine must land fully inside the current desktop area
// on any other: size capped at the area, position pulled in
// from whichever edge it hangs over.
func TestClampWinGeom(t *testing.T) {
	area := [4]int32{100, 50, 2020, 1130} // a 1920x1080 desktop at an offset
	cases := []struct {
		name string
		in   winGeom
		want winGeom
	}{
		{"inside stays", winGeom{L: 400, T: 200, R: 1200, B: 800}, winGeom{L: 400, T: 200, R: 1200, B: 800}},
		{"hangs right", winGeom{L: 1800, T: 200, R: 2600, B: 800}, winGeom{L: 1220, T: 200, R: 2020, B: 800}},
		{"hangs bottom", winGeom{L: 400, T: 900, R: 1200, B: 1500}, winGeom{L: 400, T: 530, R: 1200, B: 1130}},
		{"hangs left+top", winGeom{L: -500, T: -100, R: 300, B: 500}, winGeom{L: 100, T: 50, R: 900, B: 650}},
		{"wider than area", winGeom{L: 0, T: 0, R: 3000, B: 400}, winGeom{L: 100, T: 50, R: 2020, B: 450}},
		{"taller than area", winGeom{L: 300, T: 0, R: 900, B: 2000}, winGeom{L: 300, T: 50, R: 900, B: 1130}},
		{"exact area", winGeom{L: 100, T: 50, R: 2020, B: 1130}, winGeom{L: 100, T: 50, R: 2020, B: 1130}},
	}
	for _, c := range cases {
		got := clampWinGeom(c.in, area[0], area[1], area[2], area[3])
		if got.L != c.want.L || got.T != c.want.T || got.R != c.want.R || got.B != c.want.B {
			t.Errorf("%s: clamp(%v) = %v, want %v", c.name, c.in, got, c.want)
		}
	}
	// Degenerate area (no shell rect): the memory passes through.
	g := winGeom{L: 1, T: 2, R: 300, B: 400}
	if got := clampWinGeom(g, 0, 0, 0, 0); got != g {
		t.Errorf("degenerate area should pass through, got %v", got)
	}
}

func TestWinGeomValid(t *testing.T) {
	if (winGeom{L: 0, T: 0, R: 79, B: 100}).valid() {
		t.Error("79px wide should be invalid")
	}
	if !(winGeom{L: 0, T: 0, R: 80, B: 60}).valid() {
		t.Error("80x60 should be valid")
	}
}
