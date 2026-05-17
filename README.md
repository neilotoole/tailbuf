# tailbuf: tail, for Go objects

[![Go Reference](https://pkg.go.dev/badge/github.com/neilotoole/tailbuf.svg)](https://pkg.go.dev/github.com/neilotoole/tailbuf)
[![Go Report Card](https://goreportcard.com/badge/neilotoole/tailbuf)](https://goreportcard.com/report/neilotoole/tailbuf)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](https://github.com/neilotoole/tailbuf/blob/master/LICENSE)
![Workflow](https://github.com/neilotoole/tailbuf/actions/workflows/go.yml/badge.svg)

Package [`neilotoole/tailbuf`](https://pkg.go.dev/github.com/neilotoole/tailbuf) implements a fixed-size object tail buffer that provides a window
on the tail of items written to the buffer.

## Install

Add to your `go.mod` via `go get`:

```shell
go get github.com/neilotoole/tailbuf
```

## Usage

> [!WARNING]  
> Note that `tailbuf` is still in its `v0.0.x` infancy. There's a few things in
> the package API that probably need to be dialed in, so expect some churn.
> [Feedback](https://github.com/neilotoole/tailbuf/issues) is appreciated.

Below we'll create a [`tailbuf.Buf`](https://pkg.go.dev/github.com/neilotoole/tailbuf#Buf)
of type `string` with a capacity of `3`. You write to the buffer using [`buf.Write`](https://pkg.go.dev/github.com/neilotoole/tailbuf#Buf.Write)
or [`buf.WriteAll`](https://pkg.go.dev/github.com/neilotoole/tailbuf#Buf.WriteAll), and
you can access the tail slice using [`Buf.Tail`](https://pkg.go.dev/github.com/neilotoole/tailbuf#Buf.Tail).

```go
package main

import (
    "fmt"
    "github.com/neilotoole/tailbuf"
)

func main() {
    buf := tailbuf.New[string](3)

    buf.WriteAll("a", "b", "c")
    fmt.Println(buf.Tail())   // [a b c]

    buf.WriteAll("d", "e", "f", "g")
    fmt.Println(buf.Tail())   // [e f g]

    fmt.Println("Written:", buf.Written()) // Written: 7
}
```

When the live items do not wrap around the internal ring, `Buf.Tail` returns a
slice that aliases the buffer's storage — valid only until the next mutation
(write, pop, drop, clear, reset, `Apply`, or `Do`). When the items do wrap,
`Buf.Tail` allocates a fresh slice.

The slice returned by `Buf.Tail` is always capped at its length, so
`append`-ing to it allocates a new backing array rather than silently
overwriting the buffer's internal storage. Mutating individual elements
through the slice, on the other hand, does reach into the buffer (in the
no-wrap case) and is visible on subsequent reads.

If you want a stable snapshot regardless of wrap or future mutations, use
[`tailbuf.SliceTail`](https://pkg.go.dev/github.com/neilotoole/tailbuf#SliceTail) or
[`tailbuf.SliceNominal`](https://pkg.go.dev/github.com/neilotoole/tailbuf#SliceNominal);
both always return a freshly-allocated slice. Out-of-range upper bounds are
clipped silently rather than panicking — see the
[Bounds policy](https://pkg.go.dev/github.com/neilotoole/tailbuf#hdr-Bounds_policy)
section of the godoc for how this differs from `Buf.Peek` (which panics on
out-of-range) and for the deliberate asymmetry around negative start
values between `SliceTail` and `SliceNominal`.

> [!NOTE]
> **`Front` is the newest end; `Back` is the oldest end.** This is the
> reverse of the queue/deque convention many readers will assume:
> `PopFront` removes the most recently written item, while `PopBack`
> removes the oldest. The example below relies on this — `PopBackN(2)`
> on `[a b c]` returns `[a b]`, the two oldest.

There are various functions for popping, dropping, or peeking into the tail
buffer. `PopFront`/`PopFrontN` and `PopBack`/`PopBackN` remove and return
items from the newest and oldest ends respectively; the corresponding
`DropFront`/`DropFrontN`/`DropBack`/`DropBackN` family does the same but
discards the result instead of returning it (saving a value copy in the
singular variants and a slice allocation in the N variants).

```go
  buf := tailbuf.New[string](3)

  buf.WriteAll("a", "b", "c")
  fmt.Println(buf.Peek(0))      // a
  fmt.Println(buf.Peek(1))      // b

  fmt.Println(buf.PopBackN(2))  // [a b]  (the two oldest)
  fmt.Println(buf.Tail())       // [c]
```

There are also basic methods for interacting with the buffer:

```go
  buf := tailbuf.New[string](3)

  fmt.Println(buf.Cap())                   // 3
  fmt.Println(buf.Len())                   // 0
  buf.WriteAll("a", "b", "c")
  fmt.Println(buf.Len())                   // 3

  buf.WriteAll("d", "e", "f", "g")
  fmt.Println(buf.Len())                   // 3

  fmt.Println("Written:", buf.Written())   // 7
  buf.Reset()                              // Reset the buffer, including "written" count
  fmt.Println(buf.Len())                   // 0
  fmt.Println("Written:", buf.Written())   // 0

  buf.WriteAll("h", "i")
  fmt.Println(buf.Len())                   // 2
  fmt.Println("Written:", buf.Written())   // 2

  buf.Clear()                              // Clear is like Reset, but doesn't reset "written" count
  fmt.Println(buf.Len())                   // 0
  fmt.Println("Written:", buf.Written())   // 2
  fmt.Println("Offset:", buf.Offset())     // 2  (Clear conceptually evicts off the back, so Offset advances by the prior Len)
```


Items in the tail are addressed by *nominal index* — the position of each
item in the full stream of writes, regardless of which ones have since been
evicted. [`Buf.Bounds`](https://pkg.go.dev/github.com/neilotoole/tailbuf#Buf.Bounds)
returns the half-open nominal range currently retained;
[`Buf.InBounds`](https://pkg.go.dev/github.com/neilotoole/tailbuf#Buf.InBounds)
reports whether a given nominal index is still alive.

```go
  buf := tailbuf.New[string](3)
  buf.WriteAll("a", "b", "c", "d", "e")  // "a" and "b" have been evicted

  start, end := buf.Bounds()
  fmt.Println(start, end)              // 2 5
  fmt.Println(buf.InBounds(1))         // false (evicted)
  fmt.Println(buf.InBounds(3))         // true  ("d")
  fmt.Println(buf.Offset())            // 2 (items evicted from the back)
```

And then there's the [`Apply`](https://pkg.go.dev/github.com/neilotoole/tailbuf#Buf.Apply) method, which applies a func to each element in the buffer,
and also its bigger brother [`Do`](https://pkg.go.dev/github.com/neilotoole/tailbuf#Buf.Do), which does the same thing, but with context and
error awareness.

```go
  buf := tailbuf.New[string](3)
  buf.WriteAll("In", "Xanadu  ", "   did", "Kubla  ", "Khan")
  buf.Apply(strings.ToUpper).Apply(strings.TrimSpace)
  fmt.Println(buf.Tail()) // [DID KUBLA KHAN]
```

`Do` iterates oldest-to-newest; if the callback returns an error, iteration
halts there and items at and after that position are left untouched. The
callback also receives the item's tail-relative `index` and the buffer's
`tailOffset` (their sum is the item's nominal index).

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

The zero value of `tailbuf.Buf[T]` is a usable empty zero-capacity buffer —
it silently drops items while still incrementing `Written`. Call
[`tailbuf.New`](https://pkg.go.dev/github.com/neilotoole/tailbuf#New) to
specify a non-zero capacity.

See the [package reference](https://pkg.go.dev/github.com/neilotoole/tailbuf) for more details.
