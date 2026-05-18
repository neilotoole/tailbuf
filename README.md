# tailbuf: fixed-size tail buffer for Go

[![Go Reference](https://pkg.go.dev/badge/github.com/neilotoole/tailbuf.svg)](https://pkg.go.dev/github.com/neilotoole/tailbuf)
[![Go Report Card](https://goreportcard.com/badge/neilotoole/tailbuf)](https://goreportcard.com/report/neilotoole/tailbuf)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](https://github.com/neilotoole/tailbuf/blob/master/LICENSE)
![Workflow](https://github.com/neilotoole/tailbuf/actions/workflows/go.yml/badge.svg)

Package [`tailbuf`](https://pkg.go.dev/github.com/neilotoole/tailbuf) keeps
the N most recent items written to it. Writes are O(1) amortized; the oldest
item is evicted to make room when the buffer is at capacity. Useful as a
rolling log tail, an event ring, a debugger context buffer — anywhere you
want a bounded, FIFO-evicting view on a longer stream.

`tailbuf.Buf[T]` is generic: it can hold any type, including structs,
pointers, or interfaces.

## Install

```shell
go get github.com/neilotoole/tailbuf
```

## Stability

`tailbuf` is pre-1.0; expect the API to change before `v1.0`. Pin a
specific version in your `go.mod`. [Feedback](https://github.com/neilotoole/tailbuf/issues)
on the shape of the API is especially welcome.

## Quick start

```go
package main

import (
    "fmt"
    "github.com/neilotoole/tailbuf"
)

func main() {
    buf := tailbuf.New[string](3)

    buf.WriteAll("a", "b", "c", "d", "e")
    fmt.Println(buf.Tail())    // [c d e]
    fmt.Println(buf.Written()) // 5
}
```

The buffer holds three items; the writes of `"a"` and `"b"` have been
evicted to make room, so [`Tail`](https://pkg.go.dev/github.com/neilotoole/tailbuf#Buf.Tail)
returns the three newest items.

## Vocabulary

A `Buf[T]` is a sliding window over a longer **nominal stream** of writes.
Two terms appear throughout the API:

- **Newest end** — where new items land via [`Write`](https://pkg.go.dev/github.com/neilotoole/tailbuf#Buf.Write).
  [`Newest`](https://pkg.go.dev/github.com/neilotoole/tailbuf#Buf.Newest)
  returns the item written most recently.
- **Oldest end** — where items are evicted when the buffer fills.
  [`Oldest`](https://pkg.go.dev/github.com/neilotoole/tailbuf#Buf.Oldest)
  returns the item that's been alive the longest.

Items in the window are addressed by **nominal index**: their position in
the full stream of writes, *not* an offset into the current window. After
`WriteAll("a", "b", "c", "d", "e")` on a cap-3 buffer, the live items have
nominal indices 2, 3, 4; `"a"` (nominal 0) and `"b"` (nominal 1) are
evicted. [`Bounds`](https://pkg.go.dev/github.com/neilotoole/tailbuf#Buf.Bounds)
returns the half-open nominal range currently retained;
[`InBounds`](https://pkg.go.dev/github.com/neilotoole/tailbuf#Buf.InBounds)
reports whether a given nominal index is still live.

## Writing

```go
buf := tailbuf.New[int](4)

buf.Write(1)              // single item
buf.WriteAll(2, 3, 4, 5)  // variadic append

// Write and WriteAll return *Buf[T] for chaining.
buf2 := tailbuf.New[int](3).WriteAll(10, 20, 30)
fmt.Println(buf2.Tail())  // [10 20 30]
```

When the buffer is at capacity, every subsequent `Write` evicts the current
oldest item to make room.

## Reading the window

[`Tail`](https://pkg.go.dev/github.com/neilotoole/tailbuf#Buf.Tail) returns
the live items in oldest-to-newest order:

```go
buf := tailbuf.New[string](3).WriteAll("a", "b", "c", "d", "e")
fmt.Println(buf.Tail())  // [c d e]
```

For single-item reads:

```go
buf := tailbuf.New[int](3).WriteAll(10, 20, 30)

fmt.Println(buf.Newest())  // 30  (most recently written)
fmt.Println(buf.Oldest())  // 10  (oldest live item)
fmt.Println(buf.Peek(0))   // 10  (tail-relative; position 0 == Oldest)
fmt.Println(buf.Peek(2))   // 30  (position Len-1 == Newest)
```

`Newest` and `Oldest` return the zero value of `T` on an empty buffer.
[`Peek`](https://pkg.go.dev/github.com/neilotoole/tailbuf#Buf.Peek) panics if
the index is out of range.

### Slice aliasing

When the live items don't wrap around the internal ring, `Tail` returns a
slice that **aliases** the buffer's internal storage — valid only until
the next mutation. The returned slice has `cap == len`, so `append`-ing to
it always allocates a fresh backing array; but mutating elements through it
does reach into the buffer in the no-wrap case, and is visible on subsequent
reads.

If you need a stable snapshot regardless of wrap state or future mutations,
use [`SliceTail`](https://pkg.go.dev/github.com/neilotoole/tailbuf#SliceTail)
or [`SliceNominal`](https://pkg.go.dev/github.com/neilotoole/tailbuf#SliceNominal).
Both always allocate a fresh slice:

```go
buf := tailbuf.New[int](5).WriteAll(1, 2, 3, 4, 5)

// Tail-relative slicing: position 0 is Oldest.
fmt.Println(tailbuf.SliceTail(buf, 0, 2))   // [1 2]
fmt.Println(tailbuf.SliceTail(buf, 3, 5))   // [4 5]
fmt.Println(tailbuf.SliceTail(buf, 4, 100)) // [5]   (upper bound clipped)

// Nominal slicing: index into the full write stream.
buf2 := tailbuf.New[int](3).WriteAll(1, 2, 3, 4, 5)  // bounds = (2, 5)
fmt.Println(tailbuf.SliceNominal(buf2, 2, 5))   // [3 4 5]
fmt.Println(tailbuf.SliceNominal(buf2, 1, 3))   // [3]   (1 is evicted, skipped)
fmt.Println(tailbuf.SliceNominal(buf2, -5, 4))  // [3 4] (negative start clipped)
```

`SliceTail` and `SliceNominal` clip out-of-range upper bounds silently. See
the [Bounds policy](https://pkg.go.dev/github.com/neilotoole/tailbuf#hdr-Bounds_policy)
section of the godoc for the contrast with `Peek` (which panics) and the
deliberate asymmetry around negative start values between the two helpers.

## Removing items

Two families remove items from the buffer:

| Method                       | Returns                | Advances `Offset`? |
|------------------------------|------------------------|--------------------|
| `PopOldest()`                | the removed item       | yes (by 1)         |
| `PopOldestN(n)`              | the n removed items    | yes (by n)         |
| `DropOldest()`               | nothing                | yes (by 1)         |
| `DropOldestN(n)`             | nothing                | yes (by n)         |
| `PopNewest()`                | the removed item       | no                 |
| `PopNewestN(n)`              | the n removed items    | no                 |
| `DropNewest()`               | nothing                | no                 |
| `DropNewestN(n)`             | nothing                | no                 |

The `Drop*` variants skip the value copy (singular) or the slice allocation
(plural), so prefer them when the caller doesn't need the removed values.

```go
buf := tailbuf.New[string](3).WriteAll("a", "b", "c")

fmt.Println(buf.PopOldest())  // a   (oldest first; Offset advances)
fmt.Println(buf.PopNewest())  // c   (newest; Offset unchanged)
fmt.Println(buf.Tail())       // [b]
fmt.Println(buf.Offset())     // 1
```

```go
buf := tailbuf.New[int](5).WriteAll(1, 2, 3, 4, 5)

// PopOldestN and PopNewestN both return items in oldest-to-newest order.
fmt.Println(buf.PopOldestN(2))  // [1 2]  (the two oldest)
fmt.Println(buf.PopNewestN(2))  // [4 5]  (the two newest)
fmt.Println(buf.Tail())         // [3]
```

Removing from the **newest** end never advances `Offset` — the window simply
shrinks at that end. Removing from the **oldest** end advances `Offset` by
the number of items removed, exactly as eviction-on-write would.

## Transforming items in place

[`Apply`](https://pkg.go.dev/github.com/neilotoole/tailbuf#Buf.Apply)
replaces each item with the value returned by its callback. Calls chain via
the returned `*Buf[T]`:

```go
buf := tailbuf.New[string](5)
buf.WriteAll("In", "Xanadu  ", "   did", "Kubla  ", "Khan")
buf.Apply(strings.ToUpper).Apply(strings.TrimSpace)
fmt.Println(buf.Tail())  // [IN XANADU DID KUBLA KHAN]
```

[`Do`](https://pkg.go.dev/github.com/neilotoole/tailbuf#Buf.Do) is `Apply`
with context and error awareness. The callback receives the item's
tail-relative `index` and the buffer's `tailOffset` (their sum is the item's
nominal index); if the callback returns an error, iteration halts there and
items at and after that position are left unchanged.

```go
buf := tailbuf.New[int](3).WriteAll(1, 2, 3)

err := buf.Do(ctx, func(ctx context.Context, n, index, tailOffset int) (int, error) {
    if n > 2 {
        return n, fmt.Errorf("value too large: %d", n)
    }
    return n * 10, nil
})
fmt.Println(err)         // value too large: 3
fmt.Println(buf.Tail())  // [10 20 3]
```

`Apply` and `Do` both panic on a nil callback.

## Inspecting state

```go
buf := tailbuf.New[string](3)
buf.WriteAll("a", "b", "c", "d", "e")  // "a" and "b" have been evicted

fmt.Println(buf.Cap())        // 3  (fixed at construction)
fmt.Println(buf.Len())        // 3  (current live count, <= Cap)
fmt.Println(buf.Written())    // 5  (total writes ever; never decreases)
fmt.Println(buf.Offset())     // 2  (count of items removed from the oldest end)

start, end := buf.Bounds()    // half-open nominal range of live items
fmt.Println(start, end)       // 2 5

fmt.Println(buf.InBounds(1))  // false ("b" evicted)
fmt.Println(buf.InBounds(3))  // true  ("d" still live)
```

Invariants worth remembering:

- `Bounds() == (Offset(), Offset()+Len())`
- `Len() <= Cap()`
- `Offset() + Len() <= Written()`, with equality unless `PopNewest` or
  `DropNewest` (or their N variants) has removed an item.

## Resetting

```go
buf := tailbuf.New[string](3).WriteAll("a", "b")

buf.Reset()
fmt.Println(buf.Len(), buf.Written(), buf.Offset())  // 0 0 0

buf.WriteAll("c", "d")
buf.Clear()
fmt.Println(buf.Len(), buf.Written(), buf.Offset())  // 0 2 2
```

- [`Reset`](https://pkg.go.dev/github.com/neilotoole/tailbuf#Buf.Reset)
  empties the tail **and** zeros `Written` and `Offset` — the buffer is
  exactly as if just constructed.
- [`Clear`](https://pkg.go.dev/github.com/neilotoole/tailbuf#Buf.Clear)
  empties the tail but keeps `Written` unchanged; the cleared items are
  conceptually evicted off the oldest end, so `Offset` advances by the prior
  `Len()`.

Pick `Clear` when downstream code tracks `Written` across the operation
(the counter should not reset); pick `Reset` when you want a fresh buffer
with no history.

## Zero value

The zero value of `tailbuf.Buf[T]` is a usable empty zero-capacity buffer:

```go
var buf tailbuf.Buf[int]
buf.Write(1)               // silently dropped (cap == 0)
fmt.Println(buf.Len())     // 0
fmt.Println(buf.Written()) // 1
fmt.Println(buf.Offset())  // 1  (advances in lockstep with Written)
```

A zero-capacity buffer accepts writes (incrementing `Written` and `Offset`
in lockstep) but retains no items. Call
[`tailbuf.New`](https://pkg.go.dev/github.com/neilotoole/tailbuf#New) with a
non-zero capacity to get a buffer that actually retains items.

## Concurrency

`Buf` is **not** safe for concurrent use. Multiple goroutines may not read
or write the same `Buf` without external synchronization. The slice
returned by `Tail` may alias the internal window, so it must not be
accessed concurrently with any operation on the buffer either.

## Reference

The full API reference — including panic conditions, the detailed bounds
policy, and the per-method invariants — lives on
[pkg.go.dev](https://pkg.go.dev/github.com/neilotoole/tailbuf). Run
`go doc -all github.com/neilotoole/tailbuf` locally for the same content.
