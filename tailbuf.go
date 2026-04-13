// Package tailbuf contains a fixed-size [Buf] that retains the most recent
// items written to it. Start with [tailbuf.New] to construct one.
//
// # Model
//
// A [Buf] is conceptually a sliding window over a longer "nominal" stream of
// writes. Each call to [Buf.Write] or [Buf.WriteAll] appends to that stream.
// When the tail window is at capacity, the next write evicts the oldest item
// to make room.
//
// Items in the tail window are addressed by nominal index. The i-th item in
// the tail occupies nominal index [Buf.Offset]() + i. [Buf.Bounds] returns
// the half-open nominal range [Offset, Offset+Len) currently held; [Buf.InBounds]
// reports whether a given nominal index is one of the live items.
//
// [Buf.Written] tracks the total count of writes ever made; it is independent
// of [Buf.Offset] and is not changed by pops.
//
// # Pop semantics
//
// [Buf.PopBack] / [Buf.DropBack] remove the oldest live item; this advances
// [Buf.Offset] just like an eviction would. [Buf.PopFront] removes the newest
// live item and does NOT advance [Buf.Offset]; the tail window simply shrinks
// from its newest end.
//
// One subtle consequence: after [Buf.PopFront], the next [Buf.Write] occupies
// the nominal index that the popped item had. The buffer does not preserve
// "holes" in nominal-index space; the live items always occupy a contiguous
// nominal range. Mixing pops with writes is supported, but if your code
// relies on a one-to-one mapping between writes and nominal indices, do not
// call [Buf.PopFront].
package tailbuf

import "context"

// Buf is an append-only, fixed-size circular buffer that exposes a window on
// the most recently written items. It is not safe for concurrent use.
//
// The zero value of Buf is a usable, empty, zero-capacity buffer that
// silently discards writes (it still increments [Buf.Written]). Use
// [tailbuf.New] to specify a non-zero capacity.
//
// Buf maintains the following invariants at every public-method boundary:
//
//   - 0 <= len <= len(window)
//   - When len > 0, back is in [0, len(window)) and points to the physical
//     index of the oldest live item.
//   - When len == 0, back's value is unspecified (callers must not read it).
//   - offset is monotonically non-decreasing; it is bumped only by
//     eviction-on-write, by Pop/Drop from the back, and by Clear.
//   - written is monotonically non-decreasing; it is bumped only by Write
//     and WriteAll.
//
// See the package documentation for the buffer's overall model.
type Buf[T any] struct {
	// window is the underlying circular storage. Its length is the buffer's
	// capacity (see [Buf.Cap]). When capacity is 0, window is nil.
	window []T

	// back is the physical index in window of the oldest live item.
	// Meaningful only when len > 0.
	//
	// Note: previous versions of Buf also tracked a `front` field, with
	// `back == -1` and `front == -1` used as an "empty" sentinel. That dual
	// state was removable: front is fully derivable from (back + len - 1)
	// modulo capacity, and emptiness is equivalent to len == 0. Removing
	// front cuts a class of state-coherence bugs (notably the zero-value
	// Front()/Back() panic; see Buf.Front for the historical bug A7).
	back int

	// len is the number of live items in the tail window.
	len int

	// offset is the nominal index of the oldest live item, equivalently the
	// count of items removed from the back of the tail (by eviction-on-write,
	// PopBack, DropBack, PopBackN, DropBackN, or Clear). PopFront does NOT
	// change offset.
	//
	// Tracking offset explicitly fixes bug A6: the previous implementation
	// derived it lazily as written-cap, which was correct only when no pops
	// had occurred and gave wrong answers for Bounds, Offset, and InBounds
	// after any pop or clear.
	offset int

	// written counts every successful Write/WriteAll item, including items
	// that were silently dropped because capacity is zero. It is independent
	// of offset and len; it is not modified by pops.
	written int
}

// New returns a new [Buf] with the specified capacity. It panics if capacity
// is negative. A buffer with capacity 0 is permitted and will silently
// discard items written to it (while still incrementing [Buf.Written]).
func New[T any](capacity int) *Buf[T] {
	if capacity < 0 {
		// Bug A8 fix: the previous panic message embedded a "FIXME" string,
		// which leaked development notes into the runtime error.
		panic("tailbuf: capacity must not be negative")
	}
	return &Buf[T]{
		window: make([]T, capacity),
	}
}

