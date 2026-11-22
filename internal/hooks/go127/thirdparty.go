package go127

import "github.com/jellevandenhooff/gosim/gosimruntime"

// Hooks of packages outside the standard library (thirdPartyHooksGo127 in
// cmd/gosim), which replace their assembly.

// GithubComPetermattisGoid_Get returns the goroutine's ID, which
// github.com/petermattis/goid reads from the runtime in assembly.
func GithubComPetermattisGoid_Get() int64 {
	return int64(gosimruntime.GetGoroutine())
}
