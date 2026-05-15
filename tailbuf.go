// Package tailbuf provides a fixed-size, generic [Buf] that retains the most
// recent items written to it. The package is intentionally small: callers
// construct a [Buf] with [New], append with [Buf.Write] / [Buf.WriteAll],
// inspect with [Buf.Tail] / [Buf.Peek] / [Buf.Front] / [Buf.Back], and
// optionally remove items from either end with the Pop and Drop families.
//
// # Quick start
//
//	buf := tailbuf.New[string](3)
//	buf.WriteAll("a", "b", "c", "d", "e")
//	fmt.Println(buf.Tail())   // [c d e]
//	fmt.Println(buf.Written()) // 5
//
// # Model
//
// A [Buf] is conceptually a sliding window over a longer "nominal" stream of
// writes. Each call to [Buf.Write] or [Buf.WriteAll] appends to that stream.
// When the tail window is at capacity, the next write evicts the oldest live
// item to make room.
//
// Items in the tail window are addressed by nominal index. The i-th item in
// the tail occupies nominal index [Buf.Offset] + i. [Buf.Bounds] returns the
// half-open nominal range [Offset, Offset+Len) currently held; [Buf.InBounds]
// reports whether a given nominal index is one of the live items.
//
// [Buf.Written] tracks the total count of writes ever made; it is independent
// of [Buf.Offset] and is not changed by pops. The relationship between the
// counters is:
//
//	Bounds()       == (Offset(), Offset()+Len())
//	Cap()          == fixed at construction
//	Len()          <= Cap()
//	Offset()       <= Written()
//	Offset()+Len() <= Written()  (equality iff PopFront/PopFrontN has never
//	                               removed an item since construction or
//	                               the most recent Reset)
//
// # Pop semantics
//
// [Buf.PopBack] / [Buf.DropBack] / [Buf.PopBackN] / [Buf.DropBackN] all
// remove the oldest live item(s). They advance [Buf.Offset] by the number
// removed, exactly as eviction-on-write would.
//
// [Buf.PopFront] / [Buf.PopFrontN] remove the newest live item(s). They do
// NOT advance [Buf.Offset]; the tail window simply shrinks from its newest
// end.
//
// One subtle consequence: after [Buf.PopFront], the next [Buf.Write] occupies
// the nominal index that the popped item had. The buffer does not preserve
// "holes" in nominal-index space; the live items always occupy a contiguous
// nominal range. Mixing pops with writes is supported, but if your code
// relies on a one-to-one mapping between writes and nominal indices, do not
// call [Buf.PopFront].
//
// # Slice aliasing
//
// [Buf.Tail] returns a slice that shares storage with the buffer's internal
// window when (and only when) the live items are non-empty and physically
// contiguous in the window (i.e. they have not wrapped around). The
// empty-buffer case returns a fresh non-aliasing []T{} regardless of
// internal state. The returned slice is invalidated by the next mutating
// call. If you need to retain the slice past further mutations, copy it,
// or use [SliceTail] / [SliceNominal], which never share storage with the
// buffer (and allocate a fresh backing array for every non-empty result).
//
// # Bounds policy
//
// The two slice helpers and the indexed read disagree deliberately on
// out-of-range arguments:
//
//   - [Buf.Peek] panics on an out-of-range tail index (negative, >= Len,
//     or any non-negative index when Len is 0).
//   - [SliceTail] (tail-relative coordinates) panics on start < 0 and on
//     end < start; positions past the live tail are clipped silently.
//   - [SliceNominal] (nominal coordinates) panics only on end < start.
//     start values below [Buf.Offset] — including negative ones — are
//     clipped to "below the live range" rather than panicking, because
//     in nominal-index space "below Offset" just means "already evicted":
//     a meaningful, not erroneous, condition. Positions past the live
//     tail are clipped silently.
//
// Pick [Buf.Peek] when an out-of-range index is a programming error you want
// to surface loudly; pick the slice helpers when you want a "give me
// whatever falls inside this range" semantic that tolerates ranges that
// have partly fallen off the buffer. Prefer [SliceNominal] when working
// in nominal coordinates and you want negative or below-Offset start
// values to be treated as "already evicted" rather than as bugs.
//
// # Concurrency
//
// [Buf] is not safe for concurrent use. Mutating methods change buffer state,
// and read methods are not safe to use concurrently with those mutations.
// In addition, [Buf.Tail] may return a slice aliasing the internal window,
// so callers must not read or write that slice concurrently with any
// [Buf] operation without their own synchronization. Share a [Buf] across
// goroutines only under a lock you own, and treat the slice returned by
// [Buf.Tail] as valid only until the next method call on the buffer.
//
// # Methods overview
//
// Construction and identity:
//
//	New, Cap, Len, Written, Offset, Bounds, InBounds
//
// Read access:
//
//	Tail                  — alias-or-allocate (see "Slice aliasing" above)
//	Front, Back, Peek     — single-item reads (Peek panics on out-of-range)
//	SliceTail, SliceNominal — always allocate; clip silently on out-of-range
//
// Mutation (append):
//
//	Write, WriteAll
//
// Mutation (in place):
//
//	Apply, Do
//
// Mutation (selective remove):
//
//	PopFront,  PopFrontN  — remove from the newest end (no Offset change)
//	PopBack,   PopBackN   — remove from the oldest end (Offset advances)
//	DropFront, DropFrontN — like PopFront/PopFrontN but discard the result
//	DropBack,  DropBackN  — like PopBack/PopBackN but discard the result
//
// Mutation (bulk empty):
//
//	Clear                — empty the tail; preserve Written
//	Reset                — empty the tail; reset Written and Offset
package tailbuf

