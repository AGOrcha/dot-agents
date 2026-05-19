# macOS CI Pipefail Fix Results

## Summary

Investigated PR `#10` macOS CI failure and traced it to a workflow shell pipeline, not a Go test or runtime failure.

## Root Cause

The macOS GitHub Actions lane runs shell steps with `bash -e -o pipefail`. In `.github/workflows/test.yml`, the `Test workflow health` smoke step piped CLI output directly into `grep`:

```bash
./bin/dot-agents workflow health | grep -qiE "(healthy|health)"
```

Because `grep` exits as soon as it finds a match, the producer can receive `SIGPIPE`. Under `pipefail`, that surfaced as exit code `141`, failing the job even though `workflow health` printed healthy output.

## Implementation

- Replace CLI smoke assertions that used `command | grep` with `output="$(command)"` plus `grep <<<"$output"`.
- Keep the command output visible in CI logs with `printf '%s\n' "$output"`.

## Verification

- Reproduced the failing behavior locally with:

```bash
/bin/bash --noprofile --norc -e -o pipefail -c './bin/dot-agents workflow health | grep -qiE "(healthy|health)"'
```

- Verified the updated pattern passes under the same shell settings.
- Verified the other updated smoke assertions pass under `bash -e -o pipefail`:
  - `./bin/dot-agents --version`
  - `./bin/dot-agents --help`
  - `./bin/dot-agents kg --help`
- Validated `.github/workflows/test.yml` parses successfully with Ruby YAML.
