# A test that drives a real credstore.NewLoader() hangs on macOS CI (Keychain)

## Pattern

A Go test passes locally on Linux and in `go test` on a dev box, passes on the
ubuntu + windows CI shards, but the **macOS CI shard times out** (panic: "test
timed out after 5m0s") with the stack ending in
`credstore.(*Loader).Resolve` / `docsaccess.(*Client).resolveRequired`.

Symptom in the failing log:
```
panic: test timed out after 5m0s
...(*Client).resolveRequired(...)  client.go:161
...(*Client).Decorate(...)         client.go:141
...TestNewNilResolverFallsBackToLoader  client_test.go:366
FAIL  github.com/.../internal/docsaccess  300.060s
```

## Root cause

`credstore.NewLoader()` enables the OS keyring step **only on Darwin**
(`NewOSKeyring()` returns a real macOS Keychain handle on macOS, `nil` elsewhere).
When a test constructs a real loader (e.g. via the `New(WithResolver(nil))`
fallback to `credstore.NewLoader()`) and then actually drives a credential
resolution through it, the encrypted-store step hits the **macOS Security
Service**, which blocks waiting on an interactive Keychain prompt that never
arrives on a headless CI runner — so the test hangs to the suite timeout.

On ubuntu/windows there is no keyring, so the same code path returns a fast miss
and the test passes — which is why this is invisible until the macOS shard runs.

## Rule

Do NOT drive a credential **resolution** through a real `credstore.NewLoader()`
in a unit test. Two safe options:

1. **Assert wiring by TYPE, not by behavior.** To prove a fallback installs a
   real loader, assert the type and stop:
   ```go
   if _, ok := c.resolver.(*credstore.Loader); !ok {
       t.Fatalf("expected *credstore.Loader fallback, got %T", c.resolver)
   }
   ```
   Do not then call the method that resolves through it.

2. **Exercise the resolution chain hermetically** with the env/file steps that
   precede the keyring step: a fake resolver, or `credstore.NewLoader(
   WithStorePath(tmp), WithKeyring(nil))` pointed at a `t.TempDir()` plaintext
   `DA_CREDENTIALS_FILE`. `WithKeyring(nil)` disables the OS-keychain step, so the
   chain stays in-process and never touches the Security Service.

Coverage of a defensive fallback line is fully achievable with the type-assertion
(option 1) — you do not need to call into the keyring to hit 100%.

## Related

- `match-ci-test-flags-locally` — local pass / CI fail divergence.
- The credstore loader chain: env (`DA_CREDENTIAL_<id>`) -> `DA_CREDENTIALS_FILE`
  plaintext -> encrypted store (keychain) -> OIDC stub. The keychain step is the
  macOS hang; the first two are hermetic.