// Write appends item to the tail window. If the window is at capacity, the
// oldest live item is evicted to make room (and [Buf.Offset] advances by
// one). Returns b for chaining.
func (b *Buf[T]) Write(item T) *Buf[T] {
	if len(b.window) == 0 {
		// Capacity 0: the item is dropped on the floor, but Written still
		// reflects that the user attempted a write.
		b.written++
		return b
	}
	b.write(item)
	return b
}

// WriteAll appends each item to the tail window in order. Equivalent to
// calling [Buf.Write] for each item. Returns b for chaining.
func (b *Buf[T]) WriteAll(items ...T) *Buf[T] {
	if len(b.window) == 0 {
		b.written += len(items)
		return b
	}
	for i := range items {
		b.write(items[i])
	}
	return b
}

// write performs a single append. The caller has already verified
// len(b.window) > 0, so write never divides by zero on the modulus.
//
// Bug A5 fix: the eviction predicate is `b.len == cap`, not the previous
// `b.written > cap`. The old predicate was correct only when no items had
// ever been popped. Once PopFront ran and freed a slot, the next Write
// would still evict (because written had crossed cap historically), leaving
// the buffer in a logically-impossible state where Len() reported one count
// and Tail() returned a different one.
func (b *Buf[T]) write(item T) {
	b.written++
	winLen := len(b.window)
	switch {
	case b.len == 0:
		// First item into an empty tail. We pin back to 0 so the storage
		// fills sequentially when starting fresh; this also makes the
		// slice returned by Tail() share storage with window in the simple
		// no-wrap case.
		b.back = 0
		b.window[0] = item
		b.len = 1
	case b.len == winLen:
		// Tail at capacity; the new item replaces the oldest one in place.
		// The "oldest" advances by one, which also bumps offset because the
		// evicted item leaves the nominal range entirely.
		b.window[b.back] = item
		b.back = (b.back + 1) % winLen
		b.offset++
	default:
		// Room remaining; place item just past the current front.
		b.window[(b.back+b.len)%winLen] = item
		b.len++
	}
}

// Cap returns the buffer's fixed capacity.
func (b *Buf[T]) Cap() int {
	return len(b.window)
}

// Len returns the number of items currently in the tail window. The result
// is always in [0, [Buf.Cap]].
func (b *Buf[T]) Len() int {
	return b.len
}

// Written returns the total number of items passed to [Buf.Write] or
// [Buf.WriteAll]. It includes items that were evicted, popped, dropped, or
// silently discarded by a zero-capacity buffer.
//
// Written is independent of [Buf.Offset] and [Buf.Len]: after a [Buf.PopFront],
// Written is unchanged but the upper end of [Buf.Bounds] shrinks.
func (b *Buf[T]) Written() int {
	return b.written
}

// Offset returns the nominal index of the oldest live item, or 0 if the tail
// is empty. Equivalently, it is the number of items that have left the back
// of the tail by eviction-on-write or by [Buf.PopBack] / [Buf.DropBack] /
// [Buf.PopBackN] / [Buf.DropBackN] / [Buf.Clear]. [Buf.PopFront] does NOT
// advance Offset.
//
// Bug A6 fix: Offset is now tracked explicitly. The previous implementation
// derived it as max(0, written-cap), which gave wrong answers once any pop
// or clear had occurred.
func (b *Buf[T]) Offset() int {
	return b.offset
}

// Bounds returns the half-open nominal range [start, end) currently covered
// by the tail window. start is [Buf.Offset]; end is [Buf.Offset] + [Buf.Len].
// The range is empty when [Buf.Len] is 0 (start == end).
//
// Bug A6 fix: Bounds previously returned (Offset, Written), which was
// incorrect after a PopFront (Written did not shrink) and after a Clear
// (Written stayed at its pre-Clear value while Len was 0).
func (b *Buf[T]) Bounds() (start, end int) {
	return b.offset, b.offset + b.len
}

// InBounds reports whether the nominal index i corresponds to a live item in
// the tail window. Equivalent to:
//
//	start, end := b.Bounds()
//	b.Len() > 0 && i >= start && i < end
//
// Bug A6 fix: InBounds previously returned true for indices that were either
// never alive (after Clear with non-zero Written) or had been popped from
// the front. The check now uses Bounds, which reflects the live range.
func (b *Buf[T]) InBounds(i int) bool {
	if b.len == 0 || i < 0 {
		return false
	}
	return i >= b.offset && i < b.offset+b.len
}

