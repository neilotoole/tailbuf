package tailbuf_test

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/neilotoole/tailbuf"
)

func ExampleBuf() {
	buf := tailbuf.New[string](3)

	buf.WriteAll("a", "b", "c")
	fmt.Println(buf.Tail())

	buf.WriteAll("d", "e", "f", "g")
	fmt.Println(buf.Tail())

	fmt.Println("Written:", buf.Written())

	// Output:
	// [a b c]
	// [e f g]
	// Written: 7
}

func ExampleBuf_Peek() {
	buf := tailbuf.New[string](3)

	buf.WriteAll("a", "b", "c")
	fmt.Println(buf.Peek(0))
	fmt.Println(buf.Peek(1))

	fmt.Println(buf.PopBackN(2))
	fmt.Println(buf.Tail())
	// Output:
	// a
	// b
	// [a b]
	// [c]
}

func ExampleBuf_Len() {
	buf := tailbuf.New[string](3)

	fmt.Println(buf.Cap())
	fmt.Println(buf.Len())
	buf.WriteAll("a", "b", "c")
	fmt.Println(buf.Len())

	buf.WriteAll("d", "e", "f", "g")
	fmt.Println(buf.Len())

	fmt.Println("Written:", buf.Written())
	buf.Reset()
	fmt.Println(buf.Len())
	fmt.Println("Written:", buf.Written())

	buf.WriteAll("h", "i")
	fmt.Println(buf.Len())
	fmt.Println("Written:", buf.Written())

	buf.Clear() // Clear is like Reset, but doesn't reset the written counter
	fmt.Println(buf.Len())
	fmt.Println("Written:", buf.Written())

	// Output:
	// 3
	// 0
	// 3
	// 3
	// Written: 7
	// 0
	// Written: 0
	// 2
	// Written: 2
	// 0
	// Written: 2
}

func ExampleBuf_Apply() {
	buf := tailbuf.New[string](3)
	buf.WriteAll("In", "Xanadu  ", "   did", "Kubla  ", "Khan")
	buf.Apply(strings.ToUpper).Apply(strings.TrimSpace)
	fmt.Println(buf.Tail())

	// Output:
	// [DID KUBLA KHAN]
}

// ExampleBuf_Bounds shows how Bounds tracks the live nominal range as items
// are evicted by writes and removed by pops.
func ExampleBuf_Bounds() {
	buf := tailbuf.New[string](3)
	buf.WriteAll("a", "b", "c")
	start, end := buf.Bounds()
	fmt.Printf("after 3 writes:    bounds=(%d,%d) tail=%v\n", start, end, buf.Tail())

	buf.WriteAll("d", "e") // evicts "a", "b"
	start, end = buf.Bounds()
	fmt.Printf("after 2 evictions: bounds=(%d,%d) tail=%v\n", start, end, buf.Tail())

	buf.PopBack() // removes oldest ("c"); offset advances
	start, end = buf.Bounds()
	fmt.Printf("after PopBack:     bounds=(%d,%d) tail=%v\n", start, end, buf.Tail())

	buf.PopFront() // removes newest ("e"); end shrinks
	start, end = buf.Bounds()
	fmt.Printf("after PopFront:    bounds=(%d,%d) tail=%v\n", start, end, buf.Tail())

	// Output:
	// after 3 writes:    bounds=(0,3) tail=[a b c]
	// after 2 evictions: bounds=(2,5) tail=[c d e]
	// after PopBack:     bounds=(3,5) tail=[d e]
	// after PopFront:    bounds=(3,4) tail=[d]
}

// ExampleBuf_Front shows the relationship between Front, Back, and Tail.
func ExampleBuf_Front() {
	buf := tailbuf.New[int](3)
	buf.WriteAll(10, 20, 30)
	fmt.Println("front:", buf.Front()) // newest
	fmt.Println("back: ", buf.Back())  // oldest

	// Front/Back on an empty buffer return the zero value of T rather than
	// panicking.
	empty := tailbuf.New[int](3)
	fmt.Println("empty front:", empty.Front())
	fmt.Println("empty back: ", empty.Back())

	// Output:
	// front: 30
	// back:  10
	// empty front: 0
	// empty back:  0
}

// ExampleBuf_PopFront shows that PopFront returns the newest live item and
// shrinks the tail from its newest end without changing Offset.
func ExampleBuf_PopFront() {
	buf := tailbuf.New[string](3)
	buf.WriteAll("a", "b", "c")

	fmt.Println("popped:", buf.PopFront()) // returns "c"
	fmt.Println("tail:  ", buf.Tail())
	fmt.Println("offset:", buf.Offset()) // unchanged
	fmt.Println("len:   ", buf.Len())

	// Output:
	// popped: c
	// tail:   [a b]
	// offset: 0
	// len:    2
}

// ExampleBuf_PopBack shows that PopBack returns the oldest live item and
// advances Offset by one.
func ExampleBuf_PopBack() {
	buf := tailbuf.New[string](3)
	buf.WriteAll("a", "b", "c")

	fmt.Println("popped:", buf.PopBack()) // returns "a"
	fmt.Println("tail:  ", buf.Tail())
	fmt.Println("offset:", buf.Offset()) // advanced
	fmt.Println("len:   ", buf.Len())

	// Output:
	// popped: a
	// tail:   [b c]
	// offset: 1
	// len:    2
}

// ExampleBuf_Do shows the index/tailOffset arguments and how to derive a
// nominal index from them.
func ExampleBuf_Do() {
	buf := tailbuf.New[string](3)
	buf.WriteAll("a", "b", "c", "d", "e") // offset becomes 2

	_ = buf.Do(context.Background(),
		func(_ context.Context, item string, index, tailOffset int) (string, error) {
			nominal := index + tailOffset
			return fmt.Sprintf("%d:%s", nominal, item), nil
		})
	fmt.Println(buf.Tail())

	// Output:
	// [2:c 3:d 4:e]
}

// ExampleBuf_Reset_vs_Clear contrasts Reset (which zeroes Written and
// Offset) with Clear (which preserves Written and bumps Offset by Len).
func ExampleBuf_Reset_vs_Clear() {
	demo := func(label string, mutate func(*tailbuf.Buf[string])) {
		buf := tailbuf.New[string](3)
		buf.WriteAll("a", "b", "c", "d", "e")
		mutate(buf)
		buf.Write("z")
		start, end := buf.Bounds()
		fmt.Printf("%s: written=%d bounds=(%d,%d) tail=%v\n",
			label, buf.Written(), start, end, buf.Tail())
	}

	demo("Reset", func(b *tailbuf.Buf[string]) { b.Reset() })
	demo("Clear", func(b *tailbuf.Buf[string]) { b.Clear() })

	// Output:
	// Reset: written=1 bounds=(0,1) tail=[z]
	// Clear: written=6 bounds=(5,6) tail=[z]
}

// ExampleSliceTail shows tail-relative slicing with permissive bounds.
func ExampleSliceTail() {
	buf := tailbuf.New[int](5)
	buf.WriteAll(1, 2, 3, 4, 5)
	fmt.Println(tailbuf.SliceTail(buf, 0, 2))   // first two
	fmt.Println(tailbuf.SliceTail(buf, 3, 5))   // last two
	fmt.Println(tailbuf.SliceTail(buf, 4, 100)) // clipped to end

	// Output:
	// [1 2]
	// [4 5]
	// [5]
}

// ExampleSliceNominal shows nominal-index slicing after eviction has moved
// Offset above 0.
func ExampleSliceNominal() {
	buf := tailbuf.New[int](3)
	buf.WriteAll(1, 2, 3, 4, 5)                  // bounds become (2, 5)
	fmt.Println(tailbuf.SliceNominal(buf, 2, 5)) // entire live tail
	fmt.Println(tailbuf.SliceNominal(buf, 1, 3)) // 1 is evicted; only nominal 2 remains
	fmt.Println(tailbuf.SliceNominal(buf, 5, 9)) // entirely past end

	// Output:
	// [3 4 5]
	// [3]
	// []
}

// ExampleNew_zeroCapacity demonstrates the counter-only mode of a
// zero-capacity buffer.
func ExampleNew_zeroCapacity() {
	buf := tailbuf.New[string](0)
	buf.WriteAll("a", "b", "c")
	fmt.Println("cap:    ", buf.Cap())
	fmt.Println("len:    ", buf.Len())
	fmt.Println("written:", buf.Written())
	fmt.Println("tail:   ", buf.Tail())

	// Output:
	// cap:     0
	// len:     0
	// written: 3
	// tail:    []
}

// ExampleBuf_Back shows the relationship between Back and the oldest item
// in the tail, including the empty-buffer case.
func ExampleBuf_Back() {
	buf := tailbuf.New[int](3)
	buf.WriteAll(10, 20, 30)
	fmt.Println("back: ", buf.Back()) // oldest live item

	// On an empty buffer Back returns the zero value of T rather than
	// panicking.
	empty := tailbuf.New[int](3)
	fmt.Println("empty:", empty.Back())

	// Output:
	// back:  10
	// empty: 0
}

// ExampleBuf_PopFrontN shows that PopFrontN removes the newest n items and
// returns them in oldest-to-newest order — the LAST element of the
// returned slice is the one that was at the front before the call.
func ExampleBuf_PopFrontN() {
	buf := tailbuf.New[string](5)
	buf.WriteAll("a", "b", "c", "d", "e")

	popped := buf.PopFrontN(2) // removes "d" and "e" (the two newest)
	fmt.Println("popped:", popped)
	fmt.Println("tail:  ", buf.Tail())
	fmt.Println("offset:", buf.Offset()) // PopFrontN does NOT advance Offset

	// Output:
	// popped: [d e]
	// tail:   [a b c]
	// offset: 0
}

// ExampleBuf_PopBackN shows that PopBackN removes the oldest n items in
// oldest-to-newest order and advances Offset by the number removed.
func ExampleBuf_PopBackN() {
	buf := tailbuf.New[string](5)
	buf.WriteAll("a", "b", "c", "d", "e")

	popped := buf.PopBackN(2) // removes "a" and "b" (the two oldest)
	fmt.Println("popped:", popped)
	fmt.Println("tail:  ", buf.Tail())
	fmt.Println("offset:", buf.Offset()) // advanced by 2

	// Output:
	// popped: [a b]
	// tail:   [c d e]
	// offset: 2
}