import "context"

// Buf is an append-only, fixed-size circular buffer that exposes a window on
// the most recently written items. It is parameterized over the item type T.
//
// # Zero value
//
// The zero value is usable as an empty zero-capacity buffer: it accepts
// writes (silently dropping the items) and returns the zero value of T from
// every read. Use [New] to specify a non-zero capacity.
//
// # Concurrency
//
// Buf is not safe for concurrent use; see the package documentation for
// details.
//
// # Internal layout
//
// The buffer is laid out as a single backing slice (window) used as a ring.
// oldestIdx is the physical index of the oldest live item; the live items
// occupy "len" consecutive positions starting at oldestIdx, modulo capacity.
//
// Example A — no wrap (cap=5, oldestIdx=1, len=3):
//
//	physical:    0     1     2     3     4
//	window:    [ _  |  A  |  B  |  C  |  _ ]
//	                  ^oldestIdx
//	                                       (oldest-to-newest: A, B, C)
//
// Example B — wrapped (cap=5, oldestIdx=3, len=4):
//
//	physical:    0     1     2     3     4
//	window:    [ C  |  D  |  _  |  A  |  B ]
//	                              ^oldestIdx
//	                                       (oldest-to-newest: A, B, C, D)
//
// In both cases, the i-th live item (oldest-to-newest) sits at physical
// index (oldestIdx + i) mod cap.
//
// # Invariants
//
// Buf maintains the following invariants at every public-method boundary:
//
//   - 0 <= len <= len(window)
//   - When len > 0: oldestIdx ∈ [0, len(window)) and points to the oldest
//     live item; the newest live item is at
//     (oldestIdx + len - 1) mod len(window).
//   - When len == 0: oldestIdx has no semantic meaning to callers. The
//     implementation pins it to 0 on every path that empties the buffer
//     so that two empty buffers reached via different operation sequences
//     have identical internal state, but callers must not rely on the
//     value; future implementations may relax this canonicalization.
//   - offset never decreases except across [Buf.Reset]; it advances on
//     eviction-on-write (including the implicit eviction on every
//     [Buf.Write] / [Buf.WriteAll] against a zero-capacity buffer), on
//     [Buf.PopBack] / [Buf.PopBackN] / [Buf.DropBack] / [Buf.DropBackN], and
//     on [Buf.Clear]. [Buf.PopFront] / [Buf.PopFrontN] do NOT change offset.
//   - written never decreases except across [Buf.Reset]; it is bumped only
//     by [Buf.Write] and [Buf.WriteAll] (including writes silently dropped
//     by a zero-capacity Buf).
//   - offset + len <= written, with equality iff [Buf.PopFront] /
//     [Buf.PopFrontN] has never removed an item since construction or
//     the most recent [Buf.Reset].
type Buf[T any] struct {
	// window is the underlying circular storage. Its length is the buffer's
	// capacity (see [Buf.Cap]). When capacity is 0, window has length 0: it
	// is nil for the zero-value [Buf], and a non-nil empty slice for one
	// constructed by [New](0). That representation detail is not
	// observable through the public API.
	window []T

	// oldestIdx is the physical index in window of the oldest live item.
	// Meaningful only when len > 0; otherwise its value is irrelevant to
	// callers. The implementation canonicalizes it to 0 on every emptying
	// path so internal state is deterministic and two empty buffers
	// compare equal regardless of history; callers must not rely on
	// this — future implementations may relax the canonicalization.
	//
	// Name choice: this is "the back of the tail" in the package's
	// vocabulary, so [Buf.Back] returns window[oldestIdx]. Naming the
	// field oldestIdx (rather than back) avoids a maintainer-trap where
	// "back the cursor" is read as "the newest end" — common ring-buffer
	// literature uses head/tail to mean read/write ends in the opposite
	// orientation, so the explicit name removes that confusion.
	//
	// There is no parallel "newestIdx" field: the newest live item sits at
	// (oldestIdx + len - 1) mod len(window) and is derived on demand. A
	// single cursor + len is enough to describe the ring; carrying both
	// ends invites coherence bugs around the empty state.
	oldestIdx int

	// len is the number of live items in the tail window; always in
	// [0, len(window)].
	len int

	// offset is the nominal index of the oldest live item, equivalently the
	// count of items removed from the back of the tail by any of:
	// eviction-on-write, a Write/WriteAll against a zero-capacity buffer (a
	// conceptual eviction-on-write), PopBack, DropBack, PopBackN, DropBackN,
	// or Clear. PopFront does NOT change offset.
	//
	// offset is tracked explicitly rather than derived from written-cap; a
	// lazily-derived value would be wrong whenever a pop or clear has
	// happened, because either of those changes len without changing written.
	offset int

	// written counts every successful Write/WriteAll item, including items
	// that were silently dropped because capacity is zero. It is independent
	// of offset and len; it is not modified by pops.
	written int
}

