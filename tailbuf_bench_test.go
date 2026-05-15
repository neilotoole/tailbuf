package tailbuf_test

import (
	"testing"

	"github.com/neilotoole/tailbuf"
)

// Benchmarks in this file establish a baseline for the hot-path methods on
// Buf. Each benchmark is self-contained, preloads the buffer before the
// timer starts (except the zero-capacity case, which has nothing to
// preload), and reports allocations so -benchmem output is automatic.
//
// The standard buffer capacity is 1024, large enough that fixed per-call
// overhead does not dominate the measurement. WriteAll uses a 16-item
// batch as its input — that 16 is a batch size, not a buffer capacity.
// The item type is `int` so the cost of copying an item does not
// dominate; swap for a larger struct to measure copy-dominated workloads.
//
// Read benchmarks store results into a package-level sink variable to
// prevent the Go compiler from eliding the work via escape analysis and
// dead-code elimination. Without that, results like Peek at "0.7 ns/op"
// or a wrapped Tail() reporting "0 allocs" are artifacts of the optimizer,
// not the package's real cost.
var (
	sinkAny   any
	sinkSlice []int
	sinkInt   int
)

// BenchmarkWrite measures the steady-state eviction cost of [Buf.Write] on
// a full buffer — the dominant path for long-running use. The buffer is
// preloaded to capacity before the timer starts so the timed loop never
// touches the cheaper grow-to-full path.
func BenchmarkWrite(b *testing.B) {
	buf := tailbuf.New[int](1024).WriteAll(make([]int, 1024)...)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.Write(i)
	}
}

// BenchmarkWrite_ZeroCap measures the counter-only path: a zero-capacity
// buffer drops the item but still increments [Buf.Written].
func BenchmarkWrite_ZeroCap(b *testing.B) {
	buf := tailbuf.New[int](0)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.Write(i)
	}
}

// BenchmarkWriteAll measures [Buf.WriteAll] with a 16-item batch against a
// 1024-cap buffer already at capacity.
func BenchmarkWriteAll(b *testing.B) {
	batch := make([]int, 16)
	for i := range batch {
		batch[i] = i
	}
	buf := tailbuf.New[int](1024).WriteAll(make([]int, 1024)...)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.WriteAll(batch...)
	}
}

// BenchmarkTail_NoWrap measures [Buf.Tail] when the live items do not
// wrap, so the returned slice aliases the internal window and no
// allocation occurs.
func BenchmarkTail_NoWrap(b *testing.B) {
	buf := tailbuf.New[int](1024).WriteAll(make([]int, 1024)...)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sinkSlice = buf.Tail()
	}
}

// BenchmarkTail_Wrapped measures [Buf.Tail] in the wrap case, which
// materializes a contiguous slice. The sink assignment prevents escape
// analysis from eliding the copy.
func BenchmarkTail_Wrapped(b *testing.B) {
	buf := tailbuf.New[int](1024)
	// Drive the ring so it's physically wrapped: cap + cap/2 writes put
	// back at cap/2, with the live items spanning the physical end of
	// window. Writing exactly 2*cap would land back at 0 again (no wrap).
	for i := 0; i < 1024+512; i++ {
		buf.Write(i)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sinkSlice = buf.Tail()
	}
}

// BenchmarkSliceTail measures [SliceTail], which always allocates.
// Compare against BenchmarkTail_NoWrap to see the aliasing win.
func BenchmarkSliceTail(b *testing.B) {
	buf := tailbuf.New[int](1024).WriteAll(make([]int, 1024)...)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sinkSlice = tailbuf.SliceTail(buf, 0, 1024)
	}
}

// BenchmarkPeek measures the O(1) random-access read path.
func BenchmarkPeek(b *testing.B) {
	buf := tailbuf.New[int](1024).WriteAll(make([]int, 1024)...)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sinkInt = buf.Peek(i & 1023)
	}
}

