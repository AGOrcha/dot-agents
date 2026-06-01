# Optimize Mode

Optimize the skill's `description` field for trigger accuracy — so Claude invokes the skill when it should and skips it when it shouldn't.

## How Triggering Works

Skills appear in Claude's `available_skills` list with their name + description. Claude decides whether to consult a skill based on that description alone. Important: Claude only consults skills for tasks it can't easily handle itself — simple, one-step queries may not trigger even with a perfect description. Complex, multi-step, or specialized queries reliably trigger when the description matches.

This means eval queries should be substantive enough that Claude would actually benefit from consulting a skill.

---

## Step 1: Generate Trigger Eval Queries

Create 20 queries — a mix of should-trigger and should-not-trigger. Save as JSON:

```json
[
  {"query": "the user prompt", "should_trigger": true},
  {"query": "another prompt", "should_trigger": false}
]
```

**Quality bar:** Queries must be realistic and specific. Include file paths, personal context, column names, company names, URLs. A little backstory. Mix lengths. Focus on edge cases, not clear-cut cases.

Bad: `"Format this data"`, `"Create a chart"`
Good: `"ok so my boss just sent me this xlsx (Q4 sales final FINAL v2.xlsx) and she wants a profit margin column — revenue is in C, costs in D i think"`

**Should-trigger queries (8-10):** Different phrasings of the same intent. Formal and casual. Cases where the user doesn't name the skill explicitly but clearly needs it. Uncommon use cases. Cases where this skill competes with another skill but should win.

**Should-not-trigger queries (8-10):** Near-misses — queries that share keywords or concepts but actually need something different. Ambiguous phrasing where naive keyword matching would trigger but shouldn't. Don't use obviously irrelevant queries ("write a fibonacci function" as a negative for a PDF skill is too easy and tests nothing).

---

## Step 2: Review with User

Use the HTML template to let the user review and edit the eval set:

1. Read `assets/eval_review.html`
2. Replace placeholders:
   - `__EVAL_DATA_PLACEHOLDER__` → the JSON array (no quotes — it's a JS variable)
   - `__SKILL_NAME_PLACEHOLDER__` → skill name
   - `__SKILL_DESCRIPTION_PLACEHOLDER__` → current description
3. Write to `/tmp/eval_review_<skill-name>.html` and open: `open /tmp/eval_review_<skill-name>.html`
4. User edits queries, toggles should-trigger, clicks "Export Eval Set"
5. File downloads to `~/Downloads/eval_set.json` — check for the most recent version

This step matters. Bad eval queries produce bad descriptions.

---

## Step 3: Run the Optimization Loop

Tell the user: "This will take some time — I'll run the optimization loop in the background."

Save the eval set to the workspace, then run from the skill-architect directory:

```bash
python -m scripts.run_loop \
  --eval-set <path-to-trigger-eval.json> \
  --skill-path <path-to-skill> \
  --model <model-id-powering-this-session> \
  --max-iterations 5 \
  --verbose
```

Use the model ID from your system prompt so the trigger test matches what the user actually experiences.

While it runs, periodically tail the output to give the user progress updates.

The loop automatically:
- Splits the eval set into 60% train / 40% held-out test
- Evaluates each description candidate (3 runs per query for reliability)
- Calls Claude to propose improvements based on failures
- Selects the best description by test score (not train, to avoid overfitting)

---

## Step 4: Apply the Result

Take `best_description` from the JSON output and update the skill's SKILL.md frontmatter. Show the user before/after and report the scores.