// New returns a new [Buf] with the specified capacity. It panics if capacity
// is negative.
//
// A buffer with capacity 0 is permitted and behaves as a counter only:
// [Buf.Write] / [Buf.WriteAll] silently discard items but still bump
// [Buf.Written]. This is useful when the caller wants to enable retention
// conditionally without changing call sites.
func New[T any](capacity int) *Buf[T] {
	if capacity < 0 {
		panic("tailbuf: capacity must not be negative")
	}
	return &Buf[T]{
		window: make([]T, capacity),
	}
}

// Write appends item to the tail window and returns b for chaining. If the
// window is at capacity, the oldest live item is evicted to make room and
// [Buf.Offset] advances by one. Always increments [Buf.Written] (even when
// the buffer's capacity is zero and the item is silently dropped).
//
// See also: [Buf.WriteAll] for batch appends.
func (b *Buf[T]) Write(item T) *Buf[T] {
	if len(b.window) == 0 {
		// Capacity 0: every write is conceptually an eviction-on-write
		// (no room, so the item leaves the tail immediately). Advancing
		// both written and offset keeps the invariant
		// Offset()+Len() == Written() holding when no PopFront has run;
		// without this, a cap=0 buffer would silently break that
		// equality after the first Write.
		b.written++
		b.offset++
		return b
	}
	b.write(item)
	return b
}

// WriteAll appends each item to the tail window in the order given and
// returns b for chaining. Equivalent to calling [Buf.Write] for each item,
// but slightly cheaper because it avoids the per-item return value.
//
// If len(items) > [Buf.Cap], only the last [Buf.Cap] items remain in the
// tail (the earlier writes are immediately evicted as later writes arrive).
// Always increments [Buf.Written] by len(items), even when the buffer's
// capacity is zero and every item is silently dropped.
func (b *Buf[T]) WriteAll(items ...T) *Buf[T] {
	if len(b.window) == 0 {
		// See [Buf.Write] for why offset moves in lockstep with written in
		// the zero-capacity case.
		b.written += len(items)
		b.offset += len(items)
		return b
	}
	for i := range items {
		b.write(items[i])
	}
	return b
}

// write performs a single append. The caller has already verified
// len(b.window) > 0, so the modulus operations never divide by zero.
//
// The three cases (empty / full / partial) cover every reachable state of
// the buffer:
//
//   - empty:   no live items; place at window[0] and pin oldestIdx=0.
//   - full:    overwrite the slot at oldestIdx, then advance oldestIdx and
//     offset. The just-written slot is now the newest live item;
//     oldestIdx has moved on to what used to be the second-oldest.
//   - partial: place just past the current front; bump len.
//
// The eviction predicate is `b.len == cap`. A predicate over written (e.g.
// `b.written > cap`) would diverge from len after any PopFront, since
// PopFront shrinks len but leaves written unchanged.
func (b *Buf[T]) write(item T) {
	b.written++
	winLen := len(b.window)
	switch b.len {
	case 0:
		// First item into an empty tail. We pin oldestIdx to 0 so the storage
		// fills sequentially when starting fresh; this also makes the
		// slice returned by Tail() share storage with window in the simple
		// no-wrap case.
		b.oldestIdx = 0
		b.window[0] = item
		b.len = 1
	case winLen:
		// Tail at capacity; the new item is written into the slot currently
		// holding the oldest live item, evicting it. We then advance
		// oldestIdx so that slot is the newest position and the
		// next-oldest item becomes the new oldest. Offset bumps because
		// the evicted item leaves the nominal range entirely.
		b.window[b.oldestIdx] = item
		b.oldestIdx = (b.oldestIdx + 1) % winLen
		b.offset++
	default:
		// Room remaining; place item just past the current front.
		b.window[(b.oldestIdx+b.len)%winLen] = item
		b.len++
	}
}

// Cap returns the buffer's fixed capacity, which is the value passed to
// [New] (or 0 for the zero value of [Buf]). Cap never changes after
// construction.
func (b *Buf[T]) Cap() int {
	return len(b.window)
}

// Len returns the number of items currently in the tail window. The result
// is always in the inclusive range 0 .. [Buf.Cap].
//
// Len decreases on Pop/Drop/Clear/Reset; it grows on Write/WriteAll up to
// [Buf.Cap], at which point further writes evict instead of growing.
func (b *Buf[T]) Len() int {
	return b.len
}