func TestTail(t *testing.T) {
	buf := tailbuf.New[rune](3)
	gotLen := buf.Len()
	require.Equal(t, 0, gotLen)
	require.Equal(t, 0, buf.Written())
	require.Empty(t, buf.Tail())
	require.Empty(t, tailbuf.TailNewSlice(buf))

	buf.Write('a')
	require.Equal(t, 1, buf.Written())
	gotLen = buf.Len()
	require.Equal(t, 1, gotLen)
	gotTail := buf.Tail()
	require.Equal(t, []rune{'a'}, gotTail)
	require.Equal(t, gotTail, tailbuf.TailNewSlice(buf))

	buf.Write('b')
	require.Equal(t, 2, buf.Written())
	gotLen = buf.Len()
	require.Equal(t, 2, gotLen)
	gotTail = buf.Tail()
	require.Equal(t, []rune{'a', 'b'}, gotTail)
	require.Equal(t, gotTail, tailbuf.TailNewSlice(buf))

	buf.Write('c')
	require.Equal(t, 3, buf.Written())
	gotLen = buf.Len()
	require.Equal(t, 3, gotLen)
	gotTail = buf.Tail()
	require.Equal(t, []rune{'a', 'b', 'c'}, gotTail)
	require.Equal(t, gotTail, tailbuf.TailNewSlice(buf))

	buf.Write('d')
	require.Equal(t, 4, buf.Written())
	gotLen = buf.Len()
	require.Equal(t, 3, gotLen)
	gotTail = buf.Tail()
	require.Equal(t, []rune{'b', 'c', 'd'}, gotTail)
	require.Equal(t, gotTail, tailbuf.TailNewSlice(buf))

	buf.Write('e')
	require.Equal(t, 5, buf.Written())
	gotLen = buf.Len()
	require.Equal(t, 3, gotLen)
	gotTail = buf.Tail()
	require.Equal(t, []rune{'c', 'd', 'e'}, gotTail)
	require.Equal(t, gotTail, tailbuf.TailNewSlice(buf))

	buf.Write('f')
	require.Equal(t, 6, buf.Written())
	gotLen = buf.Len()
	require.Equal(t, 3, gotLen)
	gotTail = buf.Tail()
	require.Equal(t, []rune{'d', 'e', 'f'}, gotTail)
	require.Equal(t, gotTail, tailbuf.TailNewSlice(buf))

	buf.Write('g')
	require.Equal(t, 7, buf.Written())
	gotLen = buf.Len()
	require.Equal(t, 3, gotLen)
	gotTail = buf.Tail()
	require.Equal(t, []rune{'e', 'f', 'g'}, gotTail)
	require.Equal(t, gotTail, tailbuf.TailNewSlice(buf))

	buf.WriteAll('h', 'i', 'j')
	require.Equal(t, 10, buf.Written())
	gotLen = buf.Len()
	require.Equal(t, 3, gotLen)
	gotTail = buf.Tail()
	require.Equal(t, []rune{'h', 'i', 'j'}, gotTail)
	require.Equal(t, gotTail, tailbuf.TailNewSlice(buf))
}

func TestBuf(t *testing.T) {
	testCases := []struct {
		wantWindow         []rune
		add                rune
		wantStart, wantEnd int
	}{
		{add: 'a', wantStart: 0, wantEnd: 1, wantWindow: []rune{'a'}},
		{add: 'b', wantStart: 0, wantEnd: 2, wantWindow: []rune{'a', 'b'}},
		{add: 'c', wantStart: 0, wantEnd: 3, wantWindow: []rune{'a', 'b', 'c'}},
		{add: 'd', wantStart: 1, wantEnd: 4, wantWindow: []rune{'b', 'c', 'd'}},
		{add: 'e', wantStart: 2, wantEnd: 5, wantWindow: []rune{'c', 'd', 'e'}},
		{add: 'f', wantStart: 3, wantEnd: 6, wantWindow: []rune{'d', 'e', 'f'}},
		{add: 'g', wantStart: 4, wantEnd: 7, wantWindow: []rune{'e', 'f', 'g'}},
		{add: 'h', wantStart: 5, wantEnd: 8, wantWindow: []rune{'f', 'g', 'h'}},
	}

	buf := tailbuf.New[rune](3)

	for i, tc := range testCases {
		tc := tc
		t.Run(fmt.Sprintf("%d_%s", i, string(tc.add)), func(t *testing.T) {
			buf.Write(tc.add)
			require.Equal(t, tc.wantEnd, buf.Written())
			require.Equal(t, tc.add, buf.Front())
			window := buf.Tail()
			require.Equal(t, tc.wantWindow, window)
			start, end := buf.Bounds()
			require.Equal(t, tc.wantStart, start)
			require.Equal(t, tc.wantEnd, end)
			s := tailbuf.SliceNominal(buf, start, end+1)
			require.Equal(t, window, s)
		})
	}
}

func TestBounds(t *testing.T) {
	buf := tailbuf.New[string](3)
	start, end := buf.Bounds()
	require.Equal(t, 0, start)
	require.Equal(t, 0, end)

	require.False(t, buf.InBounds(-1))
	require.False(t, buf.InBounds(0))
	require.False(t, buf.InBounds(1))

	buf.WriteAll("a", "b", "c")
	start, end = buf.Bounds()
	require.Equal(t, 0, start)
	require.Equal(t, 3, end)
	require.True(t, buf.InBounds(0))
	require.True(t, buf.InBounds(1))
	require.True(t, buf.InBounds(2))
	require.False(t, buf.InBounds(3))

	buf.WriteAll("d", "e")
	start, end = buf.Bounds()
	require.Equal(t, 2, start)
	require.Equal(t, 5, end)
	require.False(t, buf.InBounds(0))
	require.False(t, buf.InBounds(1))
	for i := 2; i < 5; i++ {
		require.True(t, buf.InBounds(i))
	}
	require.False(t, buf.InBounds(5))
}

func TestSlice(t *testing.T) {
	buf := tailbuf.New[int](3)
	buf.WriteAll(0, 1, 2)

	start, end := buf.Bounds()
	require.Equal(t, 0, start)
	require.Equal(t, 3, end)
	s := tailbuf.SliceNominal(buf, start, end)
	require.Equal(t, []int{0, 1, 2}, s)

	s = tailbuf.SliceNominal(buf, 0, 0)
	require.Empty(t, s)

	s = tailbuf.SliceNominal(buf, 0, 1)
	require.Equal(t, []int{0}, s)
	s = tailbuf.SliceNominal(buf, 0, 2)
	require.Equal(t, []int{0, 1}, s)
	s = tailbuf.SliceNominal(buf, 0, 3)
	require.Equal(t, []int{0, 1, 2}, s)

	s = tailbuf.SliceNominal(buf, 1, 1)
	require.Empty(t, s)
	s = tailbuf.SliceNominal(buf, 1, 3)
	require.Equal(t, []int{1, 2}, s)

	buf.WriteAll(3, 4, 5)
	start, end = buf.Bounds()
	require.Equal(t, 3, start)
	require.Equal(t, 6, end)
	s = tailbuf.SliceNominal(buf, start, end)
	require.Equal(t, []int{3, 4, 5}, s)

	s = tailbuf.SliceNominal(buf, 3, 3)
	require.Empty(t, s)
	s = tailbuf.SliceNominal(buf, 3, 4)
	require.Equal(t, []int{3}, s)
	s = tailbuf.SliceNominal(buf, 3, 5)
	require.Equal(t, []int{3, 4}, s)

	buf.WriteAll(6, 7)
	s = tailbuf.SliceNominal(buf, 6, 7)
	require.Equal(t, []int{6}, s)
}

func TestApply_Do(t *testing.T) {
	buf := tailbuf.New[string](3)
	buf.WriteAll("In", "Xanadu  ", "   did", "Kubla  ", "Khan")
	buf.Apply(strings.ToUpper).Apply(strings.TrimSpace)
	got := buf.Tail()
	require.Equal(t, []string{"DID", "KUBLA", "KHAN"}, got)

	err := buf.Do(context.Background(), func(_ context.Context, item string, _, _ int) (string, error) {
		return strings.ToLower(item), nil
	})
	require.NoError(t, err)
	got = buf.Tail()
	require.Equal(t, []string{"did", "kubla", "khan"}, got)
}

func TestPeek(t *testing.T) {
	buf := tailbuf.New[int](3)

	require.Panics(t, func() {
		_ = buf.Peek(0) // panics on empty buffer
	})

	buf.WriteAll(0, 1, 2)

	got := buf.Peek(0)
	require.Equal(t, 0, got)
	got = buf.Peek(1)
	require.Equal(t, 1, got)
	got = buf.Peek(2)
	require.Equal(t, 2, got)

	require.Panics(t, func() {
		_ = buf.Peek(-1)
	})

	require.Panics(t, func() {
		_ = buf.Peek(3)
	})
}

func TestTailSlice(t *testing.T) {
	buf := tailbuf.New[int](10).WriteAll(1, 2, 3, 4, 5)
	a := buf.Tail()[0:2]
	b := tailbuf.SliceTail(buf, 0, 2)
	require.Equal(t, []int{1, 2}, b)
	require.Equal(t, a, b)
}

func TestTail_Slice_Equivalence(t *testing.T) {
	buf := tailbuf.New[int](10).WriteAll(1, 2, 3, 4, 5)
	a := buf.Tail()[0:2]
	b := tailbuf.SliceNominal(buf, 0, 2)
	require.Equal(t, []int{1, 2}, b)
	require.Equal(t, a, b)
}

func TestWrittenGTCapacity(t *testing.T) {
	buf := tailbuf.New[string](1)
	buf.WriteAll("a", "b")
	require.Equal(t, 1, buf.Cap())
	require.Equal(t, 2, buf.Written())
	tail := buf.Tail()
	require.Equal(t, []string{"b"}, tail)
	tailSlice := tailbuf.SliceTail(buf, 0, 1)
	require.Equal(t, []string{"b"}, tailSlice)
	nomSlice := tailbuf.SliceNominal(buf, 0, 2)
	require.Equal(t, []string{"b"}, nomSlice)
	nomSlice = tailbuf.SliceNominal(buf, 0, 1)
	require.Empty(t, nomSlice)
}

func TestZeroCapacity(t *testing.T) {
	buf := tailbuf.New[rune](0)
	require.Equal(t, 0, buf.Cap())
	require.Equal(t, 0, buf.Written())
	require.Equal(t, 0, buf.Len())
	require.Empty(t, buf.Tail())

	buf.Write('a')

	require.Equal(t, 1, buf.Written())
	gotLen := buf.Len()
	require.Equal(t, 0, gotLen)
	require.Empty(t, buf.Tail())
	require.Empty(t, tailbuf.SliceNominal(buf, 0, 1))
}

