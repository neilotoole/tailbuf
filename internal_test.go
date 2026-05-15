package tailbuf

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// InternalWindow exposes Buf's internal window storage for tests that need
// to verify zero-fill or other low-level state.
func InternalWindow[T any](b *Buf[T]) []T {
	return b.window
}

// TailNewSlice exposes Buf's internal tailNewSlice helper for tests that
// want to verify the always-allocate variant of [Buf.Tail].
func TailNewSlice[T any](b *Buf[T]) []T {
	return b.tailNewSlice()
}

// CheckInvariants asserts the documented invariants on b at a public-method
// boundary. Intended for test use: call after a sequence of public-API
// operations to catch a refactor that introduces a state where the public
// API returns wrong-but-internally-consistent answers.
//
// Asserted invariants (a subset of those documented on Buf):
//
//   - 0 <= len <= cap
//   - When len > 0: oldestIdx ∈ [0, cap)
//   - When len == 0: oldestIdx == 0 (the canonical-empty invariant
//     established by every Pop/Drop/Clear/Reset path that can empty
//     the buffer; grep for "oldestIdx = 0" to find the call sites)
//   - offset >= 0 and written >= 0
//   - offset + len <= written
//
// The "equality holds iff none of PopFront / PopFrontN / DropFront /
// DropFrontN has removed an item since construction or the most recent
// Reset" rider from the package doc is not asserted here — that fact
// depends on call history that CheckInvariants cannot observe from local
// state alone.
func CheckInvariants[T any](tb testing.TB, b *Buf[T]) {
	tb.Helper()
	winLen := len(b.window)
	require.GreaterOrEqual(tb, b.len, 0, "len must be >= 0")
	require.LessOrEqual(tb, b.len, winLen, "len (%d) must be <= cap (%d)", b.len, winLen)
	if b.len > 0 {
		require.GreaterOrEqual(tb, b.oldestIdx, 0, "oldestIdx must be >= 0 when len > 0")
		require.Less(tb, b.oldestIdx, winLen, "oldestIdx (%d) must be < cap (%d) when len > 0", b.oldestIdx, winLen)
	} else {
		require.Equal(tb, 0, b.oldestIdx,
			"oldestIdx must be pinned to 0 when len == 0 (canonical-empty invariant)")
	}
	require.GreaterOrEqual(tb, b.offset, 0, "offset must be >= 0")
	require.GreaterOrEqual(tb, b.written, 0, "written must be >= 0")
	require.LessOrEqual(tb, b.offset+b.len, b.written,
		"offset+len (%d) must be <= written (%d)", b.offset+b.len, b.written)
}

// RequireEqualInternalState asserts that a and b have the same internal
// state: window contents (which implies same capacity), len, oldestIdx,
// offset, and written. Two Bufs that compare equal here must produce
// identical outputs from every public read on the type.
func RequireEqualInternalState[T any](tb testing.TB, a, b *Buf[T]) {
	tb.Helper()
	require.Equal(tb, a.window, b.window)
	require.Equal(tb, a.len, b.len)
	require.Equal(tb, a.oldestIdx, b.oldestIdx)
	require.Equal(tb, a.offset, b.offset)
	require.Equal(tb, a.written, b.written)
}