// Written returns the total number of items passed to [Buf.Write] or
// [Buf.WriteAll]. It includes items that were evicted, popped, dropped, or
// silently discarded by a zero-capacity buffer.
//
// Written is independent of [Buf.Offset] and [Buf.Len]: after [Buf.PopFront],
// Written is unchanged but the upper end of [Buf.Bounds] shrinks. To recover
// the count of items currently retained, use [Buf.Len]. To recover the count
// of items removed from the back of the tail, use [Buf.Offset].
//
// Written is reset only by [Buf.Reset]; [Buf.Clear] preserves it.
func (b *Buf[T]) Written() int {
	return b.written
}

// Offset returns the nominal index of the oldest live item. When the tail
// is empty, Offset is still well-defined: it equals the start of the empty
// [Buf.Bounds] range — equivalently, the nominal index that the next
// retained item will occupy.
//
// Offset is 0 for a freshly-constructed buffer but may be non-zero on an
// empty buffer after any of: eviction-on-write (including any [Buf.Write]
// or [Buf.WriteAll] on a zero-capacity buffer), [Buf.PopBack] /
// [Buf.DropBack] / [Buf.PopBackN] / [Buf.DropBackN], or [Buf.Clear].
//
// Equivalently, Offset is the number of items that have left the back of
// the tail by any of those routes. [Buf.PopFront] does NOT advance Offset.
func (b *Buf[T]) Offset() int {
	return b.offset
}

// Bounds returns the half-open nominal range [start, end) currently covered
// by the tail window. start equals [Buf.Offset]; end equals [Buf.Offset] +
// [Buf.Len]. The range is empty when [Buf.Len] is 0 (start == end).
//
// Typical use is to translate between tail indices and nominal indices:
//
//	start, end := buf.Bounds()
//	for nominal := start; nominal < end; nominal++ {
//	    item := buf.Peek(nominal - start)
//	    // ... use item with its nominal index ...
//	}
//
// Bounds derives end from offset + len, NOT from written. After a
// [Buf.PopFront] (which shrinks len without changing written), or after a
// [Buf.Clear] (len resets to 0 but written is preserved), using written
// here would over-report the live range.
func (b *Buf[T]) Bounds() (start, end int) {
	return b.offset, b.offset + b.len
}

// InBounds reports whether nominalIndex corresponds to a live item in the
// tail window. The argument is in nominal-index space (see [Buf.Bounds] /
// [Buf.Offset]), NOT the tail-relative space accepted by [Buf.Peek].
// Equivalent to:
//
//	start, end := b.Bounds()
//	b.Len() > 0 && nominalIndex >= start && nominalIndex < end
//
// InBounds returns false when the buffer is empty, when nominalIndex is
// negative, when it is below the current [Buf.Offset] (the item has been
// evicted or popped from the back), and when it is at or beyond
// [Buf.Offset] + [Buf.Len] (the item was never live, or was popped from
// the front).
func (b *Buf[T]) InBounds(nominalIndex int) bool {
	if b.len == 0 || nominalIndex < 0 {
		return false
	}
	return nominalIndex >= b.offset && nominalIndex < b.offset+b.len
}

// Front returns the newest live item, or the zero value of T when the tail
// is empty. Front does not modify the buffer; see [Buf.PopFront] for the
// removing variant.
//
// Empty-check uses [Buf.Len] rather than any sentinel value on the
// oldest-item cursor. This makes a zero-value Buf safe to call Front on:
// len defaults to 0 there, the empty branch fires, and the nil internal
// window is never indexed.
func (b *Buf[T]) Front() T {
	if b.len == 0 {
		var zero T
		return zero
	}
	return b.window[(b.oldestIdx+b.len-1)%len(b.window)]
}

// Back returns the oldest live item, or the zero value of T when the tail is
// empty. Back does not modify the buffer; see [Buf.PopBack] for the removing
// variant.
//
// Same zero-value-safety reasoning as [Buf.Front]: empty-check uses
// [Buf.Len] so a zero-value Buf (with a nil internal window) is never
// indexed.
func (b *Buf[T]) Back() T {
	if b.len == 0 {
		var zero T
		return zero
	}
	return b.window[b.oldestIdx]
}

// Peek returns the item at the given tail-relative index, counting from
// the oldest live item: tailIndex 0 is [Buf.Back], tailIndex Len-1 is
// [Buf.Front]. The argument is in tail-relative space, NOT the
// nominal-index space used by [Buf.InBounds] / [Buf.Bounds] / [Buf.Offset].
// Panics on an out-of-range tail index (negative, or >= Len) or on an
// empty tail.
//
// Peek is O(1) and does not allocate.
//
// To convert a nominal index to a Peek argument, subtract [Buf.Offset]:
//
//	if buf.InBounds(nominal) {
//	    item := buf.Peek(nominal - buf.Offset())
//	}
func (b *Buf[T]) Peek(tailIndex int) T {
	if tailIndex < 0 || tailIndex >= b.len {
		panic("tailbuf: Peek out of bounds")
	}
	return b.window[(b.oldestIdx+tailIndex)%len(b.window)]
}

