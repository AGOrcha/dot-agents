# Extract: Token Capital Efficiency (@kmad / Kevin Madura)

Source: research/articles/token-capital-efficiency.md

## Key claims
- **Token capital efficiency** = business value captured per dollar of tokens (value / (tokens x price), across reasoning/execution/learning). Almost no firm is efficient; default-to-frontier-for-everything is driving a CFO/board "token spend backlash."
- **Determinism-probabilism spectrum:** all computing sits on a line from deterministic programs to raw LLM prompts. The *what* (intent) never disappears; only specification of the *how* fades as you move right. Under-specification pulls output toward the training-data average ("every gap you leave, Claude fills with in-distribution choice").
- Coding agents work on the far-right because **tests are a boundary**; most knowledge work lacks a codified boundary, hence variable outcomes.

## Techniques / prescription
- The motion: **define -> match -> measure -> optimize.** Decompose processes into discrete tasks (well-defined inputs, criteria, measurable outputs); "wrap the probabilistic core in a deterministic shell."
- Evals become **owned, composable, company-specific IP** — the boundary that measures model performance and lets you swap models without losing the "company veteran" expertise.
- Model selection = optimization: walk down the cost curve until accuracy crosses your tolerance. Automate via DSPy/GEPA; fine-tune/RL for high-volume tasks.

## What's novel
- The coinage/framing of token capital efficiency as a *measurable enterprise metric*, tied to a learn-loop thesis: firms that build a **digital inventory of tasks + evals** compound knowledge and cost advantages.

## Mapping to our work
- **work-tracking-storage-abstraction §3A "self-improvement loop" + result->profile/skill/rule/spec correlation edges:** This is precisely Madura's "digital inventory of tasks + evals that compounds." Our typed views (operational = skills/rules/profiles; episodic = results) ARE the substrate for a token-capital-efficiency learning loop. Strong validation.
- **stage-profile consolidation + cli-runner-verifier:** "match model to task by measuring it" maps to per-stage/per-app-type profiles selecting model + verifier. Suggests our profiles should carry a **cost/capability tolerance** dimension, not just capability.
- **KG-as-SOT:** "wrap the probabilistic core in a deterministic shell" == our deterministic CLI/workflow shell around agent stages, with the KG as the durable boundary. The "what never disappears, only the how fades" framing is a clean way to articulate why intent/spec lives in the KG while the *how* (agent prose) is disposable.
- **Concrete proposal idea:** Add a `cost` / `token_budget` + `tolerance` field to stage/app-type profiles and emit per-task token spend into the episodic view, so a `da kg` query can compute token-capital-efficiency per task type and recommend model down-shifts where eval pass-rate stays above tolerance. This is the measurable "ride the cost curve" loop on top of our existing eval surface.

## Caveats
- Conceptual/strategy piece; no benchmarks of its own. Builds on a referenced Satya Nadella "token capital" article.
