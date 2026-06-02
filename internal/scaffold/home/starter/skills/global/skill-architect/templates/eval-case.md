# Template: Eval Case

Use this structure when writing `evals/evals.json` for a skill.

```json
{
  "skill_name": "skill-name",
  "evals": [
    {
      "id": 1,
      "prompt": "Realistic user prompt — specific, with context. Not 'process this file' but 'here is invoice_jan.pdf from our accounting system, extract all line items and totals into a CSV'",
      "expected_output": "Plain-language description of what success looks like",
      "files": [],
      "expectations": [
        "The output CSV contains all line items from the invoice",
        "Each row has columns: item, quantity, unit_price, total",
        "The grand total row matches the invoice total"
      ]
    }
  ]
}
```

## Assertion Quality Checklist

For each assertion, ask:
- Would this assertion fail if the skill produced completely wrong output?
- Is it checkable from the actual output files (not just the transcript)?
- Is the description specific enough to be unambiguous in the benchmark viewer?

## eval_metadata.json (per run directory)

Each run directory also gets an `eval_metadata.json`:

```json
{
  "eval_id": 1,
  "eval_name": "extract-invoice-line-items",
  "prompt": "same prompt as above",
  "assertions": [
    "The output CSV contains all line items from the invoice",
    "Each row has columns: item, quantity, unit_price, total"
  ]
}
```

The `eval_name` becomes the directory name and the section header in the viewer — make it descriptive.
