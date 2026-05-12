# Review Delta - Common Failure Points

1. **Skipping `kg update` first.** The graph must be current before trusting `kg changes` / `kg impact` / bridge queries.

2. **Pasting full files.** Use `kg changes` and targeted `kg bridge query` output; open full files only when necessary.

3. **Ignoring test gaps.** Use `kg bridge query --intent tests_for <symbol>` for changed functions when reviewing risk.

4. **Underestimating widely-used symbols.** When impact spans many dependents, treat the change as high risk regardless of diff size.