// Tail returns a slice containing the items currently in the tail window, in
// oldest-to-newest order. The returned slice is empty when the buffer is
// empty.
//
// # Storage aliasing
//
// When the live items do not wrap around the internal window, the returned
// slice shares storage with the buffer; mutating elements via that slice
// also mutates the buffer's view of those items, and the slice is
// invalidated by any subsequent mutating call on the buffer — Write /
// WriteAll, any Pop* or Drop* variant, Clear, Reset, Apply, or Do.
//
// Tail applies a full-slice expression to cap the returned slice's capacity
// at its length. This means [append]-ing to the returned slice allocates a
// fresh backing array rather than silently writing into the ring past the
// live region, so callers cannot accidentally corrupt internal state.
// (Pre-cap behavior: append would overwrite window slots that Buf still
// considered free, breaking len / oldestIdx / offset coherence.)
//
// When the live items wrap, Tail allocates a fresh slice; the returned
// slice is independent of the buffer.
//
// To force the always-allocate behavior, use [SliceTail] (which is also a
// good choice when the caller wants a stable snapshot regardless of the
// internal layout).
//
// # Edge cases
//
// The single-item case returns a 1-element slice over the live position
// regardless of where it sits in the window; the wrap case allocates fresh
// storage.
func (b *Buf[T]) Tail() []T {
	if b.len == 0 {
		// Return a non-nil empty slice rather than b.window[:0:0]: the
		// latter would propagate nilness from the underlying window (nil
		// for the zero-value Buf, non-nil for New(0)), and that would
		// make the nil-vs-empty distinction observable through the
		// public API — contradicting the contract on the window field.
		// An allocation-free literal matches what tailNewSlice,
		// SliceTail, and SliceNominal return for the empty case.
		return []T{}
	}
	winLen := len(b.window)
	front := (b.oldestIdx + b.len - 1) % winLen
	if b.oldestIdx <= front {
		// No wrap: live items occupy window[oldestIdx .. front+1]. Returning
		// a sub-slice of the underlying storage with a 3-index full-slice
		// expression pins cap == len, so append allocates fresh rather
		// than clobbering window[front+1] and beyond.
		return b.window[b.oldestIdx : front+1 : front+1]
	}
	// Wrapped: live items span window[oldestIdx:cap] + window[0:front+1].
	// We must allocate a fresh slice to present them contiguously.
	return b.tailNewSlice()
}

// tailNewSlice always allocates a fresh, contiguous slice of the live items
// in oldest-to-newest order. Used as the wrap-case body of [Buf.Tail] and
// as a building block for the bulk pop helpers.
func (b *Buf[T]) tailNewSlice() []T {
	if b.len == 0 {
		return []T{}
	}
	winLen := len(b.window)
	s := make([]T, b.len)
	for i := 0; i < b.len; i++ {
		s[i] = b.window[(b.oldestIdx+i)%winLen]
	}
	return s
}

// zeroTail zeroes the storage slots holding live items, so that callers
// don't keep stale references to garbage-collectable values past their
// useful life. Called by [Buf.Reset], [Buf.Clear], [Buf.PopFrontN], and
// [Buf.PopBackN] when those helpers are emptying (rather than partially
// shrinking) the tail.
func (b *Buf[T]) zeroTail() {
	if b.len == 0 {
		return
	}
	var zero T
	winLen := len(b.window)
	for i := 0; i < b.len; i++ {
		b.window[(b.oldestIdx+i)%winLen] = zero
	}
}

