# Web-a11y lens — dot-agents repo overlay (web app_type)

Composes after `reviewers/reviewer.base.md` (the contract) and `reviewers/web-a11y.md` (the lens) when
present; in this repo those upstream layers resolve empty, so this overlay is **self-sufficient** and
carries the full reviewer contract on its own.

## What this lens judges

Accessibility of the **user-facing web surface** in the diff, against web.dev / WCAG 2.1 AA guidance.
Read the changed markup/components (and, where a built UI + browser is available, corroborate with an
automated pass — axe-core via Playwright or a Lighthouse a11y run — but the lens verdict is the
judgment, not the tool score). Check:

- **Semantic HTML:** real landmarks (`header/nav/main/footer`), headings in order (single `h1`, no
  skipped levels), lists/tables/`button`/`a` used for their meaning — not `div`/`span` with click
  handlers standing in for interactive elements.
- **Names & ARIA:** every control/image/icon has an accessible name (visible label, `aria-label`,
  `alt`); form inputs tied to `<label for>`; ARIA is correct and minimal (valid role, no redundant or
  conflicting attributes) — no ARIA beats wrong ARIA.
- **Focus & keyboard nav:** every interactive element is reachable and operable by keyboard; visible
  focus indicator (no `outline:none` without a replacement); logical tab order; focus managed on
  route change / modal open+close (trap + restore); no keyboard traps.
- **Contrast:** text ≥ 4.5:1 (≥ 3:1 for large text / UI component boundaries); state not conveyed by
  color alone.
- **Tap targets:** interactive targets ≥ 24×24 CSS px (44×44 recommended) with adequate spacing.
- **Media/motion:** captions/transcripts where relevant; honors `prefers-reduced-motion`; no
  content that flashes > 3×/s.

## Verdict — APPROVE / REJECT

- **REJECT (`fail`)** on any BLOCKER/HIGH: a control with no accessible name, a keyboard-inoperable or
  focus-trapping interaction, a non-semantic element replacing a native control, contrast below
  threshold on primary content, or a form field with no programmatic label. Cite the **specific
  element + rule** (e.g. "`<div onClick>` submit button — not keyboard-focusable, no role/name;
  WCAG 4.1.2").
- **APPROVE (`pass`)** when no BLOCKER/HIGH remains; log MEDIUM/LOW (e.g. minor contrast on secondary
  text, sub-44px-but-≥24px targets) as non-blocking follow-ups.

## Anti-patterns to flag

Icon-only buttons with no label; placeholder used as the only label; `tabindex` > 0; `role="button"`
on a `div` instead of `<button>`; `outline:none` with no visible focus alternative; color-only error
signaling; images with empty/decorative `alt` that actually convey meaning; a modal that neither traps
focus nor restores it on close.

Read-only. Verdict line `(lens: web-a11y)`; `fail` on any BLOCKER/HIGH, each named with its element and
WCAG/web.dev reference.
