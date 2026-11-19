package main

// layout_test.go -- v0.71 test6: the layout heal's rules. A
// healthy grid at any supported pitch must never count as
// overlapping (or the heal would re-fire forever); genuinely
// colliding legacy positions must heal exactly once, keeping
// the desktop's reading order and every non-position field.

import (
	"encoding/json"
	"testing"
)

func layoutGridBlob(pitch float64, cols, rows int) []byte {
	lay := layoutBlob{Pos: map[string]layoutPoint{}}
	i := 0
	for c := 0; c < cols; c++ {
		for r := 0; r < rows; r++ {
			lay.Pos[string(rune('a'+i))] = layoutPoint{X: 1802 - float64(c)*pitch, Y: 54 + float64(r)*pitch}
			i++
		}
	}
	b, _ := json.Marshal(lay)
	return b
}

func TestLayoutHealSkipsHealthyGrids(t *testing.T) {
	for _, pitch := range []float64{104, 94, 86} {
		b := layoutGridBlob(pitch, 3, 6)
		if _, n, ok := healLayoutBlob(b, pitch); ok || n != 0 {
			t.Errorf("pitch %v: healthy grid healed (n=%d, ok=%v)", pitch, n, ok)
		}
	}
}

func TestLayoutHealRepairsCollisionsOnce(t *testing.T) {
	lay := layoutBlob{
		Pos: map[string]layoutPoint{
			"app-a": {X: 1802, Y: 54},
			"app-b": {X: 1805, Y: 60}, // collides with app-a
			"app-c": {X: 1802, Y: 200},
			"app-d": {X: 900, Y: 400},
		},
		Folders:     []layoutFolder{{ID: "f1", Name: "组", Items: []string{"app-b"}}},
		Opens:       map[string]int64{"app-a": 3},
		Added:       map[string]int64{"app-d": 9},
		HiddenFiles: []string{"secret.txt"},
	}
	b, _ := json.Marshal(lay)
	healed, n, ok := healLayoutBlob(b, 104)
	if !ok || n != 1 {
		t.Fatalf("heal = (ok %v, n %d), want (true, 1)", ok, n)
	}
	var out layoutBlob
	if err := json.Unmarshal(healed, &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Pos) != 4 {
		t.Fatalf("positions = %d, want 4", len(out.Pos))
	}
	w, h := layoutOverlapThresholds(104)
	if got := countLayoutOverlaps(out.Pos, w, h); got != 0 {
		t.Errorf("healed blob still has %d overlaps", got)
	}
	// Reading order preserved: the healed blob reads in
	// exactly the order the original positions read in
	// (rightmost column first, top to bottom).
	want := layoutReadingOrder(lay.Pos)
	order := layoutReadingOrder(out.Pos)
	for i := range want {
		if order[i] != want[i] {
			t.Errorf("order[%d] = %s, want %s (full order %v)", i, order[i], want[i], order)
		}
	}
	// Non-position fields untouched.
	if len(out.Folders) != 1 || out.Folders[0].ID != "f1" || out.Opens["app-a"] != 3 ||
		out.Added["app-d"] != 9 || len(out.HiddenFiles) != 1 {
		t.Errorf("non-position fields changed: %+v", out)
	}
	// Healing is a fixed point: the healed blob never heals again.
	if _, n2, ok2 := healLayoutBlob(healed, 104); ok2 || n2 != 0 {
		t.Errorf("second heal = (ok %v, n %d), want no-op", ok2, n2)
	}
}

func TestLayoutHealIgnoresGarbage(t *testing.T) {
	if _, _, ok := healLayoutBlob([]byte("{nope"), 104); ok {
		t.Error("invalid JSON healed")
	}
	if _, _, ok := healLayoutBlob([]byte(`{"pos":{}}`), 104); ok {
		t.Error("empty positions healed")
	}
}
