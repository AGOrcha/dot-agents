# Followup: canonical lesson detail pages render 0 `<h1>`

**Surfaced:** 2026-05-28 by PR #165 (docs-ux-sidebar-h1-collision) verification pass.
**Severity:** P1 (a11y + SEO; not blocking).
**Type:** project-local followup (per `[[proposal-routing]]`).

## Finding

After PR #165 fixed the double-`<h1>` violation (sidebar brand demoted to `<span>`),
a full build (123 pages) showed:

- 110 pages: exactly one `<h1>` (correct)
- **13 canonical lesson detail pages: zero `<h1>`**

The 13 lesson pages render no top-level heading because their markdown source
(`.agents/lessons/<name>/LESSON.md`) starts at `##` (h2) — the lesson body
convention uses h2+ only, with the lesson title living in the file's first line
as `# Title` OR in the index, not as a rendered page h1.

## Why it matters

- **Accessibility:** screen-reader users rely on a single h1 per document for
  landmark navigation. Zero h1 is as bad as two — the page has no announced title.
- **SEO:** canonical docs at agorcha.dev want h1 for indexing. Zero h1 weakens
  the page's search ranking signal.

## Root cause options

1. The lesson-page Astro template doesn't synthesize an `<h1>` from frontmatter
   `title:` (or the lesson slug) — it just renders the markdown body, which has
   no h1.
2. The LESSON.md files use `# Title` as line 1, but the collection loader strips
   or demotes it.

## Fix shape (for the followup task)

- The canonical lesson-detail Astro layout should render an `<h1>` from the
  lesson's title (frontmatter `title:` or derived from the slug), ABOVE the
  markdown body — same pattern the demo pages now use after #166.
- Verify all 13 lesson pages render exactly one h1 post-fix.
- Cross-check the other canonical doc types (specs, proposals if rendered) for
  the same gap.

## Scope (estimated)

Single Astro layout file under `docs/web/src/pages/` (the lesson/canonical
detail route) + a build-time assertion. Small.

## Cross-references

- PR #165 (the verification that surfaced this)
- PR #166 (`docs-ux-demo-title-fallback`) — the analogous demo-title h1 source pattern
- `.agents/proposals/docs-site-usability-review.md` (parent UX review)
