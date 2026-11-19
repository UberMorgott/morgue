//go:build race

package recipe

// raceEnabled reports a -race build; heap-bound stress tests skip there.
const raceEnabled = true
