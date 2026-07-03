package runner

import (
	"context"

	"github.com/AGOrcha/dot-agents/internal/eval"
	"github.com/AGOrcha/dot-agents/internal/eval/sandbox"
)

// FakeRunner is an in-memory Runner for downstream tests (harness-driver and
// beyond) that need to inject a scripted agent without shelling out to a real
// CLI. It returns its canned Result and Err verbatim, and records the last
// call's arguments so a consumer can assert what the harness passed.
//
// This is the injectable fake the plan's TASKS.yaml notes call for ("v1 ships
// a fakeRunner for tests"): a fake Runner, distinct from the per-adapter exec
// seam. Script success, failure, or cancellation by setting Result and Err;
// leave both zero for a no-op success.
type FakeRunner struct {
	// Result is returned from every Run call.
	Result Result
	// Err is returned from every Run call; when non-nil the harness treats the
	// invocation as a launch failure (Result is still returned alongside it,
	// matching the real adapters' error contract).
	Err error

	// Calls counts how many times Run was invoked.
	Calls int
	// LastSpec and LastInstance capture the most recent Run arguments so a
	// consumer can assert the harness wired the sandbox and task through.
	LastSpec     *eval.TaskSpec
	LastInstance *sandbox.Instance
}

var _ Runner = (*FakeRunner)(nil)

// Run implements Runner. It records the call and returns the canned Result and
// Err. The context is accepted to satisfy the interface; FakeRunner performs no
// work, so it neither blocks on nor observes cancellation — a consumer that
// needs to script a cancel sets Err to a context error.
func (f *FakeRunner) Run(
	_ context.Context,
	spec *eval.TaskSpec,
	instance *sandbox.Instance,
) (Result, error) {
	f.Calls++
	f.LastSpec = spec
	f.LastInstance = instance
	return f.Result, f.Err
}