// Reset empties the tail window AND resets [Buf.Written] and [Buf.Offset] to
// 0. The result is indistinguishable from a fresh [New] of the same
// capacity. Returns b for chaining.
//
// Use Reset when the buffer should forget its history entirely (e.g. between
// independent batches of items).
//
// See also: [Buf.Clear], which empties the tail without resetting the
// counters.
func (b *Buf[T]) Reset() *Buf[T] {
	b.zeroTail()
	b.oldestIdx = 0
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
// Use Clear when the buffer should drop its current contents but continue
// accumulating Written and Offset across the boundary (e.g. when you want
// the next write to receive a fresh nominal index past the cleared region).
//
// See also: [Buf.Reset], which also resets [Buf.Written] and [Buf.Offset].
func (b *Buf[T]) Clear() *Buf[T] {
	b.zeroTail()
	b.offset += b.len
	b.oldestIdx = 0
	b.len = 0
	return b
}

// PopFront removes and returns the newest live item. Returns the zero value
// of T when the tail is empty.
//
// PopFront does NOT change [Buf.Offset]; the tail window simply shrinks from
// its newest end. Note in particular that the next [Buf.Write] will reuse
// the nominal index that the popped item had (see the package documentation
// for the consequences).
//
// See also: [Buf.PopFrontN] for the bulk variant; [Buf.DropFront] when the
// returned value is not needed; [Buf.Front] for a non-removing peek at the
// same item.
func (b *Buf[T]) PopFront() T {
	if b.len == 0 {
		var zero T
		return zero
	}
	idx := (b.oldestIdx + b.len - 1) % len(b.window)
	item := b.window[idx]
	var zero T
	b.window[idx] = zero
	b.len--
	if b.len == 0 {
		// Pin oldestIdx to 0 so every emptying path lands in the same
		// canonical empty state. The field is documented as semantically
		// meaningless when len == 0; pinning it here lets that doc claim
		// be unconditionally true and makes empty buffers compare equal
		// regardless of how they reached empty.
		b.oldestIdx = 0
	}
	return item
}

// PopFrontN removes and returns up to n newest items. The returned slice has
// the items in oldest-to-newest order — that is, the LAST element of the
// returned slice is the one that was previously the front, and the FIRST
// element is the one that was n positions back from the front.
//
// n <= 0 returns an empty slice; n >= [Buf.Len] empties the tail and returns
// all items. The returned slice is freshly allocated.
//
// PopFrontN does NOT change [Buf.Offset]; same caveat as [Buf.PopFront].
//
// See also: [Buf.DropFrontN] when the returned values are not needed.
func (b *Buf[T]) PopFrontN(n int) []T {
	if b.len == 0 || n < 1 {
		return []T{}
	}
	if n >= b.len {
		s := b.tailNewSlice()
		b.zeroTail()
		// We deliberately do NOT bump b.offset (PopFront semantics shrink
		// from the front). We also don't call Clear, which would.
		b.oldestIdx = 0
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
		idx := (b.oldestIdx + base + i) % winLen
		s[i] = b.window[idx]
		b.window[idx] = zero
	}
	b.len -= n
	return s
}

// PopBack removes and returns the oldest live item, advancing [Buf.Offset]
// by one. Returns the zero value of T when the tail is empty.
//
// See also: [Buf.PopBackN] for the bulk variant; [Buf.DropBack] when the
// returned value is not needed; [Buf.Back] for a non-removing peek.
func (b *Buf[T]) PopBack() T {
	if b.len == 0 {
		var zero T
		return zero
	}
	item := b.window[b.oldestIdx]
	var zero T
	b.window[b.oldestIdx] = zero
	b.oldestIdx = (b.oldestIdx + 1) % len(b.window)
	b.len--
	b.offset++
	if b.len == 0 {
		// See PopFront for the rationale; same canonical-empty pin.
		b.oldestIdx = 0
	}
	return item
}

// PopBackN removes and returns up to n oldest items in oldest-to-newest
// order, advancing [Buf.Offset] by the number actually removed. n <= 0
// returns an empty slice; n >= [Buf.Len] empties the tail and returns all
// items.
//
// The returned slice is freshly allocated.
//
// See also: [Buf.DropBackN] when the returned value is not needed.
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
		s[i] = b.window[b.oldestIdx]
		b.window[b.oldestIdx] = zero
		b.oldestIdx = (b.oldestIdx + 1) % winLen
	}
	b.len -= n
	b.offset += n
	return s
}

// DropFront removes the newest live item without returning it. It is a
// no-op when the tail is empty.
//
// DropFront is identical to [Buf.PopFront] except that it does not return
// the removed item; prefer it when the caller doesn't need the value back.
// Like PopFront, it does NOT advance [Buf.Offset]; the tail window simply
// shrinks from its newest end.
//
// See also: [Buf.PopFront], [Buf.DropFrontN].
func (b *Buf[T]) DropFront() {
	if b.len == 0 {
		return
	}
	idx := (b.oldestIdx + b.len - 1) % len(b.window)
	var zero T
	b.window[idx] = zero
	b.len--
	if b.len == 0 {
		// See PopFront for the rationale; same canonical-empty pin.
		b.oldestIdx = 0
	}
}

// DropFrontN removes up to n newest items without returning them. n <= 0
// is a no-op; n >= [Buf.Len] empties the tail.
//
// DropFrontN is identical to [Buf.PopFrontN] except that it does not
// allocate or return the removed items; prefer it when the caller doesn't
// need the values back. Like PopFrontN, it does NOT advance [Buf.Offset].
//
// See also: [Buf.PopFrontN], [Buf.DropFront].
func (b *Buf[T]) DropFrontN(n int) {
	if b.len == 0 || n < 1 {
		return
	}
	if n >= b.len {
		b.zeroTail()
		// Same caveat as PopFrontN: do NOT bump b.offset (PopFront
		// semantics shrink from the front), and don't call Clear which
		// would. Pin oldestIdx to 0 explicitly to match the canonical-
		// empty invariant.
		b.oldestIdx = 0
		b.len = 0
		return
	}

	winLen := len(b.window)
	// Zero the n newest slots in place. They occupy tail-relative
	// positions [len-n, len).
	base := b.len - n
	var zero T
	for i := 0; i < n; i++ {
		idx := (b.oldestIdx + base + i) % winLen
		b.window[idx] = zero
	}
	b.len -= n
}

