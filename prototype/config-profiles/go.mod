// Self-contained prototype module. Intentionally NOT part of the root
// go.mod / coverage gate: `go test ./...` at the repo root does not descend
// into this directory because it declares its own module path.
module proto/config-profiles

go 1.26