// Front returns the newest live item, or the zero value of T when the tail
// is empty.
//
// Bug A7 fix: Front previously checked b.front == -1 to detect emptiness.
// That sentinel was set by [tailbuf.New] but not by the zero-value Buf
// (where the field defaulted to 0), so calling Front on a zero-value Buf
// indexed into a nil window and panicked, contradicting the package doc's
// "the zero value is usable" promise. The check now uses [Buf.Len].
func (b *Buf[T]) Front() T {
	if b.len == 0 {
		var zero T
		return zero
	}
	return b.window[(b.back+b.len-1)%len(b.window)]
}

// Back returns the oldest live item, or the zero value of T when the tail is
// empty.
//
// Bug A7 fix: same fix as [Buf.Front].
func (b *Buf[T]) Back() T {
	if b.len == 0 {
		var zero T
		return zero
	}
	return b.window[b.back]
}

// Peek returns the n-th item in the tail window, counting from the oldest
// (n=0). Panics if n is negative, n >= [Buf.Len], or the tail is empty.
//
// Cleanup (B4): the previous implementation forked on b.front > b.back, but
// both branches evaluated the same expression. The fork is gone; the
// modular arithmetic naturally handles wrap.
func (b *Buf[T]) Peek(n int) T {
	if n < 0 || n >= b.len {
		panic("tailbuf: Peek out of bounds")
	}
	return b.window[(b.back+n)%len(b.window)]
}

// Tail returns a slice containing the items currently in the tail window, in
// oldest-to-newest order. When the live items do not wrap around the
// internal window, the returned slice shares storage with the buffer and is
// invalidated by the next mutation. Use [SliceTail] for a slice that is
// always independently allocated.
//
// The single-item case returns a 1-element slice over the live position
// regardless of where it sits in the window; the wrap case allocates fresh
// storage.
func (b *Buf[T]) Tail() []T {
	if b.len == 0 {
		return b.window[:0]
	}
	winLen := len(b.window)
	front := (b.back + b.len - 1) % winLen
	if b.back <= front {
		return b.window[b.back : front+1]
	}
	return b.tailNewSlice()
}

// tailNewSlice always allocates a fresh slice of the live items.
func (b *Buf[T]) tailNewSlice() []T {
	if b.len == 0 {
		return []T{}
	}
	winLen := len(b.window)
	s := make([]T, b.len)
	for i := 0; i < b.len; i++ {
		s[i] = b.window[(b.back+i)%winLen]
	}
	return s
}

// zeroTail zeroes the storage slots holding live items. Called by [Buf.Reset],
// [Buf.Clear], and the bulk pop/drop helpers when the tail is being emptied.
func (b *Buf[T]) zeroTail() {
	if b.len == 0 {
		return
	}
	var zero T
	winLen := len(b.window)
	for i := 0; i < b.len; i++ {
		b.window[(b.back+i)%winLen] = zero
	}
}

// Reset empties the tail window AND resets [Buf.Written] and [Buf.Offset] to
// 0. The result is indistinguishable from a fresh [tailbuf.New] of the same
// capacity. Returns b for chaining.
//
// See also: [Buf.Clear].
func (b *Buf[T]) Reset() *Buf[T] {
	b.zeroTail()
	b.back = 0
	b.len = 0
	b.offset = 0
	b.written = 0
	return b
}

// Clear empties the tail window without resetting [Buf.Written]. The cleared
// items are conceptually evicted off the back, so [Buf.Offset] advances by
// the previous [Buf.Len]; this keeps [Buf.Bounds] consistent (the empty
// range starts at the position of the next write). Returns b for chaining.
//
// Bug A6 fix: Clear previously left b.len == 0 while leaving b.offset and
// b.written unchanged, so Bounds reported a non-empty range over indices
// that were no longer live.
//
// See also: [Buf.Reset].
func (b *Buf[T]) Clear() *Buf[T] {
	b.zeroTail()
	b.offset += b.len
	b.back = 0
	b.len = 0
	return b
}

// PopFront removes and returns the newest live item. Returns the zero value
// of T when the tail is empty. Does NOT change [Buf.Offset]; the tail window
// shrinks from its newest end.
func (b *Buf[T]) PopFront() T {
	if b.len == 0 {
		var zero T
		return zero
	}
	idx := (b.back + b.len - 1) % len(b.window)
	item := b.window[idx]
	var zero T
	b.window[idx] = zero
	b.len--
	return item
}

