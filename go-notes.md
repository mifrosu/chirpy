# Go Notes

## `MaxHeaderBytes` and `1 << 20`

From `main.go`:

```go
server := &http.Server{
	Addr:           ":" + port,
	Handler:        mux,
	ReadTimeout:    10 * time.Second,
	WriteTimeout:   10 * time.Second,
	MaxHeaderBytes: 1 << 20,
}
```

### `MaxHeaderBytes:`

A field name in the `http.Server` struct literal. Go uses `field: value` syntax when building a struct.

It caps the number of bytes the server will read when parsing request headers, including the request line and all header lines. If a client sends more, the server responds with `431 Request Header Fields Too Large`.

### `1 << 20`

A bitwise left shift. It shifts the bits of `1` left by 20 places, which is the same as 2^20.

| Expression | Value     | Size |
|------------|-----------|------|
| `1 << 10`  | 1,024     | 1 KB |
| `1 << 20`  | 1,048,576 | 1 MB |

So the limit here is 1 MB. Writing sizes as shifts is a common idiom, because it's easier to read than `1048576`. The standard library uses the same value as its default: `http.DefaultMaxHeaderBytes = 1 << 20`. Setting it explicitly just makes the limit visible.

Constant expressions like this are evaluated at compile time, so they cost nothing at runtime.

## Power operations in Go

Go has no `**` operator (Python's `2**20`). The options are below.

### 1. Bit shift (powers of 2 only, integers)

```go
1 << 20  // 1048576
```

This is the idiomatic choice for powers of two. It is evaluated at compile time when the operands are constants, so it works in `const` declarations and struct literals.

### 2. `math.Pow` (any base, floats)

```go
import "math"

math.Pow(2, 20)       // 1.048576e+06 (float64)
int(math.Pow(2, 20))  // cast to int
```

- Takes and returns `float64`, so integer results need a cast.
- Floats can lose precision for large results.
- It runs at runtime, so it is **not** a constant expression and can't be used in a `const` declaration.

### 3. Loop or helper (integer powers, any base)

```go
func ipow(base, exp int) int {
	result := 1
	for i := 0; i < exp; i++ {
		result *= base
	}
	return result
}

ipow(2, 20)
```

The standard library has no integer power function. `math/big` has `Exp`, but it's overkill for most cases.

### Quick reference

| Need                          | Use                   |
|-------------------------------|-----------------------|
| Power of 2, integer/constant  | `1 << n`              |
| Any base, float result        | `math.Pow(x, y)`      |
| Any base, exact integer       | custom `ipow` helper  |
| Huge integers                 | `math/big` (`Exp`)    |
