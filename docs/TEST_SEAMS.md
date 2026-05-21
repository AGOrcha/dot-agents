# Test Seams — Canonical Convention

How to make an error/branch path provable in tests when it cannot be
triggered through real inputs (e.g. `os.MkdirAll` never fails on a
writable tmp dir; `json.Marshal` of a fixed struct never errors).

## Rule

**Use interface-based dependency injection (interface-DI).** A narrow
collaborator interface, a production struct implementation backed by the
real call, injected via constructor / struct field / explicit parameter.
A test passes a fake.

**Do not add package-level function-variable seams** —
`var osX = os.X` swapped in tests (`withXStub(t, …)` save/restore). That
pattern is legacy. It is still present in some packages only because it
predates this convention and is being migrated out
(`.agents/workflow/plans/seam-interface-di-migration/`); its prevalence
is not endorsement. Do not extend it, and do not convert a single file
to interface-DI in isolation inside an otherwise-func-var package —
that is its own inconsistency; convert the whole package as one unit.

`prefer-test-seam-over-untestable` still holds: a seam beats a
`[defensive-unreachable]` coverage-exception entry. Only the seam
*shape* is mandated here.

## Canonical shape

A collaborator interface scoped to exactly the operations the unit needs
(not a grab-bag), a `std…`/`os…` production impl, injection at the
narrowest site that stays out of production cascade.

```go
// dirCleaner is the narrow filesystem collaborator emptyProjectDirs needs.
type dirCleaner interface {
	ReadDir(name string) ([]os.DirEntry, error)
	RemoveAll(path string) error
}

type osDirCleaner struct{}

func (osDirCleaner) ReadDir(n string) ([]os.DirEntry, error) { return os.ReadDir(n) }
func (osDirCleaner) RemoveAll(p string) error                { return os.RemoveAll(p) }

func emptyProjectDirs(dc dirCleaner, project string) error { … } // prod passes osDirCleaner{}
```

### Reference implementations in this tree

| Shape | Where |
|---|---|
| Narrow collaborator, param-injected, prod passes `osDirCleaner{}` | `commands/remove.go` (`dirCleaner` / `osDirCleaner`) |
| Collaborator threaded through a `sync.Once` wrapper; prod passes `stdSchemaCompiler{}`, no production cascade | `commands/workflow/seams.go` (`schemaCompiler` / `stdSchemaCompiler`) |
| First-class injected collaborator (constructor) | `internal/graphstore/store.go` `NewHandle(store Store)` |

### Naming

- **Multi-method interfaces** (most cases): name by the *role* the
  collaborator plays — `dirCleaner`, `schemaCompiler`, `Store`. No
  `-er` suffix required when the role name already reads as a noun
  ending in something else.
- **Single-method interfaces**: follow Go style and use the method
  name plus `-er` (Sonar S8196 enforces this). Prefix with the file
  it serves to keep per-file scope explicit when multiple files would
  otherwise collide on the same name — e.g. `initDirMaker` (init.go's
  MkdirAll seam). Rename to a multi-method role name if the interface
  grows additional methods later.

### Choosing the injection site

- **Constructor / struct field** when the unit is a type — inject once,
  store the interface, production wires the real impl at construction.
- **Explicit parameter** when the unit is a free function (Cobra
  handlers, `compiled<Name>Schema()` once-wrappers). Pass the interface;
  production call sites pass the `std…` value. Thread it only as far as
  needed — pick the narrowest function whose callers are test + a single
  production line, so the change does not cascade through handler
  signatures. (See `compiled<Name>Schema(sc schemaCompiler)`: only
  `validate<Name>` + tests call it, so the request path never changes.)
- Never substitute a swappable package-level interface *variable*
  (`var fs fileSystem = osFS{}` overridden in tests). That is a
  func-var seam wearing an interface — same anti-pattern.

## Test side

A fake whose nil func-fields delegate to the real implementation, so a
test overrides only the operation it wants to fault-inject:

```go
type fakeDirCleaner struct {
	readDir   func(string) ([]os.DirEntry, error)
	removeAll func(string) error
}

func (f fakeDirCleaner) ReadDir(n string) ([]os.DirEntry, error) {
	if f.readDir != nil { return f.readDir(n) }
	return os.ReadDir(n)
}
// …RemoveAll likewise
```

Drive both the happy path (real delegation) and each error branch
(one overridden func returning a sentinel) — assert the caller wraps/
propagates as the code does. Table-drive when one unit has many
delegators (see `lazy_test.go`).

## Goroutine-lifecycle seams

If the seam involves a goroutine the unit must await on shutdown, track
it with a `sync.WaitGroup` **serialised by a mutex + `closed` flag** so
every `Add` is ordered-before `Wait` — a lazily-spawned `Add` racing
`Close()`'s `Wait()` is a real data race (`go test -race` will flag it).
Reference: `SQLiteStore` reaper shutdown in `internal/graphstore/sqlite.go`.
Verify such changes locally with CI's exact flags (`-race -count=1`).

## Migration status

`commands/workflow` (done), the `commands.dirCleaner` slice of
`commands/remove.go`, and `internal/graphstore` collaborators are on the
new shape. Remaining func-var packages — `commands/`, `commands/kg/`,
`commands/skills/`, `commands/agents/`, `internal/platform/` — are
tracked in `seam-interface-di-migration` (one reviewable unit each).

Related: lesson `prefer-interface-di-over-funcvar-seams`; lesson
`match-ci-test-flags-locally`.
