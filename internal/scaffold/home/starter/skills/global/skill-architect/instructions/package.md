# Package Mode

Validate and package a skill as a distributable `.skill` file.

## Steps

**1. Validate** — Run the quick validation script from the skill-architect directory:

```bash
python -m scripts.quick_validate <path-to-skill>
```

This checks:
- SKILL.md exists and has valid YAML frontmatter
- `name` is present and kebab-case (max 64 chars)
- `description` is present, no angle brackets, max 1024 chars
- No unexpected frontmatter keys

Fix any validation errors before proceeding.

**2. Package:**

```bash
python -m scripts.package_skill <path-to-skill> [output-directory]
```

This creates `<skill-name>.skill` (a zip file) in the current directory or specified output directory. The packager excludes:
- `__pycache__/`, `node_modules/`, `*.pyc`, `.DS_Store`
- `evals/` at the skill root (not needed for distribution)

**3. Present the result** — Tell the user the path to the `.skill` file. If `present_files` tool is available, use it to offer the file for download.

## Notes

- The skill name in the output filename comes from the directory name, not the frontmatter
- If the skill directory is read-only, copy it to `/tmp/<skill-name>/` before packaging
- Preserve the original `name` frontmatter when updating an existing published skill — don't rename to v2
