---
name: sidecar-manifest-pattern
description: Store integrity hashes and metadata in a separate sidecar file, not inline in the content being hashed
type: feedback
---

When adding integrity verification (content hashing) to files that have structured headers/frontmatter, do NOT store the hash inside the frontmatter of the file being hashed — that creates a self-referential hash (the hash changes when you write it, breaking verification).

**Why:** This came up in KG Phase 6A. If the hash is stored in the note's own frontmatter, `sha256(note_file)` changes every time you write the hash into it.

**How to apply:** Use a sidecar manifest file (e.g., `ops/integrity/manifest.json`) keyed by ID. Hash only the body content (exclude the frontmatter). Update the manifest atomically after every write.

Pattern:
```go
func noteBodyHash(body string) string {
    sum := sha256.Sum256([]byte(body))
    return "sha256:" + hex.EncodeToString(sum[:])
}

// In createGraphNote / updateGraphNote:
_ = updateManifest(kgHomeDir, note.ID, body) // body only, not full file
```

The lint check re-derives the expected hash rather than trusting a cached value, because partial writes can leave the manifest stale.
