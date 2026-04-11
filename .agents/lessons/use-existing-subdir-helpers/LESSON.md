---
name: use-existing-subdir-helpers
description: When iterating over note types in kg.go, use noteSubdir(t) rather than noteType+"s" — "entity" maps to "entities", not "entitys".
type: feedback
---

`commands/kg.go` has a `noteSubdir(noteType string) string` helper that handles the irregular English plurals used as directory names (e.g. `entity → entities`, `synthesis → synthesis`). Any new code that needs to map a note type to its filesystem subdirectory must call this function instead of appending "s".

**Why:** When implementing `runKGWarm`, the type filter used `noteTypeFilter + "s"` to derive the subdir, producing `entitys` which does not exist. The test for `--type entity` passed 0 notes instead of 2. `noteSubdir` already exists precisely to avoid this problem.

**How to apply:** Before writing `noteType + "s"` anywhere in `commands/kg.go`, search for `noteSubdir` first. If you are iterating all note types, use the `allTypes` slice with `noteSubdir(t)` to build the directory list.
