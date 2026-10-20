package go127

// gosim runs no synctest bubbles: the whole simulation already has a fake
// clock and a deterministic scheduler. A goroutine is never in a bubble.
func InternalSynctest_IsInBubble() bool {
	return false
}
