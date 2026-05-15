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
