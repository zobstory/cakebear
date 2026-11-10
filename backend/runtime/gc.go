package runtime

import (
	"os"
	"runtime/debug"
)

// defaultGCPercent is the GOGC a compiled program runs with when its
// environment sets none: 200, where Go's default is 100. The collector then
// lets the heap grow to three times what the last cycle kept before it runs
// again, instead of twice, which is fewer cycles for more memory.
//
// Measured on Linux (arm64, 12 CPUs, 2026-11-10) against Node 24.18, at 1, 8
// and 32 connections (CLAUDE.md, the serving perf step): CPU per request
// 0.72 to 0.98 times the default's, and peak memory at most 0.78 times
// Node's, at 32 connections of string building. 400 saved more CPU but
// peaked at 1.5 times Node's there, so it wasn't taken. Idle memory is the
// same at any setting.
const defaultGCPercent = 200

// A GOGC in the environment, `off` included, is the user's and stands.
func init() {
	if _, set := os.LookupEnv("GOGC"); !set {
		debug.SetGCPercent(defaultGCPercent)
	}
}