// DropBack removes the oldest live item, advancing [Buf.Offset] by one. It
// is a no-op when the tail is empty.
//
// DropBack is identical to [Buf.PopBack] except that it does not allocate
// or return the removed item; prefer it when the caller doesn't need the
// value back.
//
// See also: [Buf.PopBack], [Buf.DropBackN].
func (b *Buf[T]) DropBack() {
	if b.len == 0 {
		return
	}
	var zero T
	b.window[b.oldestIdx] = zero
	b.oldestIdx = (b.oldestIdx + 1) % len(b.window)
	b.len--
	b.offset++
	if b.len == 0 {
		// See PopFront for the rationale; same canonical-empty pin.
		b.oldestIdx = 0
	}
}

// DropBackN removes up to n oldest items, advancing [Buf.Offset] by the
// number actually removed. n <= 0 is a no-op; n >= [Buf.Len] empties the
// tail.
//
// DropBackN is identical to [Buf.PopBackN] except that it does not allocate
// or return the removed items; prefer it when the caller doesn't need the
// values back.
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
		b.window[b.oldestIdx] = zero
		b.oldestIdx = (b.oldestIdx + 1) % winLen
	}
	b.len -= n
	b.offset += n
}

// Apply replaces each item in the tail window with fn(item), in
// oldest-to-newest order. fn is invoked exactly [Buf.Len] times. Returns b
// for chaining.
//
// Apply iterates in place without allocating, and handles wrap
// transparently. Compared to a hand-rolled loop over [Buf.Tail]: Apply
// skips the allocation that [Buf.Tail] must do when the live items wrap,
// so Apply is meaningfully faster in that case. When the items do not
// wrap, a direct loop over [Buf.Tail] is roughly twice as fast (the
// compiler inlines the loop body and avoids the per-iteration modular
// indexing that Apply must perform; see the package benchmarks for
// current absolute numbers). Apply is the natural choice when you want
// correctness under wrap without having to think about it, and when you
// don't need an index, an early exit, or an error result — for those,
// use [Buf.Do].
//
// Behavior is undefined if fn modifies b (whether by writing, popping, or
// otherwise).
//
// # Example
//
//	buf := tailbuf.New[string](3).WriteAll(" hi ", " HO ", " HUM ")
//	buf.Apply(strings.TrimSpace).Apply(strings.ToLower)
//	fmt.Println(buf.Tail()) // [hi ho hum]
//
// The implementation uses uniform modular indexing — exactly len iterations,
// no special-case fork on whether the live items wrap. A simpler-looking
// "no-wrap branch + wrapped branch" structure has to disambiguate cases
// like len == 1 (where the oldest and newest cursors coincide) and post-pop wraps
// (which the wrapped branch can miscount), and getting that right is
// fragile.
func (b *Buf[T]) Apply(fn func(item T) T) *Buf[T] {
	winLen := len(b.window)
	for i := 0; i < b.len; i++ {
		idx := (b.oldestIdx + i) % winLen
		b.window[idx] = fn(b.window[idx])
	}
	return b
}