func TestPopFront(t *testing.T) {
	buf := tailbuf.New[rune](3)
	buf.WriteAll('a', 'b', 'c')
	require.Equal(t, 3, buf.Written())
	require.Equal(t, 3, buf.Len())
	require.Equal(t, 'c', buf.Front())
	require.Equal(t, 'a', buf.Back())
	require.Equal(t, []rune{'a', 'b', 'c'}, buf.Tail())

	got := buf.PopFront()
	require.Equal(t, 'c', got)
	require.Equal(t, 3, buf.Written())
	require.Equal(t, 2, buf.Len())
	require.Equal(t, 'b', buf.Front())
	require.Equal(t, []rune{'a', 'b', 0}, tailbuf.InternalWindow(buf))
	require.Equal(t, []rune{'a', 'b'}, buf.Tail())

	got = buf.PopFront()
	require.Equal(t, 'b', got)
	require.Equal(t, 3, buf.Written())
	require.Equal(t, 1, buf.Len())
	require.Equal(t, 'a', buf.Front())
	require.Equal(t, []rune{'a', 0, 0}, tailbuf.InternalWindow(buf))
	require.Equal(t, []rune{'a'}, buf.Tail())

	got = buf.PopFront()
	require.Equal(t, 'a', got)
	require.Equal(t, 3, buf.Written())
	require.Empty(t, buf.Front())
	requireZeroInternalWindow(t, buf)
	require.Equal(t, 0, buf.Len())
	require.Equal(t, []rune{}, buf.Tail())

	got = buf.PopFront()
	require.Zero(t, got)
	require.Equal(t, 3, buf.Written())
	require.Equal(t, 0, buf.Len())
	require.Empty(t, buf.Front())
	requireZeroInternalWindow(t, buf)
	require.Equal(t, []rune{}, buf.Tail())
}

func TestPopBack(t *testing.T) {
	buf := tailbuf.New[rune](3)
	buf.WriteAll('a', 'b', 'c')
	require.Equal(t, 3, buf.Written())
	require.Equal(t, 3, buf.Len())
	require.Equal(t, 'c', buf.Front())
	require.Equal(t, 'a', buf.Back())
	require.Equal(t, []rune{'a', 'b', 'c'}, tailbuf.InternalWindow(buf))
	require.Equal(t, []rune{'a', 'b', 'c'}, buf.Tail())

	got := buf.PopBack()
	require.Equal(t, 'a', got)
	require.Equal(t, 3, buf.Written())
	require.Equal(t, 2, buf.Len())
	require.Equal(t, 'b', buf.Back())
	require.Equal(t, []rune{0, 'b', 'c'}, tailbuf.InternalWindow(buf))
	require.Equal(t, []rune{'b', 'c'}, buf.Tail())

	got = buf.PopBack()
	require.Equal(t, 'b', got)
	require.Equal(t, 3, buf.Written())
	require.Equal(t, 1, buf.Len())
	require.Equal(t, 'c', buf.Back())
	require.Equal(t, []rune{0, 0, 'c'}, tailbuf.InternalWindow(buf))

	got = buf.PopBack()
	require.Equal(t, 'c', got)
	require.Equal(t, 3, buf.Written())
	require.Equal(t, 0, buf.Len())
	require.Empty(t, buf.Back())
	requireZeroInternalWindow(t, buf)

	got = buf.PopBack()
	require.Zero(t, got)
	require.Equal(t, 3, buf.Written())
	require.Equal(t, 0, buf.Len())
	require.Empty(t, buf.Back())
	requireZeroInternalWindow(t, buf)
}

func TestDropBack(t *testing.T) {
	buf := tailbuf.New[rune](3)
	buf.WriteAll('a', 'b', 'c')
	require.Equal(t, 3, buf.Written())
	require.Equal(t, 3, buf.Len())
	require.Equal(t, 'a', buf.Back())
	require.Equal(t, []rune{'a', 'b', 'c'}, buf.Tail())

	buf.DropBack()
	require.Equal(t, 3, buf.Written())
	require.Equal(t, 2, buf.Len())
	require.Equal(t, 'b', buf.Back())
	require.Equal(t, []rune{0, 'b', 'c'}, tailbuf.InternalWindow(buf))
	require.Equal(t, []rune{'b', 'c'}, buf.Tail())

	buf.DropBack()
	require.Equal(t, 3, buf.Written())
	require.Equal(t, 1, buf.Len())
	require.Equal(t, 'c', buf.Back())
	require.Equal(t, []rune{0, 0, 'c'}, tailbuf.InternalWindow(buf))
	require.Equal(t, []rune{'c'}, buf.Tail())

	buf.DropBack()
	require.Equal(t, 3, buf.Written())
	require.Equal(t, 0, buf.Len())
	require.Empty(t, buf.Back())
	requireZeroInternalWindow(t, buf)
	require.Empty(t, buf.Tail())

	buf.DropBack()
	require.Equal(t, 3, buf.Written())
	require.Equal(t, 0, buf.Len())
	require.Empty(t, buf.Back())
	requireZeroInternalWindow(t, buf)
	require.Empty(t, buf.Tail())
}

func TestPopBackN(t *testing.T) {
	all := []rune{'a', 'b', 'c', 'd', 'e', 'f', 'g', 'h', 'i', 'j'}
	buf := tailbuf.New[rune](10)
	buf.WriteAll(all...)
	require.Equal(t, 10, buf.Len())
	require.Equal(t, 10, buf.Written())
	require.Equal(t, all, buf.Tail())

	got := buf.PopBackN(0)
	require.Empty(t, got)
	require.Equal(t, 10, buf.Len())
	require.Equal(t, 10, buf.Written())
	require.Equal(t, all, buf.Tail())

	got = buf.PopBackN(1)
	require.Equal(t, []rune{'a'}, got)
	require.Equal(t, 9, buf.Len())
	require.Equal(t, 10, buf.Written())
	window := tailbuf.InternalWindow(buf)
	require.Equal(t, []rune{0, 'b', 'c', 'd', 'e', 'f', 'g', 'h', 'i', 'j'}, window)
	gotTail := buf.Tail()
	require.Equal(t, all[1:], gotTail)

	got = buf.PopBackN(3)
	require.Equal(t, []rune{'b', 'c', 'd'}, got)
	require.Equal(t, 6, buf.Len())
	require.Equal(t, 10, buf.Written())
	gotTail = buf.Tail()
	require.Equal(t, all[4:], gotTail)

	got = buf.PopBackN(10)
	require.Equal(t, []rune{'e', 'f', 'g', 'h', 'i', 'j'}, got)
	require.Equal(t, 0, buf.Len())
	require.Equal(t, 10, buf.Written())
	require.Empty(t, buf.Tail())
}

