---
name: verify-managed-file-target
description: When a task touches a managed file path in a dot-agents repository, first use the intended managed generation path; if a repo-local file is still required, verify the final path is not a generated managed symlink before closing the task
type: feedback
---

In a dot-agents-managed repository, files like `AGENTS.md` are usually generated through the normal management flow rather than authored directly in-place. If the task is to add or update one of these paths, first determine whether the correct fix is to generate or import the managed source through the repo’s intended command or skill flow. Only create a stand-alone repo file when that is explicitly the desired end state.

**Why:** A task can appear complete while the user-facing path still points at generated global content, or while a manual file masks the managed workflow the repo expects. Either mistake creates drift and makes the result fragile across future `refresh` or `/init` runs.

**How to apply:**
1. Before editing a managed path like `AGENTS.md`, confirm the intended source of truth: managed generation/import flow or a repo-local file.
2. Prefer the repo’s command or skill flow for managed outputs, such as the `/init`-driven setup path, instead of writing the final rendered file by hand.
3. If the requested outcome is a repo-local file, check the final path with `ls -l <file>` or equivalent to verify it is not still a generated symlink or hard link.
4. When reporting completion, state whether the file now comes from managed generation or from repo-local content so the next agent does not assume the wrong ownership model.