// BenchmarkApply_vs_TailLoop compares [Buf.Apply] against the
// "range over [Buf.Tail]" pattern.
//
// Caveat — the two pairs measure different things:
//
//   - In the no-wrap case [Buf.Tail] aliases the buffer's internal window,
//     so the TailLoop subtest's element writes DO reach the buffer.
//     This is an apples-to-apples comparison and TailLoop is the faster
//     one (the compiler inlines the loop body and avoids the modular
//     indexing that Apply must perform).
//   - In the wrap case [Buf.Tail] returns a fresh allocation, so the
//     TailLoop subtest's writes go into that throwaway slice and never
//     reach the buffer. The two wrapped subtests therefore measure
//     semantically different operations: Apply mutates the ring; TailLoop
//     allocates, copies, mutates the copy, and discards it. That is
//     exactly the silent-data-loss footgun the godoc on [Buf.Apply] and
//     the "Slice aliasing" section of the package doc warn against.
//
// In short: the no-wrap pair quantifies how much slower Apply's uniform
// modular indexing is than a direct loop; the wrap pair quantifies the
// cost of Tail's allocation but underestimates the cost of writing the
// transformed values back.
func BenchmarkApply_vs_TailLoop(b *testing.B) {
	b.Run("Apply/no-wrap", func(b *testing.B) {
		buf := tailbuf.New[int](1024).WriteAll(make([]int, 1024)...)
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			buf.Apply(func(n int) int { return n + 1 })
		}
	})
	b.Run("TailLoop/no-wrap", func(b *testing.B) {
		buf := tailbuf.New[int](1024).WriteAll(make([]int, 1024)...)
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			t := buf.Tail()
			for j := range t {
				t[j]++
			}
			sinkSlice = t
		}
	})
	b.Run("Apply/wrapped", func(b *testing.B) {
		buf := tailbuf.New[int](1024)
		for i := 0; i < 1024+512; i++ {
			buf.Write(i)
		}
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			buf.Apply(func(n int) int { return n + 1 })
		}
	})
	b.Run("TailLoop/wrapped", func(b *testing.B) {
		buf := tailbuf.New[int](1024)
		for i := 0; i < 1024+512; i++ {
			buf.Write(i)
		}
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			t := buf.Tail()
			for j := range t {
				t[j]++
			}
			sinkSlice = t
		}
	})
}

// BenchmarkPopFront_Refill measures [Buf.PopFront] in a steady-state
// "pop-then-refill" loop. The per-op cost is one PopFront + one Write,
// where the Write lands on a not-yet-full buffer (PopFront just shrank
// len), so this measures Pop + Write-without-eviction, not Pop +
// Write-with-eviction. Halve the reported ns/op only for a rough
// per-primitive estimate; the two halves are not interchangeable with
// the steady-state-evicting Write measured by BenchmarkWrite.
func BenchmarkPopFront_Refill(b *testing.B) {
	buf := tailbuf.New[int](1024).WriteAll(make([]int, 1024)...)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sinkInt = buf.PopFront()
		buf.Write(i)
	}
}

// BenchmarkPopBack_Refill mirrors BenchmarkPopFront_Refill for the back
// end so the two sides can be compared directly. Same composite-op
// caveat applies: this measures Pop + Write-without-eviction.
func BenchmarkPopBack_Refill(b *testing.B) {
	buf := tailbuf.New[int](1024).WriteAll(make([]int, 1024)...)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sinkInt = buf.PopBack()
		buf.Write(i)
	}
}

// BenchmarkDropFront_Refill mirrors BenchmarkPopFront_Refill for the
// no-allocation discard variant. The whole point of DropFront is to
// avoid the value-copy cost of returning the item; this benchmark with
// [b.ReportAllocs] locks in the "0 allocs/op" contract that justifies
// DropFront's existence.
func BenchmarkDropFront_Refill(b *testing.B) {
	buf := tailbuf.New[int](1024).WriteAll(make([]int, 1024)...)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.DropFront()
		buf.Write(i)
	}
}

// BenchmarkDropFrontN_Refill measures the bulk discard. The PopFrontN
// counterpart would allocate a fresh []T on every call to return the
// discarded items; DropFrontN avoids that allocation entirely. Pinning
// the 0-alloc contract with [b.ReportAllocs] is the contract that
// distinguishes DropFrontN from PopFrontN at the type level.
func BenchmarkDropFrontN_Refill(b *testing.B) {
	buf := tailbuf.New[int](1024).WriteAll(make([]int, 1024)...)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.DropFrontN(16)
		buf.WriteAll(make([]int, 16)...)
	}
}

// Keep sinkAny referenced so the linker cannot elide it if a test file
// is the only consumer of the package-level sinks.
var _ = sinkAny