func TestPopFrontN(t *testing.T) {
	all := []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j"}
	buf := tailbuf.New[string](10)
	buf.WriteAll(all...)
	require.Equal(t, 10, buf.Len())
	require.Equal(t, 10, buf.Written())
	require.Equal(t, all, buf.Tail())

	got := buf.PopFrontN(0)
	require.Empty(t, got)
	require.Equal(t, 10, buf.Len())
	require.Equal(t, 10, buf.Written())
	require.Equal(t, all, buf.Tail())

	got = buf.PopFrontN(1)
	require.Equal(t, []string{"j"}, got)
	require.Equal(t, 9, buf.Len())
	require.Equal(t, 10, buf.Written())
	window := tailbuf.InternalWindow(buf)
	require.Equal(t, []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", ""}, window)
	gotTail := buf.Tail()
	require.Equal(t, []string{"a", "b", "c", "d", "e", "f", "g", "h", "i"}, gotTail)

	got = buf.PopFrontN(2)
	require.Equal(t, []string{"h", "i"}, got)
	require.Equal(t, 7, buf.Len())
	require.Equal(t, 10, buf.Written())
	gotTail = buf.Tail()
	require.Equal(t, []string{"a", "b", "c", "d", "e", "f", "g"}, gotTail)

	got = buf.PopFrontN(10)
	require.Equal(t, []string{"a", "b", "c", "d", "e", "f", "g"}, got)
	require.Equal(t, 0, buf.Len())
	require.Equal(t, 10, buf.Written())
	gotTail = buf.Tail()
	require.Empty(t, gotTail)
}

func TestLen(t *testing.T) {
	all := []string{"a", "b", "c"}
	buf := tailbuf.New[string](3)
	require.Equal(t, 0, buf.Len())
	buf.Write("a")
	require.Equal(t, 1, buf.Len())
	buf.Write("b")
	require.Equal(t, 2, buf.Len())
	buf.Write("c")
	require.Equal(t, 3, buf.Len())
	buf.Clear()
	require.Equal(t, 0, buf.Len())
	buf.WriteAll(all...)
	require.Equal(t, 3, buf.Len())
}

func TestDropBackN(t *testing.T) {
	all := []rune{'a', 'b', 'c', 'd', 'e', 'f', 'g', 'h', 'i', 'j'}
	buf := tailbuf.New[rune](10)
	buf.WriteAll(all...)
	require.Equal(t, 10, buf.Len())
	require.Equal(t, 10, buf.Written())
	require.Equal(t, all, buf.Tail())

	buf.DropBackN(0)
	require.Equal(t, 10, buf.Len())
	require.Equal(t, 10, buf.Written())
	require.Equal(t, all, buf.Tail())

	buf.DropBackN(1)
	require.Equal(t, 9, buf.Len())
	require.Equal(t, 10, buf.Written())
	window := tailbuf.InternalWindow(buf)
	require.Equal(t, []rune{0, 'b', 'c', 'd', 'e', 'f', 'g', 'h', 'i', 'j'}, window)
	gotTail := buf.Tail()
	require.Equal(t, all[1:], gotTail)

	buf.DropBackN(3)
	require.Equal(t, 6, buf.Len())
	require.Equal(t, 10, buf.Written())
	gotTail = buf.Tail()
	require.Equal(t, all[4:], gotTail)

	buf.DropBackN(10)
	require.Equal(t, 0, buf.Len())
	require.Equal(t, 10, buf.Written())
	require.Empty(t, buf.Tail())
}

func TestPopBack_PopBackN_Equivalence(t *testing.T) {
	all := []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j"}
	buf1 := tailbuf.New[string](10)
	buf2 := tailbuf.New[string](10)

	tailbuf.RequireEqualInternalState(t, buf1, buf2)

	buf1.WriteAll(all...)
	buf2.WriteAll(all...)

	tailbuf.RequireEqualInternalState(t, buf1, buf2)
	tail1 := buf1.Tail()
	tail2 := buf2.Tail()

	require.Equal(t, tail1, tail2)

	buf1.PopBackN(5)
	for i := 0; i < 5; i++ {
		buf2.PopBack()
	}

	tailbuf.RequireEqualInternalState(t, buf1, buf2)
	require.Equal(t, tail1, tail2)

	require.Equal(t, buf1.Tail(), buf2.Tail())
}

func requireZeroInternalWindow[T any](tb testing.TB, buf *tailbuf.Buf[T]) {
	tb.Helper()
	window := tailbuf.InternalWindow(buf)
	for i := range window {
		require.Zero(tb, window[i])
	}
}

// The Bug-* tests below are regression tests for issues identified during
// the initial code review. Each test references the bug label used in the
// review notes and the function-level doc comments in tailbuf.go.

// TestBugA1_ApplyOverIteration covers the case where the tail has a single
// item at a non-zero physical position. The pre-fix Apply iterated over the
// dead positions of window and applied fn to the live item twice; this test
// uses a non-idempotent fn so that any over-iteration shows up in the
// result.
func TestBugA1_ApplyOverIteration(t *testing.T) {
	t.Run("len=1_after_pops_at_non_zero_index", func(t *testing.T) {
		buf := tailbuf.New[string](3)
		buf.WriteAll("a", "b", "c", "d") // wrap: window=[d,b,c], oldestIdx=1
		buf.PopFront()                   // remove d, len=2
		buf.PopFront()                   // remove c, len=1, single item 'b' at window[1]

		calls := 0
		buf.Apply(func(s string) string {
			calls++
			return s + "!"
		})
		require.Equal(t, 1, calls, "fn must run exactly once when Len==1")
		require.Equal(t, []string{"b!"}, buf.Tail())
	})

	t.Run("len=1_at_index_zero", func(t *testing.T) {
		buf := tailbuf.New[string](3)
		buf.WriteAll("a") // oldestIdx=0, len=1, single item at window[0]

		calls := 0
		buf.Apply(func(s string) string {
			calls++
			return s + "!"
		})
		require.Equal(t, 1, calls)
		require.Equal(t, []string{"a!"}, buf.Tail())
	})

	t.Run("len=2_wrapped_calls_each_once", func(t *testing.T) {
		// Sanity check: the multi-item wrap case still works correctly.
		buf := tailbuf.New[int](3)
		buf.WriteAll(1, 2, 3, 4) // window=[4,2,3], oldestIdx=1
		buf.PopFront()           // remove 4, len=2

		calls := 0
		buf.Apply(func(n int) int {
			calls++
			return n * 10
		})
		require.Equal(t, 2, calls)
		require.Equal(t, []int{20, 30}, buf.Tail())
	})
}

// TestBugA1_DoArguments covers the historical mismatch between the Do
// godoc and its implementation. The godoc says fn receives
// (item, tailRelativeIndex, tailOffset), but the previous code passed
// (item, physicalIndex, tailRelativeIndex). The fix makes the values match
// the documented contract.
func TestBugA1_DoArguments(t *testing.T) {
	buf := tailbuf.New[int](3)
	buf.WriteAll(10, 20, 30, 40) // window=[40,20,30], oldestIdx=1, offset=1

	type call struct {
		item, index, tailOffset int
	}
	var calls []call
	err := buf.Do(context.Background(), func(_ context.Context, item, index, tailOffset int) (int, error) {
		calls = append(calls, call{item, index, tailOffset})
		return item, nil
	})
	require.NoError(t, err)
	require.Equal(t, []call{
		{item: 20, index: 0, tailOffset: 1},
		{item: 30, index: 1, tailOffset: 1},
		{item: 40, index: 2, tailOffset: 1},
	}, calls)
}

// TestBugA2_SliceTailAfterPopBack covers the case where the live items do
// not wrap but b.oldestIdx > 0. The pre-fix SliceTail indexed
// window[start:end] directly, which silently returned items from before
// the live region.
func TestBugA2_SliceTailAfterPopBack(t *testing.T) {
	buf := tailbuf.New[int](5)
	buf.WriteAll(1, 2, 3) // oldestIdx=0, len=3
	buf.PopBack()         // oldestIdx=1, len=2, tail=[2,3]

	require.Equal(t, []int{2, 3}, buf.Tail())
	require.Equal(t, []int{2, 3}, tailbuf.SliceTail(buf, 0, 2))
	require.Equal(t, []int{2}, tailbuf.SliceTail(buf, 0, 1))
	require.Equal(t, []int{3}, tailbuf.SliceTail(buf, 1, 2))
}

// TestBugA3_SliceTailSingleItemElsewhere covers the case where the only
// live item is not at window[0]. The pre-fix code special-cased this by
// returning window[0] regardless of where the item actually lived.
func TestBugA3_SliceTailSingleItemElsewhere(t *testing.T) {
	t.Run("after_pops_from_wrapped_state", func(t *testing.T) {
		buf := tailbuf.New[string](3)
		buf.WriteAll("a", "b", "c", "d") // window=[d,b,c]
		buf.PopFront()                   // remove d, len=2
		buf.PopFront()                   // remove c, len=1, item 'b' at window[1]

		require.Equal(t, []string{"b"}, buf.Tail())
		require.Equal(t, []string{"b"}, tailbuf.SliceTail(buf, 0, 1))
	})

	t.Run("after_popback_to_single", func(t *testing.T) {
		buf := tailbuf.New[string](3)
		buf.WriteAll("a", "b", "c") // window=[a,b,c]
		buf.PopBack()               // remove a, item 'c' at window[2]
		buf.PopBack()               // remove b, single item 'c' at window[2]

		require.Equal(t, []string{"c"}, buf.Tail())
		require.Equal(t, []string{"c"}, tailbuf.SliceTail(buf, 0, 1))
	})
}

// TestBugA4_SliceOutOfRange covers the previously-undefined behavior of
// passing nominal or tail-relative indices that fall past the live tail
// against a wrapped buffer. The functions now return an empty slice
// rather than panicking.
func TestBugA4_SliceOutOfRange(t *testing.T) {
	buf := tailbuf.New[int](3)
	buf.WriteAll(1, 2, 3, 4, 5) // window=[4,5,3], offset=2, len=3, written=5

	t.Run("SliceTail_past_end", func(t *testing.T) {
		require.Empty(t, tailbuf.SliceTail(buf, 4, 5))
		require.Empty(t, tailbuf.SliceTail(buf, 3, 3))
		require.Empty(t, tailbuf.SliceTail(buf, 100, 200))
	})

	t.Run("SliceNominal_past_end", func(t *testing.T) {
		require.Empty(t, tailbuf.SliceNominal(buf, 5, 6))
		require.Empty(t, tailbuf.SliceNominal(buf, 100, 200))
	})

	t.Run("SliceNominal_overlapping_end", func(t *testing.T) {
		// The valid nominal range is [2,5); asking for [4,7) should clip
		// to [4,5) and return only the last live item.
		require.Equal(t, []int{5}, tailbuf.SliceNominal(buf, 4, 7))
	})

	t.Run("SliceNominal_before_start", func(t *testing.T) {
		// Asking for nominals entirely below offset returns empty.
		require.Empty(t, tailbuf.SliceNominal(buf, 0, 2))
	})

	t.Run("SliceTail_panics_on_negative_start", func(t *testing.T) {
		require.Panics(t, func() { _ = tailbuf.SliceTail(buf, -1, 1) })
	})

	t.Run("SliceTail_panics_on_inverted_range", func(t *testing.T) {
		require.Panics(t, func() { _ = tailbuf.SliceTail(buf, 2, 1) })
	})

	t.Run("SliceNominal_panics_on_inverted_range", func(t *testing.T) {
		require.Panics(t, func() { _ = tailbuf.SliceNominal(buf, 5, 4) })
	})
}

// TestBugA5_WriteAfterPopFront covers the state-corruption case where the
// pre-fix write predicate (b.written > cap) caused an unwarranted eviction
// after a PopFront freed space in the tail. The new predicate (b.len ==
// cap) only evicts when the tail is actually full.
func TestBugA5_WriteAfterPopFront(t *testing.T) {
	buf := tailbuf.New[string](3)
	buf.WriteAll("a", "b", "c") // tail=[a,b,c], len=3, written=3
	buf.PopFront()              // remove c, tail=[a,b], len=2, written=3
	require.Equal(t, []string{"a", "b"}, buf.Tail())

	// With the pre-fix bug, this Write would evict 'a' (because written
	// becomes 4 > cap), leaving Len()==3 but Tail()==[b,d].
	buf.Write("d")

	require.Equal(t, 3, buf.Len())
	require.Equal(t, []string{"a", "b", "d"}, buf.Tail())
	require.Equal(t, 4, buf.Written())

	// And one more write should now correctly evict the actual oldest, 'a'.
	buf.Write("e")
	require.Equal(t, []string{"b", "d", "e"}, buf.Tail())
	require.Equal(t, 5, buf.Written())
}

// TestBugA6_BoundsAfterMutations covers the cases where the pre-fix
// Bounds/Offset/InBounds returned ranges that included indices no longer
// (or never) live. The new versions track offset explicitly via Pop/Clear.
func TestBugA6_BoundsAfterMutations(t *testing.T) {
	t.Run("after_PopBack_advances_offset", func(t *testing.T) {
		buf := tailbuf.New[string](3)
		buf.WriteAll("a", "b", "c")
		buf.PopBack() // 'a' is gone; offset advances by 1

		start, end := buf.Bounds()
		require.Equal(t, 1, start)
		require.Equal(t, 3, end)
		require.Equal(t, 1, buf.Offset())
		require.False(t, buf.InBounds(0))
		require.True(t, buf.InBounds(1))
		require.True(t, buf.InBounds(2))
		require.False(t, buf.InBounds(3))
	})

	t.Run("after_PopFront_shrinks_end", func(t *testing.T) {
		buf := tailbuf.New[string](3)
		buf.WriteAll("a", "b", "c")
		buf.PopFront() // 'c' is gone; offset unchanged, end shrinks

		start, end := buf.Bounds()
		require.Equal(t, 0, start)
		require.Equal(t, 2, end)
		require.True(t, buf.InBounds(0))
		require.True(t, buf.InBounds(1))
		require.False(t, buf.InBounds(2))
	})

	t.Run("after_Clear_bounds_are_empty_at_next_write_pos", func(t *testing.T) {
		buf := tailbuf.New[string](3)
		buf.WriteAll("a", "b", "c", "d", "e") // offset=2, written=5
		buf.Clear()

		start, end := buf.Bounds()
		require.Equal(t, 5, start)
		require.Equal(t, 5, end)
		require.False(t, buf.InBounds(0))
		require.False(t, buf.InBounds(2))
		require.False(t, buf.InBounds(4))
		require.False(t, buf.InBounds(5))

		// The next write lives at the new offset.
		buf.Write("f")
		require.True(t, buf.InBounds(5))
		require.Equal(t, []string{"f"}, buf.Tail())
	})

	t.Run("after_DropBackN_advances_offset_by_n", func(t *testing.T) {
		buf := tailbuf.New[int](5)
		buf.WriteAll(1, 2, 3, 4, 5)
		buf.DropBackN(2) // remove 1, 2

		start, end := buf.Bounds()
		require.Equal(t, 2, start)
		require.Equal(t, 5, end)
		require.False(t, buf.InBounds(1))
		require.True(t, buf.InBounds(2))
	})

	t.Run("InBounds_false_when_empty", func(t *testing.T) {
		buf := tailbuf.New[int](3)
		require.False(t, buf.InBounds(0))
	})
}

// TestBugA7_ZeroValueBuf covers the case where calls on a zero-value Buf
// (i.e. var buf tailbuf.Buf[T]) panicked in the prior implementation
// because the empty-state sentinel was stored as front==-1 in New, but the
// zero value defaults front to 0 and indexes into a nil window. The new
// implementation uses len==0 as the empty check, so the zero value is
// genuinely usable as an empty zero-capacity buffer.
func TestBugA7_ZeroValueBuf(t *testing.T) {
	var buf tailbuf.Buf[string]

	require.Equal(t, 0, buf.Cap())
	require.Equal(t, 0, buf.Len())
	require.Equal(t, 0, buf.Written())
	require.Equal(t, 0, buf.Offset())
	require.Empty(t, buf.Tail())
	require.Empty(t, buf.Front())
	require.Empty(t, buf.Back())
	require.Empty(t, buf.PopFront())
	require.Empty(t, buf.PopBack())
	require.Empty(t, buf.PopFrontN(3))
	require.Empty(t, buf.PopBackN(3))
	require.Empty(t, tailbuf.SliceTail(&buf, 0, 5))
	require.Empty(t, tailbuf.SliceNominal(&buf, 0, 5))

	buf.DropBack()
	buf.DropBackN(3)
	require.Equal(t, 0, buf.Len())

	// Writes to a zero-cap buffer are silently dropped but still counted.
	buf.Write("x").WriteAll("y", "z")
	require.Equal(t, 0, buf.Len())
	require.Equal(t, 3, buf.Written())
}

// TestBugA8_NewPanicMessage verifies the panic message for a negative
// capacity no longer contains the development FIXME string.
func TestBugA8_NewPanicMessage(t *testing.T) {
	defer func() {
		r := recover()
		require.NotNil(t, r)
		msg, ok := r.(string)
		require.True(t, ok)
		require.NotContains(t, msg, "FIXME")
	}()
	_ = tailbuf.New[int](-1)
}

// TestPopFrontWriteReuseNominalIndex documents (and pins) the model
// described in the package doc: after PopFront, the next Write occupies
// the nominal index that the popped item had. This is a behavioral
// contract worth a test so it doesn't drift unintentionally.
func TestPopFrontWriteReuseNominalIndex(t *testing.T) {
	buf := tailbuf.New[string](3)
	buf.WriteAll("a", "b", "c") // tail=[a,b,c], offset=0, len=3
	require.Equal(t, "c", buf.Front())
	require.Equal(t, 2, buf.Offset()+buf.Len()-1) // c at nominal 2

	buf.PopFront()  // tail=[a,b], offset=0, len=2
	buf.Write("c2") // tail=[a,b,c2], offset=0, len=3; c2 at nominal 2
	require.Equal(t, "c2", buf.Front())
	require.Equal(t, 2, buf.Offset()+buf.Len()-1)
	start, end := buf.Bounds()
	require.Equal(t, 0, start)
	require.Equal(t, 3, end)
}

// TestSliceTail_AfterClearAndRefill verifies that the Slice* helpers
// continue to work after Clear has bumped offset.
func TestSliceTail_AfterClearAndRefill(t *testing.T) {
	buf := tailbuf.New[int](3)
	buf.WriteAll(1, 2, 3, 4, 5) // offset=2
	buf.Clear()                 // offset=5
	buf.WriteAll(10, 20)        // tail=[10,20] at nominals [5,6]

	require.Equal(t, []int{10, 20}, buf.Tail())
	require.Equal(t, []int{10, 20}, tailbuf.SliceTail(buf, 0, 2))
	require.Equal(t, []int{10, 20}, tailbuf.SliceNominal(buf, 5, 7))
	require.Equal(t, []int{20}, tailbuf.SliceNominal(buf, 6, 7))
	require.Empty(t, tailbuf.SliceNominal(buf, 0, 5)) // pre-clear range is gone
}

// TestPopFront_PopFrontN_Equivalence mirrors TestPopBack_PopBackN_Equivalence
// for the front-end pop variants.
func TestPopFront_PopFrontN_Equivalence(t *testing.T) {
	all := []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j"}
	buf1 := tailbuf.New[string](10).WriteAll(all...)
	buf2 := tailbuf.New[string](10).WriteAll(all...)

	popped1 := buf1.PopFrontN(4)
	var popped2 []string
	for i := 0; i < 4; i++ {
		popped2 = append([]string{buf2.PopFront()}, popped2...)
	}

	require.Equal(t, popped1, popped2)
	tailbuf.RequireEqualInternalState(t, buf1, buf2)
	require.Equal(t, buf1.Tail(), buf2.Tail())
}

// TestDropBack_DropBackN_Equivalence is the analogous parity test for the
// drop-back variants.
func TestDropBack_DropBackN_Equivalence(t *testing.T) {
	all := []rune{'a', 'b', 'c', 'd', 'e'}
	buf1 := tailbuf.New[rune](5).WriteAll(all...)
	buf2 := tailbuf.New[rune](5).WriteAll(all...)

	buf1.DropBackN(3)
	for i := 0; i < 3; i++ {
		buf2.DropBack()
	}
	tailbuf.RequireEqualInternalState(t, buf1, buf2)
}

// TestDo_NilContext pins the documented behavior that Do substitutes
// context.Background() for a nil context, rather than panicking when fn
// later calls ctx.Err(). A future deletion of the nil-check would be
// caught here.
func TestDo_NilContext(t *testing.T) {
	buf := tailbuf.New[int](3).WriteAll(10, 20, 30)

	// Bind nil to a typed variable so the static-analysis check on "nil
	// Context literal" doesn't fire here; passing a nil Context is exactly
	// the case under test.
	var nilCtx context.Context
	var seenCtxs []context.Context
	err := buf.Do(nilCtx, func(ctx context.Context, item, _, _ int) (int, error) {
		seenCtxs = append(seenCtxs, ctx)
		return item, nil
	})
	require.NoError(t, err)
	require.Len(t, seenCtxs, 3)
	for i, c := range seenCtxs {
		require.NotNilf(t, c, "Do must replace nil ctx with a real one (call %d)", i)
		require.NoErrorf(t, c.Err(), "substituted ctx must not be canceled (call %d)", i)
	}
}

// TestDo_ErrorHaltsAndPreservesPartialMutation pins the contract that when
// fn returns an error at iteration i, items at tail-relative positions
// [0, i) have been replaced and items at [i, Len) are untouched. The
// returned error is propagated unchanged.
//
// We deliberately drive the buffer into a wrapped state (oldestIdx > 0,
// items span the physical end of window) to ensure the partial-mutation
// accounting is correct under wrap, not just for oldestIdx=0.
func TestDo_ErrorHaltsAndPreservesPartialMutation(t *testing.T) {
	// cap=4, write 6 items: window=[5,6,3,4], oldestIdx=2, len=4. Tail is [3,4,5,6].
	buf := tailbuf.New[int](4).WriteAll(1, 2, 3, 4, 5, 6)
	require.Equal(t, []int{3, 4, 5, 6}, buf.Tail())

	sentinel := errors.New("stop at index 2")
	err := buf.Do(context.Background(),
		func(_ context.Context, item, index, _ int) (int, error) {
			if index == 2 {
				return 0, sentinel
			}
			return item * 100, nil
		})
	require.ErrorIs(t, err, sentinel)

	// Indices 0,1 mutated (3->300, 4->400); indices 2,3 untouched (5, 6).
	require.Equal(t, []int{300, 400, 5, 6}, buf.Tail())
}

// TestApplyDo_WrappedLen3Plus exercises Apply and Do over a multi-item
// wrapped tail (cap=4, oldestIdx=2, len=4). The A1 over-iteration regression
// class is most likely to re-emerge when wrap produces both a pre-wrap
// and post-wrap segment, so we want to pin "exactly Len calls in
// oldest-to-newest order" against this shape specifically.
func TestApplyDo_WrappedLen3Plus(t *testing.T) {
	// Both subtests share this initial state:
	// cap=4, write 6 items: window=[5,6,3,4], oldestIdx=2, len=4. Tail=[3,4,5,6].
	const bufCap = 4
	all := []int{1, 2, 3, 4, 5, 6}
	wantTail := []int{3, 4, 5, 6}

	t.Run("Apply_visits_each_live_once_in_order", func(t *testing.T) {
		buf := tailbuf.New[int](bufCap).WriteAll(all...)
		require.Equal(t, wantTail, buf.Tail())

		var seen []int
		buf.Apply(func(n int) int {
			seen = append(seen, n)
			return n * 10
		})
		require.Equal(t, wantTail, seen, "Apply must visit live items oldest-to-newest, exactly once each")
		require.Equal(t, []int{30, 40, 50, 60}, buf.Tail())
	})

	t.Run("Do_visits_each_live_once_with_correct_indices", func(t *testing.T) {
		buf := tailbuf.New[int](bufCap).WriteAll(all...)
		require.Equal(t, wantTail, buf.Tail())

		var seenItems, seenIndices, seenOffsets []int
		err := buf.Do(context.Background(),
			func(_ context.Context, item, index, off int) (int, error) {
				seenItems = append(seenItems, item)
				seenIndices = append(seenIndices, index)
				seenOffsets = append(seenOffsets, off)
				return item * 10, nil
			})
		require.NoError(t, err)
		require.Equal(t, wantTail, seenItems)
		require.Equal(t, []int{0, 1, 2, 3}, seenIndices, "index must be tail-relative")
		require.Equal(t, []int{2, 2, 2, 2}, seenOffsets, "tailOffset must be constant across calls")
		require.Equal(t, []int{30, 40, 50, 60}, buf.Tail())
	})
}

// TestPopFrontWriteReuseNominalIndex_AfterEviction extends
// TestPopFrontWriteReuseNominalIndex into the post-eviction regime where
// Offset is non-zero. The original review found that the offset=0 case
// alone would not catch a regression that special-cased PopFront-then-Write
// against a non-zero offset.
func TestPopFrontWriteReuseNominalIndex_AfterEviction(t *testing.T) {
	buf := tailbuf.New[string](3)
	buf.WriteAll("a", "b", "c", "d", "e") // tail=[c,d,e], offset=2
	require.Equal(t, []string{"c", "d", "e"}, buf.Tail())
	require.Equal(t, 2, buf.Offset())
	require.Equal(t, "e", buf.Front())
	require.Equal(t, 4, buf.Offset()+buf.Len()-1) // e at nominal 4

	buf.PopFront() // tail=[c,d], offset still 2, len=2
	require.Equal(t, []string{"c", "d"}, buf.Tail())
	require.Equal(t, 2, buf.Offset())

	buf.Write("x") // tail=[c,d,x], offset=2; x at nominal 4 (reuses e's slot)
	require.Equal(t, []string{"c", "d", "x"}, buf.Tail())
	require.Equal(t, "x", buf.Front())
	require.Equal(t, 2, buf.Offset(), "Write after PopFront must not advance offset")
	require.Equal(t, 4, buf.Offset()+buf.Len()-1, "x reuses the popped item's nominal index")

	start, end := buf.Bounds()
	require.Equal(t, 2, start)
	require.Equal(t, 5, end)
	require.Equal(t, 6, buf.Written(), "Written counts every Write, including post-pop reuse")
}

// TestTail_AppendDoesNotCorruptBuffer pins the 3-index cap on the slice
// returned by [Buf.Tail]. Before the cap was added, a caller doing
// append(buf.Tail(), x) would silently write into the buffer's internal
// window past the live region, breaking len/back/offset coherence. The cap
// forces append to allocate instead.
func TestTail_AppendDoesNotCorruptBuffer(t *testing.T) {
	t.Run("no-wrap tail", func(t *testing.T) {
		// cap=5, len=3, no wrap. Tail is window[0:3]; window[3] is free.
		buf := tailbuf.New[int](5).WriteAll(1, 2, 3)
		tail := buf.Tail()
		require.Equal(t, 3, cap(tail),
			"Tail must return a slice whose cap equals its len, to force append to allocate")

		// append(tail, 99) must NOT write through to window[3]; if it did,
		// a subsequent Write would see the dirty slot and produce wrong
		// output. We verify both by inspecting InternalWindow and by
		// continuing to use the buffer normally.
		_ = append(tail, 99)
		require.Equal(t, []int{1, 2, 3, 0, 0}, tailbuf.InternalWindow(buf),
			"append to Tail() result must not touch internal storage")
		buf.Write(4)
		require.Equal(t, []int{1, 2, 3, 4}, buf.Tail())
	})

	t.Run("empty tail", func(t *testing.T) {
		buf := tailbuf.New[int](3)
		tail := buf.Tail()
		require.Empty(t, tail)
		require.Equal(t, 0, cap(tail), "empty Tail must have cap 0")

		_ = append(tail, 99)
		require.Equal(t, []int{0, 0, 0}, tailbuf.InternalWindow(buf))
	})

	t.Run("single-item tail", func(t *testing.T) {
		// cap=3, len=1 at index 0.
		buf := tailbuf.New[int](3).WriteAll(7)
		tail := buf.Tail()
		require.Equal(t, []int{7}, tail)
		require.Equal(t, 1, cap(tail))

		_ = append(tail, 99)
		require.Equal(t, []int{7, 0, 0}, tailbuf.InternalWindow(buf))
	})
}

// TestReset_FromWrappedState verifies Reset fully zeroes the internal
// window even when the live items wrap around the end of physical storage,
// and that all metadata is truly reset. A regression in zeroTail's modular
// loop that stopped at the physical boundary would leak stale references
// and would not fail any other test.
func TestReset_FromWrappedState(t *testing.T) {
	buf := tailbuf.New[string](3)
	buf.WriteAll("a", "b", "c", "d", "e") // window=[d,e,c], oldestIdx=2, wrapped
	require.Equal(t, 2, buf.Offset())
	require.Equal(t, []string{"c", "d", "e"}, buf.Tail())

	buf.Reset()
	requireZeroInternalWindow(t, buf)
	require.Equal(t, 0, buf.Len())
	require.Equal(t, 0, buf.Offset())
	require.Equal(t, 0, buf.Written())
	require.Empty(t, buf.Tail())

	// The buffer should behave as if freshly constructed.
	buf.Write("x")
	require.Equal(t, []string{"x"}, buf.Tail())
	require.Equal(t, 1, buf.Written())
	require.Equal(t, "x", tailbuf.InternalWindow(buf)[0])
}

// TestSliceTail_WrappedBufferClipEnd covers the joint case where the
// buffer is physically wrapped AND the caller passes an end index past
// the live range. The modular read loop and the upper-bound clip path
// interact here; the existing TestBugA4 tests cover start past end, but
// not this start-inside / end-past combination.
func TestSliceTail_WrappedBufferClipEnd(t *testing.T) {
	buf := tailbuf.New[int](3)
	buf.WriteAll(1, 2, 3, 4, 5) // window=[4,5,3], oldestIdx=2, wrapped
	require.Equal(t, []int{3, 4, 5}, buf.Tail())

	// Tail-relative positions [1, 100): end is clipped to 3, so [1, 3).
	require.Equal(t, []int{4, 5}, tailbuf.SliceTail(buf, 1, 100))
	// End exactly at len: full tail.
	require.Equal(t, []int{3, 4, 5}, tailbuf.SliceTail(buf, 0, 3))
	// Partial from the start.
	require.Equal(t, []int{3, 4}, tailbuf.SliceTail(buf, 0, 2))
	// start == end == len: empty.
	require.Empty(t, tailbuf.SliceTail(buf, 3, 3))
	require.Empty(t, tailbuf.SliceTail(buf, 3, 100))
}

// TestTail_ElementMutationVisibleViaPeek pins the documented aliasing
// contract that in the no-wrap case, mutating an element of the slice
// returned by Tail is visible through subsequent reads of the buffer.
// The 3-index cap prevents append from corrupting the buffer, but
// element-level writes are still observed.
func TestTail_ElementMutationVisibleViaPeek(t *testing.T) {
	buf := tailbuf.New[int](5).WriteAll(1, 2, 3) // no-wrap, oldestIdx=0, len=3
	tail := buf.Tail()
	require.Equal(t, []int{1, 2, 3}, tail)

	tail[0] = 99
	tail[2] = 77
	require.Equal(t, 99, buf.Peek(0), "element mutation through Tail() must be visible via Peek")
	require.Equal(t, 77, buf.Peek(2))
	require.Equal(t, []int{99, 2, 77}, buf.Tail())
}

// TestDo_ErrorOnFirstIteration covers the boundary where fn returns an
// error at index 0: no items should have been mutated, and the error
// propagates. Complements TestDo_ErrorHaltsAndPreservesPartialMutation
// which errors at a non-zero index.
func TestDo_ErrorOnFirstIteration(t *testing.T) {
	buf := tailbuf.New[int](4).WriteAll(10, 20, 30)
	before := append([]int(nil), buf.Tail()...)

	sentinel := errors.New("stop at index 0")
	err := buf.Do(context.Background(),
		func(_ context.Context, item, _, _ int) (int, error) {
			return item * -1, sentinel // returned value is ignored on error
		})
	require.ErrorIs(t, err, sentinel)
	require.Equal(t, before, buf.Tail(),
		"no items must be mutated when fn returns an error at index 0")
}

// TestZeroValue_vs_NewZero_NilObservability pins the package contract
// that the internal nil-vs-empty representation of the window (nil for
// the zero-value Buf, non-nil for New(0)) is NOT observable through any
// public method. In particular, Tail() on an empty buffer must return a
// non-nil empty slice regardless of how the Buf was constructed.
func TestZeroValue_vs_NewZero_NilObservability(t *testing.T) {
	var zero tailbuf.Buf[int]
	new0 := tailbuf.New[int](0)

	require.NotNil(t, zero.Tail(), "zero-value Buf.Tail() must not be nil")
	require.NotNil(t, new0.Tail(), "New(0) Buf.Tail() must not be nil")
	require.Empty(t, zero.Tail())
	require.Empty(t, new0.Tail())

	// All the other public-API readers should also report identical state
	// between the two construction paths.
	require.Equal(t, zero.Cap(), new0.Cap())
	require.Equal(t, zero.Len(), new0.Len())
	require.Equal(t, zero.Written(), new0.Written())
	require.Equal(t, zero.Offset(), new0.Offset())
	zStart, zEnd := zero.Bounds()
	nStart, nEnd := new0.Bounds()
	require.Equal(t, zStart, nStart)
	require.Equal(t, zEnd, nEnd)
}

// TestZeroCap_OffsetTracksWritten pins the contract that, for a
// zero-capacity buffer, every Write/WriteAll advances Offset in lockstep
// with Written. This keeps the invariant Offset()+Len() == Written()
// intact (equality holds since no PopFront can run on a cap=0 buffer)
// and is the natural consequence of treating cap=0 writes as immediate
// eviction-on-write.
func TestZeroCap_OffsetTracksWritten(t *testing.T) {
	buf := tailbuf.New[int](0)
	require.Equal(t, 0, buf.Offset())
	require.Equal(t, 0, buf.Written())

	buf.Write(1)
	require.Equal(t, 1, buf.Offset())
	require.Equal(t, 1, buf.Written())

	buf.WriteAll(2, 3, 4)
	require.Equal(t, 4, buf.Offset())
	require.Equal(t, 4, buf.Written())

	// Bounds is the empty range at the current position.
	start, end := buf.Bounds()
	require.Equal(t, 4, start)
	require.Equal(t, 4, end)
	require.Equal(t, 0, buf.Len())

	// Invariant: Offset()+Len() == Written() when no PopFront has run.
	require.Equal(t, buf.Written(), buf.Offset()+buf.Len())

	// And the zero value of Buf must behave identically.
	var zero tailbuf.Buf[int]
	zero.Write(10).WriteAll(20, 30)
	require.Equal(t, 3, zero.Offset())
	require.Equal(t, 3, zero.Written())
}

// TestWriteAll_EmptyVarargsIsNoOp pins that WriteAll with zero arguments
// does not touch state, but does return the receiver for chaining.
func TestWriteAll_EmptyVarargsIsNoOp(t *testing.T) {
	buf := tailbuf.New[int](3).WriteAll(1, 2)
	ret := buf.WriteAll()
	require.Same(t, buf, ret, "WriteAll must return the receiver for chaining even with no items")
	require.Equal(t, []int{1, 2}, buf.Tail())
	require.Equal(t, 2, buf.Written())
	require.Equal(t, 2, buf.Len())

	// Also on an empty buffer and on a zero-cap buffer.
	emptyBuf := tailbuf.New[int](3)
	emptyBuf.WriteAll()
	require.Equal(t, 0, emptyBuf.Len())
	require.Equal(t, 0, emptyBuf.Written())

	zeroCap := tailbuf.New[int](0)
	zeroCap.WriteAll()
	require.Equal(t, 0, zeroCap.Written())
}

// TestPeek_FrontBackConsistency pins that Peek at the boundary positions
// agrees with Front and Back. A refactor that diverged one of the three
// code paths would be caught by this.
func TestPeek_FrontBackConsistency(t *testing.T) {
	buf := tailbuf.New[int](4)
	// Drive the buffer into a wrapped state with len == cap.
	for i := 0; i < 6; i++ {
		buf.Write(i * 10)
	}
	// Live tail: [20, 30, 40, 50].
	require.Equal(t, buf.Peek(0), buf.Back(), "Peek(0) == Back()")
	require.Equal(t, buf.Peek(buf.Len()-1), buf.Front(), "Peek(Len-1) == Front()")
}

// TestSlice_BoundaryAtLen covers the edge case where start == end == Len:
// both Slice* helpers must return an empty slice without panicking.
func TestSlice_BoundaryAtLen(t *testing.T) {
	buf := tailbuf.New[int](3).WriteAll(1, 2, 3)

	require.Empty(t, tailbuf.SliceTail(buf, 3, 3))
	// Nominal index 3 equals offset+len (0+3) for a non-evicted buffer.
	require.Empty(t, tailbuf.SliceNominal(buf, 3, 3))

	// And again against a wrapped buffer where offset > 0.
	buf.WriteAll(4, 5) // window=[4,5,3], offset=2, len=3
	require.Empty(t, tailbuf.SliceTail(buf, 3, 3))
	require.Empty(t, tailbuf.SliceNominal(buf, 5, 5))
}

// TestApply_EmptyBuffer pins that Apply on a len==0 buffer is a no-op (fn
// never invoked) and returns the receiver for chaining. A regression in
// the loop guard would not be caught by the existing wrapped/no-wrap
// tests, which all run against non-empty buffers.
func TestApply_EmptyBuffer(t *testing.T) {
	buf := tailbuf.New[int](5)
	calls := 0
	got := buf.Apply(func(n int) int {
		calls++
		return n + 1
	})
	require.Same(t, buf, got, "Apply must return the receiver for chaining")
	require.Zero(t, calls, "fn must not be invoked when Len == 0")

	// Also for the zero-value Buf, where window is nil.
	var z tailbuf.Buf[int]
	calls = 0
	gotZ := z.Apply(func(n int) int {
		calls++
		return n
	})
	require.Same(t, &z, gotZ)
	require.Zero(t, calls)
}

// TestDo_EmptyBuffer pins that Do on a len==0 buffer is a no-op: fn never
// invoked, nil error returned. Complements TestApply_EmptyBuffer.
func TestDo_EmptyBuffer(t *testing.T) {
	buf := tailbuf.New[int](5)
	calls := 0
	err := buf.Do(context.Background(), func(_ context.Context, n, _, _ int) (int, error) {
		calls++
		return n, nil
	})
	require.NoError(t, err)
	require.Zero(t, calls)

	// Zero-value Buf.
	var z tailbuf.Buf[int]
	calls = 0
	err = z.Do(context.Background(), func(_ context.Context, n, _, _ int) (int, error) {
		calls++
		return n, nil
	})
	require.NoError(t, err)
	require.Zero(t, calls)
}

// TestApplyDo_WrappedLen2_ModularCrossing covers Apply and Do at len==2
// where the iteration genuinely crosses the modular boundary. The
// existing TestApplyDo_WrappedLen3Plus uses len=4 and TestBugA1 covers
// len=2 with adjacent physical positions; neither hits a (oldestIdx + i)
// % winLen wrap mid-iteration at len=2. Setup: cap=3, oldestIdx=2, len=2,
// window=[4,_,3]. Iteration visits physical 2 then 0.
func TestApplyDo_WrappedLen2_ModularCrossing(t *testing.T) {
	mk := func() *tailbuf.Buf[int] {
		// WriteAll(1,2,3,4,5) on cap=3 leaves window=[4,5,3] oldest=2 len=3.
		// PopFront removes 5 (newest); window=[4,_,3] oldest=2 len=2.
		// The two live items, oldest-to-newest, are 3 then 4. Iteration
		// visits physical indices 2 (value 3) then 0 (value 4).
		buf := tailbuf.New[int](3).WriteAll(1, 2, 3, 4, 5)
		buf.PopFront()
		return buf
	}

	t.Run("Apply visits oldest then newest exactly once each", func(t *testing.T) {
		buf := mk()
		var visits []int
		buf.Apply(func(n int) int {
			visits = append(visits, n)
			return n * 10
		})
		require.Equal(t, []int{3, 4}, visits, "Apply must visit 3 (oldest) then 4 (newest)")
		require.Equal(t, []int{30, 40}, buf.Tail(), "Tail must reflect transformed values")
	})

	t.Run("Do visits oldest then newest exactly once each", func(t *testing.T) {
		buf := mk()
		var visits []int
		var indices []int
		err := buf.Do(context.Background(), func(_ context.Context, n, index, _ int) (int, error) {
			visits = append(visits, n)
			indices = append(indices, index)
			return n * 10, nil
		})
		require.NoError(t, err)
		require.Equal(t, []int{3, 4}, visits)
		require.Equal(t, []int{0, 1}, indices, "Do's index argument is tail-relative")
		require.Equal(t, []int{30, 40}, buf.Tail())
	})
}

// TestInvariantWalker drives a buffer through a varied sequence of Writes,
// Pops, Drops, Clears, and Resets, calling tailbuf.CheckInvariants after
// every operation. Catches any future state-tracking refactor that breaks
// one of the documented invariants on Buf (e.g. offset+len > written).
func TestInvariantWalker(t *testing.T) {
	buf := tailbuf.New[int](4)
	tailbuf.CheckInvariants(t, buf)

	// Field order: pointer-only field first (do), pointer+len field second
	// (name). Reverses fieldalignment lint by keeping the two GC-scanned
	// pointer slots contiguous at the head of the struct.
	steps := []struct {
		do   func()
		name string
	}{
		{func() { buf.Write(1) }, "Write 1"},
		{func() { buf.Write(2) }, "Write 2"},
		{func() { buf.WriteAll(3, 4) }, "WriteAll 3,4"},
		{func() { buf.Write(5) }, "Write 5 (eviction)"},
		{func() { buf.PopFront() }, "PopFront"},
		{func() { buf.Write(6) }, "Write 6"},
		{func() { buf.PopBack() }, "PopBack"},
		{func() { buf.DropBack() }, "DropBack"},
		{func() { buf.PopFrontN(2) }, "PopFrontN(2)"},
		{func() { buf.WriteAll(7, 8, 9, 10, 11) }, "WriteAll 7,8,9,10,11"},
		{func() { buf.PopBackN(2) }, "PopBackN(2)"},
		{func() { buf.DropBackN(99) }, "DropBackN(99)"},
		{func() { buf.WriteAll(12, 13) }, "WriteAll 12,13"},
		{func() { buf.Clear() }, "Clear"},
		{func() { buf.Write(14) }, "Write 14"},
		{func() { buf.Reset() }, "Reset"},
		{func() { buf.Write(15) }, "Write 15"},
	}
	for _, step := range steps {
		step.do()
		t.Run(step.name, func(t *testing.T) {
			tailbuf.CheckInvariants(t, buf)
		})
	}
}

// TestInvariantWalker_ZeroCap walks the cap=0 buffer through writes, drops,
// pops, and resets, validating the invariants at each step. In particular,
// every Write/WriteAll must keep offset+len <= written; the documented
// equality (when no PopFront has run) means offset == written here.
func TestInvariantWalker_ZeroCap(t *testing.T) {
	check := func(buf *tailbuf.Buf[int], label string) {
		t.Run(label, func(t *testing.T) {
			tailbuf.CheckInvariants(t, buf)
			// Stronger invariant for cap=0 with no PopFront: equality.
			require.Equal(t, buf.Written(), buf.Offset(), "cap=0: offset == written")
		})
	}

	// New(0)
	buf := tailbuf.New[int](0)
	check(buf, "New(0) fresh")
	buf.Write(1)
	check(buf, "after Write")
	buf.WriteAll(2, 3, 4)
	check(buf, "after WriteAll(3)")
	// PopFront/PopBack/DropBack must be no-ops on a cap=0 buffer; pin that
	// alongside the rest of the invariants.
	require.Equal(t, 0, buf.PopFront(), "PopFront on cap=0 returns zero value")
	check(buf, "after PopFront")
	require.Equal(t, 0, buf.PopBack(), "PopBack on cap=0 returns zero value")
	check(buf, "after PopBack")
	buf.DropBack()
	check(buf, "after DropBack")
	buf.Clear() // no-op on empty; offset unchanged
	check(buf, "after Clear")
	buf.Reset()
	check(buf, "after Reset")
	buf.Write(5)
	check(buf, "after Write post-Reset")

	// Zero value.
	var z tailbuf.Buf[int]
	check(&z, "zero value fresh")
	z.Write(1)
	check(&z, "zero value after Write")
	z.PopFront() // exercises the nil-window guard
	check(&z, "zero value after PopFront")
	z.PopBack()
	check(&z, "zero value after PopBack")
}

// TestSliceNominal_NegativeStartClipsNotPanics pins the documented asymmetry
// between SliceTail (panics on start<0) and SliceNominal (clips start<0
// like any other below-Offset index). In nominal-index space, "below
// Offset" is meaningful — it denotes an item that has already been
// evicted — and negative is just the extreme of that. A future refactor
// that "mirrors SliceTail" by adding a start<0 panic to SliceNominal
// would break this contract; this test fails it loudly.
func TestSliceNominal_NegativeStartClipsNotPanics(t *testing.T) {
	buf := tailbuf.New[int](3).WriteAll(1, 2, 3, 4, 5) // Bounds = (2, 5); live tail [3,4,5]

	// Negative start entirely below the live range: clipped to empty.
	require.NotPanics(t, func() {
		got := tailbuf.SliceNominal(buf, -10, 0)
		require.Empty(t, got, "(-10, 0) is entirely below Offset; expected empty")
	})

	// Negative start, end inside the live range: clipped at Offset.
	// Bounds=(2,5); requesting (-10, 4) reads nominal positions 2 and 3
	// (items 3 and 4) — nominal 4 (item 5) is excluded by the half-open
	// upper bound.
	require.NotPanics(t, func() {
		got := tailbuf.SliceNominal(buf, -10, 4)
		require.Equal(t, []int{3, 4}, got, "negative start clipped to Offset; end is exclusive at nominal 4")
	})

	// Negative start, end past the live range: returns full live tail.
	require.NotPanics(t, func() {
		got := tailbuf.SliceNominal(buf, -10, 100)
		require.Equal(t, []int{3, 4, 5}, got, "clipped to the full live range on both ends")
	})

	// Inverted range still panics, even with negatives.
	require.PanicsWithValue(t, "tailbuf: end must be >= start", func() {
		tailbuf.SliceNominal(buf, -1, -5)
	})

	// SliceTail's contrasting contract: tail-relative negative start panics.
	require.PanicsWithValue(t, "tailbuf: start must be >= 0", func() {
		tailbuf.SliceTail(buf, -1, 2)
	})

	// Extreme negatives must not silent-overflow. A naive
	// `start - b.offset` would wrap to a large positive on these inputs
	// once Offset > 0; the contract says start <= Offset is just clipped
	// to "start of live range" regardless of how far below.
	t.Run("math.MinInt start does not overflow", func(t *testing.T) {
		got := tailbuf.SliceNominal(buf, math.MinInt, 100)
		require.Equal(t, []int{3, 4, 5}, got,
			"math.MinInt start must clip to Offset, not overflow into empty")
	})
	t.Run("math.MinInt start with math.MaxInt end", func(t *testing.T) {
		got := tailbuf.SliceNominal(buf, math.MinInt, math.MaxInt)
		require.Equal(t, []int{3, 4, 5}, got,
			"MinInt..MaxInt must read the full live range")
	})
	t.Run("math.MinInt end short-circuits to empty", func(t *testing.T) {
		// end < start would panic, so test the boundary: end == start.
		require.Equal(t, []int{}, tailbuf.SliceNominal(buf, math.MinInt, math.MinInt))
	})
}

// TestCanonicalEmpty_ViaEveryRoute pins the new invariant established by the
// "Canonicalize empty-buffer state" change: every operation that empties
// the buffer leaves it in the canonical empty state (oldestIdx == 0).
// Without this test, removing any of the three `if b.len == 0 {
// b.oldestIdx = 0 }` lines in PopFront / PopBack / DropBack compiles
// and passes every other test, silently reintroducing the asymmetry.
func TestCanonicalEmpty_ViaEveryRoute(t *testing.T) {
	// Drive the buffer into a wrapped state where oldestIdx != 0, so that
	// reaching empty via the modular cursor advance lands at oldestIdx > 0
	// before the empty-pin fires.
	mkWrapped := func() *tailbuf.Buf[int] {
		// WriteAll(1,2,3,4) on cap=3 evicts 1; window=[4,2,3], oldestIdx=1,
		// len=3, offset=1, written=4. The wrap puts the live items at
		// physical positions 1,2,0 — visiting from oldestIdx=1 wraps once.
		return tailbuf.New[int](3).WriteAll(1, 2, 3, 4)
	}

	t.Run("PopBack to empty", func(t *testing.T) {
		buf := mkWrapped()
		for i := 0; i < 3; i++ {
			buf.PopBack()
		}
		require.Zero(t, buf.Len())
		tailbuf.CheckInvariants(t, buf)
	})

	t.Run("DropBack to empty", func(t *testing.T) {
		buf := mkWrapped()
		for i := 0; i < 3; i++ {
			buf.DropBack()
		}
		require.Zero(t, buf.Len())
		tailbuf.CheckInvariants(t, buf)
	})

	t.Run("PopFront to empty", func(t *testing.T) {
		buf := mkWrapped()
		for i := 0; i < 3; i++ {
			buf.PopFront()
		}
		require.Zero(t, buf.Len())
		tailbuf.CheckInvariants(t, buf)
	})

	t.Run("post-empty Write lands at window[0]", func(t *testing.T) {
		// The canonical-empty pin guarantees the next Write hits the
		// "case 0" path in the write helper, which writes at window[0].
		// Verify via Tail() — the single live item should be at the head
		// of the underlying storage, independent of which emptying route
		// we took.
		// Field order: pointer-only field before mixed-pointer field to
		// satisfy fieldalignment lint (see TestInvariantWalker for the
		// same shape).
		for _, drain := range []struct {
			do   func(*tailbuf.Buf[int])
			name string
		}{
			{func(b *tailbuf.Buf[int]) {
				for i := 0; i < 3; i++ {
					b.PopBack()
				}
			}, "PopBack"},
			{func(b *tailbuf.Buf[int]) {
				for i := 0; i < 3; i++ {
					b.DropBack()
				}
			}, "DropBack"},
			{func(b *tailbuf.Buf[int]) {
				for i := 0; i < 3; i++ {
					b.PopFront()
				}
			}, "PopFront"},
		} {
			t.Run(drain.name, func(t *testing.T) {
				buf := mkWrapped()
				drain.do(buf)
				buf.Write(99)
				require.Equal(t, []int{99}, buf.Tail())
				require.Equal(t, 99, buf.Peek(0))
				tailbuf.CheckInvariants(t, buf)
			})
		}
	})
}

// TestBoundsInBounds_AfterReset pins that Reset leaves Bounds and InBounds
// in a coherent state for every relevant nominal index. The existing
// TestReset_FromWrappedState checks Offset/Len/Written but never queries
// Bounds or InBounds, so a regression that left offset stale through
// Reset would not be caught.
func TestBoundsInBounds_AfterReset(t *testing.T) {
	// Drive the buffer into a wrapped state with non-zero offset so the
	// pre-Reset Bounds value is genuinely different from (0, 0).
	buf := tailbuf.New[int](3)
	buf.WriteAll(1, 2, 3, 4, 5) // window=[4,5,3], oldest=2, len=3, offset=2

	preStart, preEnd := buf.Bounds()
	require.Equal(t, 2, preStart)
	require.Equal(t, 5, preEnd)

	buf.Reset()

	start, end := buf.Bounds()
	require.Equal(t, 0, start, "Reset must zero the start of Bounds")
	require.Equal(t, 0, end, "Reset must zero the end of Bounds")

	require.False(t, buf.InBounds(0), "InBounds must be false on an empty post-Reset buffer for nominal 0")
	require.False(t, buf.InBounds(2), "...and for what used to be the live range")
	require.False(t, buf.InBounds(5), "...and for what used to be just past the live range")
	require.False(t, buf.InBounds(-1))

	// A subsequent Write must produce a coherent post-Reset state.
	buf.Write(99)
	start, end = buf.Bounds()
	require.Equal(t, 0, start)
	require.Equal(t, 1, end)
	require.True(t, buf.InBounds(0))
	require.False(t, buf.InBounds(1))
}

// TestTail_MutationUnderWrapDoesNotCorruptBuffer pins the wrap-case half
// of Tail's aliasing contract: when the live items wrap, Tail allocates
// a fresh slice (so mutations to it are NOT visible through Peek). The
// no-wrap case is already pinned by TestTail_ElementMutationVisibleViaPeek.
func TestTail_MutationUnderWrapDoesNotCorruptBuffer(t *testing.T) {
	buf := tailbuf.New[int](4)
	// Force wrap: cap=4 plus two extra writes leaves oldestIdx=2, len=4,
	// items spanning physical [2,3,0,1] = [3,4,5,6].
	buf.WriteAll(1, 2, 3, 4, 5, 6)
	require.Equal(t, []int{3, 4, 5, 6}, buf.Tail())

	tail := buf.Tail()
	// Mutate every position of the returned slice.
	for i := range tail {
		tail[i] = 999
	}
	// Peek must still report the original values; the buffer's internal
	// storage must be untouched.
	require.Equal(t, 3, buf.Peek(0))
	require.Equal(t, 4, buf.Peek(1))
	require.Equal(t, 5, buf.Peek(2))
	require.Equal(t, 6, buf.Peek(3))
	require.Equal(t, []int{3, 4, 5, 6}, buf.Tail())
}

// TestPeekFrontBack_AfterPopFront pins that the Peek(0)==Back(),
// Peek(Len-1)==Front() consistency holds after a PopFront has shrunk the
// live range. The existing TestPeek_FrontBackConsistency only tests
// against a freshly-wrapped full buffer.
func TestPeekFrontBack_AfterPopFront(t *testing.T) {
	buf := tailbuf.New[int](4)
	// Drive to wrap: cap=4, WriteAll(1..5) evicts 1; window=[5,2,3,4],
	// oldest=1, len=4. Live tail oldest-to-newest is [2,3,4,5].
	buf.WriteAll(1, 2, 3, 4, 5)
	require.Equal(t, []int{2, 3, 4, 5}, buf.Tail())

	// PopFront: newest (5) removed; oldest unchanged.
	popped := buf.PopFront()
	require.Equal(t, 5, popped)
	require.Equal(t, 3, buf.Len())
	require.Equal(t, []int{2, 3, 4}, buf.Tail())

	require.Equal(t, buf.Back(), buf.Peek(0), "Peek(0) must equal Back after PopFront")
	require.Equal(t, buf.Front(), buf.Peek(buf.Len()-1), "Peek(Len-1) must equal Front after PopFront")

	// Specifically: live tail is [2,3,4]; Back=2, Front=4.
	require.Equal(t, 2, buf.Back())
	require.Equal(t, 4, buf.Front())
}

// TestChaining_ReturnsReceiver pins that the mutating methods documented
// as "returns b for chaining" actually return the receiver. A future
// refactor that swapped any of them to return a new *Buf (or nil) would
// break chained call sites silently.
func TestChaining_ReturnsReceiver(t *testing.T) {
	buf := tailbuf.New[int](3)

	require.Same(t, buf, buf.Write(1))
	require.Same(t, buf, buf.WriteAll(2, 3))
	require.Same(t, buf, buf.Apply(func(n int) int { return n }))
	require.Same(t, buf, buf.Reset())
	require.Same(t, buf, buf.Clear())
}

// TestPopBack_ThenWrite_Wrapped pins that Write after PopBack on a wrapped
// buffer reuses the freed slot (no extra eviction). The A5 regression
// covers Write-after-PopFront; this is the symmetric case that A5+A6
// together imply but no single test exercises directly.
func TestPopBack_ThenWrite_Wrapped(t *testing.T) {
	buf := tailbuf.New[int](3).WriteAll(1, 2, 3, 4) // window=[4,2,3], oldest=1, len=3
	require.Equal(t, []int{2, 3, 4}, buf.Tail())
	require.Equal(t, 1, buf.Offset())

	popped := buf.PopBack()
	require.Equal(t, 2, popped)
	require.Equal(t, []int{3, 4}, buf.Tail())
	require.Equal(t, 2, buf.Offset(), "PopBack must advance Offset")

	// The next Write must NOT evict — there's a free slot now.
	buf.Write(5)
	require.Equal(t, 3, buf.Len(), "len must grow back to 3")
	require.Equal(t, []int{3, 4, 5}, buf.Tail())
	require.Equal(t, 2, buf.Offset(), "Offset must NOT advance on a non-evicting Write")
	require.Equal(t, 5, buf.Written())

	// A second Write evicts because we're back at capacity.
	buf.Write(6)
	require.Equal(t, []int{4, 5, 6}, buf.Tail())
	require.Equal(t, 3, buf.Offset(), "Offset must advance once on eviction")
	require.Equal(t, 6, buf.Written())
}
