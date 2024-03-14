# tailbuf: tail, for Go objects

[![Go Reference](https://pkg.go.dev/badge/github.com/neilotoole/tailbuf.svg)](https://pkg.go.dev/github.com/neilotoole/tailbuf)
[![Go Report Card](https://goreportcard.com/badge/neilotoole/tailbuf)](https://goreportcard.com/report/neilotoole/tailbuf)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](https://github.com/neilotoole/tailbuf/blob/master/LICENSE)
![Workflow](https://github.com/neilotoole/tailbuf/actions/workflows/go.yml/badge.svg)

Package `tailbuf` implements a fixed-size object tail buffer that provides a window
on the tail of items written to the buffer.

## Install

Add to your `go.mod` via `go get`:

```shell
go get github.com/neilotoole/tailbuf
```

## Usage

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
    fmt.Println(buf.Tail()) 	// [a b c]

    buf.WriteAll("d", "e", "f", "g")
    fmt.Println(buf.Tail()) 	// [e f g]

    fmt.Println("Written:", buf.Written()) // Written: 7
}
```

Note that `Buf.Tail` returns a slice into the buffer's internal storage, so it's
only valid until the next write operation. If you need to retain the tail slice,
you should copy the returned slice, or instead use [`tailbuf.SliceTail`](https://pkg.go.dev/github.com/neilotoole/tailbuf#SliceTail), which
always returns a freshly-allocated slice.

There are various functions for popping, dropping, or peeking into the tail buffer.

```go
  buf := tailbuf.New[string](3)

  buf.WriteAll("a", "b", "c")
  fmt.Println(buf.Peek(0))      // a
  fmt.Println(buf.Peek(1))      // b

  fmt.Println(buf.PopBackN(2))  // [a b]
  fmt.Println(buf.Tail())       // [c]
```

There's also the [`Apply`](https://pkg.go.dev/github.com/neilotoole/tailbuf#Buf.Apply) method, which applies a func to each element in the buffer,
and also its bigger brother [`Do`](https://pkg.go.dev/github.com/neilotoole/tailbuf#Buf.Do), which does the same thing, but with context and
error awareness.

```go
  buf := tailbuf.New[string](3)
  buf.WriteAll("In", "Xanadu  ", "   did", "Kubla  ", "Khan")
  buf.Apply(strings.ToUpper).Apply(strings.TrimSpace)
  fmt.Println(buf.Tail()) // [DID KUBLA KHAN]
```

See the [package reference](https://pkg.go.dev/github.com/neilotoole/tailbuf) for more details.
