package main

// layout.go -- v0.71 test6: desktop layout overlap healing.
//
// The stored layout blob (Data/layout.json, served by
// GET /api/layout) can carry positions the desktop no longer
// paints from: folder members, removed apps and legacy free
// positions from before the grid era. Some of those sit on top
// of each other forever -- the frontend repaints and
// re-persists only the top-level icons it manages, so the
// stale collisions never heal on disk, and the load-time
// overlap warning repeated on every desktop load (43 on one
// field machine, a drifting 38 -> 40 -> 41 on another). The
// handler now heals once: re-pack every stored position onto
// canonical grid cells in the frontend's own reading order and
// write the result back through the datastore, so the warning
// fires exactly once and the disk agrees with the screen. The
// frontend only consumes stored positions to derive icon order
// and insertion hints -- it repaints real cells and persists
// them on first render -- so a canonical re-pack is invisible
// except that the collisions are gone.

import (
	"encoding/json"
	"sort"
)

// layoutPitch is the desktop's icon grid pitch: the frontend's
// deskPitch() is deskIconSize()+50 with the size from settings
// (default 54 -> 104).
func layoutPitch() float64 {
	size := settingsGet().DeskIconSize
	if size <= 0 {
		size = 54
	}
	return float64(size + 50)
}

// layoutOverlapThresholds returns the pair-distance below
// which two stored positions count as overlapping. At the
// default pitch these are the historical tile extents (92x100
// inside a 104 pitch); on a tighter grid they shrink to the
// pitch itself, where two distinct cells are never closer than
// one pitch on both axes at once -- so a healthy small-icon
// grid never trips the detector.
func layoutOverlapThresholds(pitch float64) (w, h float64) {
	if pitch >= 104 {
		return 92, 100
	}
	return pitch, pitch
}

// countLayoutOverlaps counts stored-position pairs closer than
// the thresholds on both axes (the load-time detector's rule).
func countLayoutOverlaps(pos map[string]layoutPoint, w, h float64) int {
	keys := make([]string, 0, len(pos))
	for k := range pos {
		keys = append(keys, k)
	}
	n := 0
	for i := 0; i < len(keys); i++ {
		for j := i + 1; j < len(keys); j++ {
			dx := pos[keys[i]].X - pos[keys[j]].X
			if dx < 0 {
				dx = -dx
			}
			dy := pos[keys[i]].Y - pos[keys[j]].Y
			if dy < 0 {
				dy = -dy
			}
			if dx < w && dy < h {
				n++
			}
		}
	}
	return n
}

// layoutReadingOrder sorts position keys the way the desktop
// reads them: rightmost column first, top to bottom inside a
// column (the frontend's reconcileDeskOrder rule).
func layoutReadingOrder(pos map[string]layoutPoint) []string {
	keys := make([]string, 0, len(pos))
	for k := range pos {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		a, b := pos[keys[i]], pos[keys[j]]
		if a.X != b.X {
			return a.X > b.X
		}
		if a.Y != b.Y {
			return a.Y < b.Y
		}
		return keys[i] < keys[j]
	})
	return keys
}

// layoutCanonicalCell is the frontend's cellPosFor shape on a
// virtual 1920x1080 desktop at the default pitch: column-major
// from the right edge, 10 rows per column.
func layoutCanonicalCell(i int) layoutPoint {
	const pitch, rows = 104, 10
	col, row := i/rows, i%rows
	x := 1920 - 118 - col*pitch
	if x < 4 {
		x = 4
	}
	return layoutPoint{X: float64(x), Y: float64(54 + row*pitch)}
}

// healLayoutBlob re-packs a layout blob's positions onto
// canonical cells when (and only when) the stored positions
// overlap at the given pitch. It returns the healed blob, the
// overlap count that was found, and whether healing happened.
// Every non-position field (folders, opens, added, hidden
// files) is carried over untouched.
func healLayoutBlob(b []byte, pitch float64) ([]byte, int, bool) {
	var lay layoutBlob
	if err := json.Unmarshal(b, &lay); err != nil || len(lay.Pos) == 0 {
		return nil, 0, false
	}
	w, h := layoutOverlapThresholds(pitch)
	n := countLayoutOverlaps(lay.Pos, w, h)
	if n == 0 {
		return nil, 0, false
	}
	for i, k := range layoutReadingOrder(lay.Pos) {
		lay.Pos[k] = layoutCanonicalCell(i)
	}
	out, err := json.Marshal(lay)
	if err != nil {
		return nil, n, false
	}
	return out, n, true
}
