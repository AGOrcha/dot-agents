# First-class resource-family docs — design (spec)

**Spec id:** `first-class-resource-docs`
**Status:** design artifact (spec tier). Plan: `workflow/plans/first-class-resource-docs/`.

## 1. Problem & why

dot-agents manages several **resource families** — config, hooks, skills, agents,
MCP, rules, plugins — each with its own `da` command surface. But user-facing doc
coverage is uneven and code-bound:

- **config** — only `CONFIG_RELEVANCE.md` (a narrow sub-topic); no guide to the
  layered model (`extends`/sources/`.agentsrc.lock`). Closed by the 0.4.0 layered-config guide.
- **hooks** — `HOOKS.md` (contract-shaped, not a user guide).
- **skills** — `SKILL_COMMAND_INTEGRATION.md` (integration map, not a how-to).
- **plugins** — `PLUGIN_CONTRACT.md` (contract).
- **agents / mcp / rules** — **no dedicated doc at all.**

The existing docs skew toward `*_CONTRACT.md` / `*_SPEC.md` (authoritative for
implementers) rather than first-class, task-oriented guides a *user* follows. A
reader today often has to dig into the code to understand how a family actually
works. That's the gap this initiative closes.

## 2. Decisions

- **One first-class guide per resource family**, surfaced in the Starlight docs
  site (the `Guides` section) and linked from README.
- **Shared template** (set by the layered-config guide, the exemplar): `Overview`
  → `Model` (the data/resource shape) → `Commands` (the `da <family>` surface) →
  `Worked example` (a real, runnable walkthrough with output) → `Reference`
  (links to the contract/spec doc for implementers). Guides are task-oriented;
  the `*_CONTRACT.md`/`*_SPEC.md` docs remain the implementer-authoritative source
  the guide's Reference section points at (no duplication of contract detail).
- **Public by default**; gate internal-only families per the docs visibility
  model (dm3) if any are maintainer-only.
- **Verify claims against code** (`da <family> --help` + the family's package) —
  never document unshipped behavior.
- **Contracts stay; guides are additive.** This does not rewrite the contract
  docs; it adds the missing user-facing layer and cross-links the two.

## 3. Family scope & current coverage

| Family | Today | Guide needed |
|--------|-------|--------------|
| config | CONFIG_RELEVANCE.md (narrow) | layered-config guide — 0.4.0 (exemplar) |
| hooks | HOOKS.md (contract) | user guide (events → platforms, authoring) |
| skills | SKILL_COMMAND_INTEGRATION.md | user guide (authoring, promote, graph) |
| agents | none | user guide |
| mcp | none | user guide |
| rules | none | user guide |
| plugins | PLUGIN_CONTRACT.md | user guide (PLUGIN.yaml, emit) |

`workflow` and `kg` already carry substantial docs (WORKFLOW_CLIENT_COMMANDS,
KNOWLEDGE_GRAPH_SUBPROJECT_SPEC + the README KG pillar); assess whether they need
a guide in this shape or are adequately covered — out of the initial set.

## 4. Done criteria

1. Each in-scope family has a first-class guide following the shared template,
   present in the docs site sidebar and linked from README.
2. A new user can perform the family's core tasks from the guide without reading code.
3. agents / mcp / rules — previously undocumented — each have a guide.
4. Each guide's commands/claims are verified against `da <family> --help` + code;
   the guide cross-links its contract/spec doc rather than duplicating it.

## 5. Deferred / out of scope

- Rewriting the `*_CONTRACT.md`/`*_SPEC.md` docs (they remain the implementer source).
- `workflow`/`kg` guides (assess separately; already substantially documented).
- Auto-generating guides from the command tree (a future nicety, not required).

## 6. Relationship to other work

- **config guide** (the 0.4.0 layered-config guide) is the exemplar that sets the
  template; the other families follow it.
- Consumes the `RESOURCE_COMMAND_CONTRACT.md` shared-surface contract and each
  family's contract/spec doc as the Reference target.
