//go:build race

package graph

// The race detector slows code 5-10x, which makes wall-clock guards meaningless.
const raceEnabled = true