// Do replaces each item in the tail window with the value returned by fn,
// halting (and returning the error) if fn returns one. fn is invoked at
// most [Buf.Len] times, in oldest-to-newest order; if fn returns a non-nil
// error at iteration i, items at positions [0, i) have been replaced with
// the values fn returned and items at positions [i, Len) are unchanged.
//
// The (T, error) pair returned by fn is consumed together: if err is
// non-nil, the returned T at that iteration is discarded. Callers that
// want a "best-effort transform plus a non-fatal warning" pattern should
// return (newValue, nil) and surface the warning out of band; Do is not
// suitable for partial-success-with-payload semantics.
//
// fn receives:
//
//   - ctx        — the same context Do was called with (or context.Background
//     if Do was called with nil).
//   - item       — the current value at the position.
//   - index      — the tail-relative index, 0..Len-1, oldest-to-newest.
//   - tailOffset — the value of [Buf.Offset] at the start of iteration; this
//     is constant across all calls in a single Do invocation. The nominal
//     index of the item is index + tailOffset.
//
// The context is passed through to fn but is not checked between calls;
// check it inside fn if cancellation is needed.
//
// Behavior is undefined if fn modifies b (whether by writing, popping, or
// otherwise).
//
// # Example
//
//	err := buf.Do(ctx, func(ctx context.Context, item string, index, tailOffset int) (string, error) {
//	    if err := ctx.Err(); err != nil {
//	        return item, err
//	    }
//	    return fmt.Sprintf("%d: %s", index+tailOffset, item), nil
//	})
func (b *Buf[T]) Do(ctx context.Context, fn func(ctx context.Context, item T, index, tailOffset int) (T, error)) error {
	if b.len == 0 {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	// Snapshot tailOffset once so the value passed to fn is constant for the
	// duration of this call, matching the godoc contract.
	tailOffset := b.offset
	winLen := len(b.window)
	for i := 0; i < b.len; i++ {
		idx := (b.oldestIdx + i) % winLen
		v, err := fn(ctx, b.window[idx], i, tailOffset)
		if err != nil {
			// Halt without writing v back: positions [0, i) keep the values
			// fn already returned, positions [i, Len) are untouched, and v
			// is discarded per the contract documented above.
			return err
		}
		b.window[idx] = v
	}
	return nil
}

// SliceNominal returns a freshly-allocated slice of items whose nominal
// indices fall in the half-open range [start, end). Nominal indices outside
// the current tail are clipped silently: indices below [Buf.Offset] are
// skipped (including negative ones — see below), and indices at or beyond
// [Buf.Offset] + [Buf.Len] are skipped. If the requested range and the
// live range do not overlap, the returned slice is empty.
//
// Panics if end < start. Negative start values do NOT panic: a nominal
// index below [Buf.Offset] denotes an item that has already been evicted
// from the back, and negative is just the extreme case of that — clipped
// to the start of the live range like any other below-Offset index. This
// is the deliberate asymmetry between SliceNominal (nominal coordinates,
// where "below Offset" is meaningful) and [SliceTail] (tail-relative
// coordinates, where start < 0 has no meaning and panics).
//
// SliceNominal is a thin wrapper over [SliceTail]: it translates nominal
// coordinates to tail-relative coordinates by subtracting [Buf.Offset], then
// delegates. The returned slice never shares storage with the buffer.
//
// See the "Bounds policy" section of the package doc for the broader
// rationale and the contrast with [Buf.Peek].
//
// # Example
//
//	buf := tailbuf.New[int](3).WriteAll(1, 2, 3, 4, 5)
//	// Bounds = (2, 5); the live tail is [3, 4, 5].
//	tailbuf.SliceNominal(buf, 2, 5)    // [3 4 5]
//	tailbuf.SliceNominal(buf, 1, 3)    // [3]      (1 is below Offset, clipped)
//	tailbuf.SliceNominal(buf, -10, 4)  // [3 4]   (negative start clipped to Offset)
//	tailbuf.SliceNominal(buf, 5, 100)  // []       (entirely past end)
func SliceNominal[T any](b *Buf[T], start, end int) []T {
	if end < start {
		panic("tailbuf: end must be >= start")
	}

	// Translate nominal coordinates to tail-relative ones. Clip both
	// bounds against b.offset BEFORE subtracting, so the subtractions
	// can't signed-overflow on inputs like math.MinInt — which would
	// wrap to a large positive and silently violate the documented
	// "below Offset is clipped" contract. b.offset is always >= 0, so
	// after these clips, both differences are guaranteed non-negative
	// and bounded.
	if end <= b.offset {
		// The entire requested range is below the live tail.
		return []T{}
	}
	tailEnd := end - b.offset // end > b.offset >= 0, safe.
	var tailStart int
	if start > b.offset {
		tailStart = start - b.offset // start > b.offset >= 0, safe.
	}
	// (start <= b.offset leaves tailStart at 0, the "below the live
	// range" clip.)
	return SliceTail(b, tailStart, tailEnd)
}

// SliceTail returns a freshly-allocated slice of the items at tail-relative
// half-open positions [start, end). start counts from the oldest live item
// (so position 0 is [Buf.Back] and position [Buf.Len]-1 is [Buf.Front]).
// Positions past the live tail are clipped silently. The returned slice
// never shares storage with the buffer.
//
// Panics if start < 0 or end < start. Out-of-range upper bounds clip
// silently rather than panic; see the "Bounds policy" section of the
// package doc for the rationale and for the contrast with [Buf.Peek].
//
// Note the deliberate asymmetry with [SliceNominal]: tail-relative
// coordinates have no interpretation for start < 0 (nothing below
// position 0 exists), so SliceTail panics; nominal coordinates DO have
// meaning for start values below [Buf.Offset] (they refer to items that
// have already been evicted), so SliceNominal clips instead of panicking.
//
// # Example
//
//	buf := tailbuf.New[int](5).WriteAll(1, 2, 3, 4, 5)
//	tailbuf.SliceTail(buf, 0, 2)   // [1 2]
//	tailbuf.SliceTail(buf, 3, 5)   // [4 5]
//	tailbuf.SliceTail(buf, 4, 10)  // [5]   (clipped)
//	tailbuf.SliceTail(buf, 7, 9)   // []    (entirely past end)
//
// Use [SliceNominal] when working in nominal-index coordinates rather than
// tail-relative ones; it is a thin wrapper over SliceTail.
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
		s[i] = b.window[(b.oldestIdx+start+i)%winLen]
	}
	return s
}
