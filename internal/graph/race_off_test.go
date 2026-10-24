//go:build !race

package graph

// raceEnabled reports whether the test binary was built with -race.
const raceEnabled = false
