//go:build race

package redact

// raceEnabled skips the absolute time cap: the race detector is 10x slower.
const raceEnabled = true
