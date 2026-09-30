# Chirpy

A small Go web server, built as a learning project following the
[Boot.dev](https://boot.dev) backend curriculum and 
[Go net/http docs](https://pkg.go.dev/net/http#pkg-overview).

## Features

- Serves static files from `./static` (`index.html` is served at `/`)
- Reserved `/api/` route for future API handlers (currently a no-op)
- Structured logging with `log/slog`, using ISO 8601 (RFC 3339) UTC timestamps
- HTTP server timeouts and a 1 MB request header limit

## Routes

| Path      | Behaviour                                              |
|-----------|--------------------------------------------------------|
| `/api/`   | Reserved for API handlers (returns an empty response)  |
| `/`       | `http.FileServer` over `./static`; 404 for missing files |

Only the contents of `./static` are exposed. Source files such as `main.go`
are not reachable over HTTP.
