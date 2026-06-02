# Eval Mode

Run test cases against a skill, grade the outputs, and launch an interactive review viewer.

## Setup

Put workspace results in `<skill-name>-workspace/` as a sibling to the skill directory. Within the workspace, organize by iteration (`iteration-1/`, `iteration-2/`, etc.), and within that, each test case gets a directory named descriptively (e.g., `extract-invoice-fields/` not `eval-0/`).

Save test cases to `evals/evals.json` inside the skill directory. See `references/schemas.md` for the full schema. Minimal structure:

```json
{
  "skill_name": "example-skill",
  "evals": [
    {
      "id": 1,
      "prompt": "User's task prompt",
      "expected_output": "Description of expected result",
      "files": [],
      "expectations": []
    }
  ]
}
```

---

## Step 1: Spawn all runs in the same turn

For each test case, spawn two subagents simultaneously — one with the skill, one without. Never spawn with-skill runs first and then baseline runs later.

**With-skill run prompt:**
```
Execute this task:
- Skill path: <path-to-skill>
- Task: <eval prompt>
- Input files: <eval files if any, or "none">
- Save outputs to: <workspace>/iteration-<N>/<eval-name>/with_skill/outputs/
- Outputs to save: <what the user cares about>
```

**Baseline run** (save to `without_skill/outputs/` for new skills; `old_skill/outputs/` when improving an existing one — point it at a pre-edit snapshot).

Write an `eval_metadata.json` for each test case directory:
```json
{
  "eval_id": 1,
  "eval_name": "descriptive-name",
  "prompt": "The user's task prompt",
  "assertions": []
}
```

---

## Step 2: Draft assertions while runs are in progress

Don't wait idle. Draft quantitative assertions for each test case and explain them to the user.

Good assertions are:
- Objectively verifiable
- Discriminating — they pass for correct output, fail for wrong output
- Named descriptively so they read clearly in the benchmark viewer

Update `eval_metadata.json` and `evals/evals.json` with assertions once drafted.

Subjective skills (writing style, design quality) — don't force assertions. Focus on qualitative human review.

---

## Step 3: Capture timing data

When each subagent task completes, save the notification's `total_tokens` and `duration_ms` immediately to `timing.json` in the run directory:

```json
{
  "total_tokens": 84852,
  "duration_ms": 23332,
  "total_duration_seconds": 23.3
}
```

This data only exists in the task notification — it cannot be recovered afterward.

---

## Step 4: Grade, aggregate, and launch the viewer

Once all runs complete:

**1. Grade each run** — spawn a grader subagent (read `agents/grader.md`) or grade inline. Save results to `grading.json` in each run directory. The `expectations` array must use `text`, `passed`, and `evidence` fields exactly — the viewer depends on these names.

For assertions that can be checked programmatically, write and run a script rather than eyeballing.

**2. Aggregate into benchmark:**
```bash
python -m scripts.aggregate_benchmark <workspace>/iteration-N --skill-name <name>
```
This produces `benchmark.json` and `benchmark.md`.

**3. Analyst pass** — read `agents/analyzer.md` (the "Analyzing Benchmark Results" section) and surface patterns the aggregate stats hide: non-discriminating assertions, high-variance evals, time/token tradeoffs.

**4. Launch the viewer:**
```bash
nohup python <skill-architect-path>/eval-viewer/generate_review.py \
  <workspace>/iteration-N \
  --skill-name "skill-name" \
  --benchmark <workspace>/iteration-N/benchmark.json \
  > /dev/null 2>&1 &
VIEWER_PID=$!
```
For iteration 2+, add `--previous-workspace <workspace>/iteration-<N-1>`.

**Headless/no-display environments:** Use `--static <output_path>` to write a standalone HTML file instead of starting a server. Feedback will be downloaded as `feedback.json` when the user clicks "Submit All Reviews".

**IMPORTANT:** Generate the viewer and get outputs in front of the human BEFORE evaluating results yourself. The human review comes first.

**5. Tell the user:** "I've opened the results in your browser. The 'Outputs' tab lets you click through each test case and leave feedback; 'Benchmark' shows quantitative comparison. Come back when you're done."

---

## Step 5: Read the feedback

When the user is done, read `feedback.json`:
```json
{
  "reviews": [
    {"run_id": "eval-0-with_skill", "feedback": "chart is missing axis labels"},
    {"run_id": "eval-1-with_skill", "feedback": ""}
  ],
  "status": "complete"
}
```

Empty feedback = the user thought it was fine. Focus improvements on cases with specific complaints.

Kill the viewer server when done:
```bash
kill $VIEWER_PID 2>/dev/null
```

---

## Blind Comparison (advanced)

For rigorous A/B comparison between two skill versions, read `agents/comparator.md` and `agents/analyzer.md`. An independent agent judges quality without knowing which version produced which output. Optional — the human review loop is sufficient for most cases.