// PopFrontN removes and returns up to n newest items, in oldest-to-newest
// order (i.e. the last element of the returned slice is the item that was
// previously the front). Does NOT change [Buf.Offset]. n <= 0 returns an
// empty slice; n >= [Buf.Len] empties the tail and returns all items.
//
// The returned slice is freshly allocated.
//
// Cleanup (B4): the prior implementation forked on b.front > b.back; both
// branches were textually identical.
func (b *Buf[T]) PopFrontN(n int) []T {
	if b.len == 0 || n < 1 {
		return []T{}
	}
	if n >= b.len {
		s := b.tailNewSlice()
		b.zeroTail()
		// We deliberately do NOT bump b.offset (PopFront semantics shrink
		// from the front). We also don't call Clear, which would.
		b.back = 0
		b.len = 0
		return s
	}

	winLen := len(b.window)
	s := make([]T, n)
	// The n newest items occupy tail-relative positions [len-n, len). Visit
	// them in oldest-to-newest order so the returned slice has that order.
	base := b.len - n
	var zero T
	for i := 0; i < n; i++ {
		idx := (b.back + base + i) % winLen
		s[i] = b.window[idx]
		b.window[idx] = zero
	}
	b.len -= n
	return s
}

// PopBack removes and returns the oldest live item, advancing [Buf.Offset]
// by one. Returns the zero value of T when the tail is empty.
//
// See also: [Buf.DropBack].
func (b *Buf[T]) PopBack() T {
	if b.len == 0 {
		var zero T
		return zero
	}
	item := b.window[b.back]
	var zero T
	b.window[b.back] = zero
	b.back = (b.back + 1) % len(b.window)
	b.len--
	b.offset++
	return item
}

// PopBackN removes and returns up to n oldest items, in oldest-to-newest
// order, advancing [Buf.Offset] by the number actually removed. n <= 0
// returns an empty slice; n >= [Buf.Len] empties the tail and returns all
// items.
//
// The returned slice is freshly allocated.
//
// Cleanup (B4): the prior implementation forked on b.front > b.back; both
// branches were textually identical.
func (b *Buf[T]) PopBackN(n int) []T {
	if b.len == 0 || n < 1 {
		return []T{}
	}
	if n >= b.len {
		s := b.tailNewSlice()
		// Clear bumps offset by the live count, which matches the semantics
		// of popping every item from the back.
		b.Clear()
		return s
	}

	winLen := len(b.window)
	s := make([]T, n)
	var zero T
	for i := 0; i < n; i++ {
		s[i] = b.window[b.back]
		b.window[b.back] = zero
		b.back = (b.back + 1) % winLen
	}
	b.len -= n
	b.offset += n
	return s
}

// DropBack removes the oldest live item, advancing [Buf.Offset] by one. It
// is a no-op when the tail is empty.
//
// See also: [Buf.PopBack].
func (b *Buf[T]) DropBack() {
	if b.len == 0 {
		return
	}
	var zero T
	b.window[b.back] = zero
	b.back = (b.back + 1) % len(b.window)
	b.len--
	b.offset++
}

// DropBackN removes up to n oldest items, advancing [Buf.Offset] by the
// number actually removed. n <= 0 is a no-op; n >= [Buf.Len] empties the
// tail.
//
// Cleanup (B4): the prior implementation forked on b.front > b.back; both
// branches were textually identical.
func (b *Buf[T]) DropBackN(n int) {
	if b.len == 0 || n < 1 {
		return
	}
	if n >= b.len {
		b.Clear()
		return
	}

	winLen := len(b.window)
	var zero T
	for i := 0; i < n; i++ {
		b.window[b.back] = zero
		b.back = (b.back + 1) % winLen
	}
	b.len -= n
	b.offset += n
}

