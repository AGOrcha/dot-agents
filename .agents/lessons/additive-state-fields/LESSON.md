---
name: additive-state-fields
description: Pattern for adding new data to existing state structs without breaking callers
type: feedback
---

When extending a central state struct (like `workflowOrientState`) to carry new data, always add the new field as a separate, zero-value-safe slice or pointer — never merge new concepts into existing fields.

**Why:** Merging canonical plans into `ActivePlans` would break the existing JSON contract, confuse orient consumers that rely on `active_plans` structure, and complicate the graceful degradation path. A separate `canonical_plans` field is backward-compatible, easy to nil-check, and keeps concerns separate.

**How to apply:**
- New data type → new field, never repurpose an existing field
- Use `[]T{}` (not nil) as the initial value so JSON marshals to `[]` not `null`
- Update all render/display functions that loop over state — add a new section rather than modifying existing ones
- Write one integration test that asserts the new field is populated in `collectWorkflowState()`
