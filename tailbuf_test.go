package tailbuf_test

import (
	"context"
	"errors"
	"fmt"
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
		buf.WriteAll("a", "b", "c", "d") // wrap: window=[d,b,c], back=1
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
		buf.WriteAll("a") // back=0, len=1, single item at window[0]

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
		buf.WriteAll(1, 2, 3, 4) // window=[4,2,3], back=1
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
	buf.WriteAll(10, 20, 30, 40) // window=[40,20,30], back=1, offset=1

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
// not wrap but b.back > 0. The pre-fix SliceTail indexed window[start:end]
// directly, which silently returned items from before the live region.
func TestBugA2_SliceTailAfterPopBack(t *testing.T) {
	buf := tailbuf.New[int](5)
	buf.WriteAll(1, 2, 3) // back=0, len=3
	buf.PopBack()         // back=1, len=2, tail=[2,3]

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
// We deliberately drive the buffer into a wrapped state (back > 0, items
// span the physical end of window) to ensure the partial-mutation accounting
// is correct under wrap, not just for back=0.
func TestDo_ErrorHaltsAndPreservesPartialMutation(t *testing.T) {
	// cap=4, write 6 items: window=[5,6,3,4], back=2, len=4. Tail is [3,4,5,6].
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
// wrapped tail (cap=4, back=2, len=4). The A1 over-iteration regression
// class is most likely to re-emerge when wrap produces both a pre-wrap
// and post-wrap segment, so we want to pin "exactly Len calls in
// oldest-to-newest order" against this shape specifically.
func TestApplyDo_WrappedLen3Plus(t *testing.T) {
	// Both subtests share this initial state:
	// cap=4, write 6 items: window=[5,6,3,4], back=2, len=4. Tail=[3,4,5,6].
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
	buf.WriteAll("a", "b", "c", "d", "e") // window=[d,e,c], back=2, wrapped
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
	buf.WriteAll(1, 2, 3, 4, 5) // window=[4,5,3], back=2, wrapped
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
	buf := tailbuf.New[int](5).WriteAll(1, 2, 3) // no-wrap, back=0, len=3
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