// Apply replaces each item in the tail window with fn(item), in
// oldest-to-newest order. fn is invoked exactly [Buf.Len] times. Returns b
// for chaining.
//
// Apply is cheaper than the equivalent loop over [Buf.Tail] (which may
// allocate when the live items wrap).
//
// Behavior is undefined if fn modifies b.
//
// Bug A1 fix: the prior implementation forked on b.front > b.back. The
// else-branch assumed front < back (a true wrap with len > 1) and iterated
// over [back, cap) and [0, front+1]. When front == back (always the case
// when len == 1, sometimes after pops), the else-branch ran fn on every
// dead position in window and applied fn to the single live item twice.
// Idempotent fns hid the bug; non-idempotent ones did not. The fix uses
// modular indexing over exactly len iterations.
func (b *Buf[T]) Apply(fn func(item T) T) *Buf[T] {
	winLen := len(b.window)
	for i := 0; i < b.len; i++ {
		idx := (b.back + i) % winLen
		b.window[idx] = fn(b.window[idx])
	}
	return b
}

// Do replaces each item in the tail window with the value returned by fn,
// halting (and returning the error) if fn returns one. fn is invoked at most
// [Buf.Len] times, in oldest-to-newest order.
//
// fn receives the item, the tail-relative index (0..Len-1), and the value of
// [Buf.Offset] at iteration start. The nominal index of the item is the sum
// of the latter two arguments.
//
// The context is passed through to fn but is not checked between calls;
// check it inside fn if cancellation is needed.
//
// Behavior is undefined if fn modifies b.
//
// Bug A1 fix: same iteration bug as [Buf.Apply], same fix.
//
// Argument-order fix: the prior implementation passed (item, physicalIndex,
// tailRelativeIndex) to fn, but the documentation described
// (item, tailRelativeIndex, tailOffset). Callers writing to the documented
// contract were reading both arguments wrong. The values now match the
// documented contract.
func (b *Buf[T]) Do(ctx context.Context, fn func(ctx context.Context, item T, index, tailOffset int) (T, error)) error {
	if b.len == 0 {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	winLen := len(b.window)
	for i := 0; i < b.len; i++ {
		idx := (b.back + i) % winLen
		v, err := fn(ctx, b.window[idx], i, b.offset)
		if err != nil {
			return err
		}
		b.window[idx] = v
	}
	return nil
}

// SliceNominal returns a freshly-allocated slice of items whose nominal
// indices fall in the half-open range [start, end). Nominal indices outside
// the current tail are clipped silently.
//
// Panics if end < start.
//
// SliceNominal is a thin wrapper over [SliceTail]: it translates nominal
// coordinates to tail-relative coordinates by subtracting [Buf.Offset], then
// delegates.
//
// Bug A4 fix: the prior implementation could panic with
// "slice bounds out of range" when the nominal range fell entirely past the
// end of a wrapped tail (because the underlying SliceTail did not handle
// over-large indices).
func SliceNominal[T any](b *Buf[T], start, end int) []T {
	if end < start {
		panic("tailbuf: end must be >= start")
	}

	// Translate nominal coordinates to tail-relative ones. Clamp the lower
	// bound to 0 (anything earlier was already evicted), and let SliceTail
	// clamp the upper bound to b.len.
	tailStart := start - b.offset
	if tailStart < 0 {
		tailStart = 0
	}
	tailEnd := end - b.offset
	if tailEnd <= tailStart {
		return []T{}
	}
	return SliceTail(b, tailStart, tailEnd)
}

// SliceTail returns a freshly-allocated slice of the items at tail-relative
// half-open positions [start, end). start counts from the oldest live item
// (i.e. position 0 is [Buf.Back]). Positions past the live tail are clipped
// silently. The returned slice never shares storage with the buffer.
//
// Panics if start < 0 or end < start.
//
// Bug A2 / A3 / A4 fix: the prior implementation indexed the simple-case
// branch as window[start:end], which was correct only when b.back happened
// to be 0; it special-cased b.front == b.back with a hard-coded
// window[0] read that returned the wrong value when the single item was
// elsewhere; and it could panic on out-of-range indices against a wrapped
// tail. The new implementation translates tail-relative positions to
// physical ones with the same modular formula used by [Buf.Tail],
// [Buf.Apply], etc.
func SliceTail[T any](b *Buf[T], start, end int) []T {
	if start < 0 {
		panic("tailbuf: start must be >= 0")
	}
	if end < start {
		panic("tailbuf: end must be >= start")
	}

	if b.len == 0 {
		return []T{}
	}
	if start > b.len {
		start = b.len
	}
	if end > b.len {
		end = b.len
	}
	n := end - start
	if n == 0 {
		return []T{}
	}

	winLen := len(b.window)
	s := make([]T, n)
	for i := 0; i < n; i++ {
		s[i] = b.window[(b.back+start+i)%winLen]
	}
	return s
}
