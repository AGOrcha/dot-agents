---
name: test-string-count-ids
description: Avoid strings.Count(content, id) in tests when the ID appears multiple times in a single valid entry (e.g., in link anchor AND file path)
type: feedback
---

When testing that a file entry is not duplicated, do not use `strings.Count(content, id)` if the ID appears more than once in a single valid entry.

**Why:** In link-format entries like `- [dec-001](notes/decisions/dec-001.md): Title — Summary`, the ID `dec-001` appears twice: once in the anchor text and once in the file path. So `strings.Count(content, "dec-001") == 2` even for a single, correct entry — causing a spurious test failure.

**How to apply:** Count a uniquely-occurring prefix of the entry instead:
```go
// Bad
strings.Count(content, "dec-001") != 1

// Good — the list item prefix appears exactly once per entry
strings.Count(content, "- [dec-001]") != 1
```

Apply this whenever testing for duplicate suppression in markdown index files with link-format entries.
