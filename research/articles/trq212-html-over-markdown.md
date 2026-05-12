## Thariq (Claude Code @ Anthropic) — Using Claude Code: The Unreasonable Effectiveness of HTML

**Source**: https://x.com/trq212/status/2052809885763747935
**Author**: Thariq (@trq212) — Verified; Claude Code @ Anthropic, prev YC W20
**Date**: 2026-05-08
**Method**: Playwright
**Word count**: ~1,800 words
**Engagement**: 352 replies, 1,035 reposts, 5,579 likes, 10,506 bookmarks, 2.3M views

---

### Summary

Thariq (Claude Code team at Anthropic) advocates replacing Markdown with HTML as the primary output format for agent-produced artifacts. Reasons: HTML has vastly higher information density (CSS, SVG, JavaScript, tables, interactions), is easier to read at >100 lines, is shareable via URL, and enables two-way interaction (sliders, copy-as-prompt). Use cases: specs, code review explainers, designs, reports, custom editing interfaces. Key caveat: version-control diffs are noisier with HTML. Notable: 2.3M views, the most viral article in this corpus.

---

### Body

Markdown has become the dominant file format used by agents to communicate with us. It's simple, portable, has some rich text capability and is easy to edit. But as agents have become more and more powerful, markdown has become a restricting format. It's difficult to read a markdown file of more than a hundred lines.

I've started preferring HTML as an output format instead of Markdown and increasingly see this being used by others on the Claude Code team.

**Why HTML?**

**Information Density.** HTML can represent: tabular data (tables), design data (CSS), illustrations (SVG), code snippets (script tags), interactions (HTML elements with JavaScript + CSS), workflows (SVG and HTML), spatial data (absolute positions and canvases), images (image tags). "There is almost no set of information that Claude can read that you cannot fairly efficiently represent with HTML."

**Visual Clarity.** HTML documents are much easier to read — Claude can organize structure visually with tabs, illustrations, links. Can be mobile responsive.

**Ease of Sharing.** Markdown files are hard to share; browsers don't render them natively. With HTML, upload to S3, share the link. The chance of someone actually reading your spec or PR writeup is much higher if it's HTML.

**Two-way Interaction.** HTML can allow sliders, knobs, and controls to adjust designs or algorithms. End with a "copy as JSON" or "copy as prompt" button to export changes back into Claude Code.

**Data Ingestion.** Claude Code can read the full filesystem, MCPs (Slack, Linear, etc.), browser context, git history — then synthesize into rich HTML. "The diagrams you see in this article are a direct result of that."

**Use Cases:**

*Specs, Planning & Exploration:* Instead of a simple markdown plan, build a web of HTML files — explorations of different options, then mockups, then implementation plan. Pass all files into a new session for implementation.

*Code Review & Understanding:* Render diffs, annotations, flowcharts, modules in HTML. Attach an HTML code explainer to every PR. Often works better than the default GitHub diff view.

*Design & Prototypes:* Claude Design is based on HTML. Prototype interactions with sliders/knobs; ask Claude to generate multiple variations side-by-side.

*Reports, Research & Learning:* Synthesize across Slack, codebase, git history, internet into readable reports. Slideshow/deck format, SVG diagrams.

*Custom Editing Interfaces:* Build throwaway HTML editors for structured data. Always end with "copy as JSON" or "copy as prompt" export button.

Example: "I need to reprioritize these 30 Linear tickets. Make me an HTML file with each ticket as a draggable card across Now / Next / Later / Cut columns. Add a 'copy as markdown' button."

**FAQ:**

Q: Isn't it less token efficient?
A: Yes, but the added expressiveness and much higher likelihood of me reading it means better overall output. With 1MM context window in Opus 4.7, increased token usage is not noticeable.

Q: What about version control?
A: "This is honestly one of the biggest downsides of HTML, HTML diffs are noisy and hard to review compared to Markdown."

Q: How do I get Claude to match my style?
A: Create a single design system HTML file by pointing Claude at your codebase. Use that as a reference for other HTML files.

**Core principle:** HTML is not about a skill or a technique; it's about staying more in the loop. "I feel much more in the loop than ever before when using HTML."

---

### Key Quotes

> "There is almost no set of information that Claude can read that you cannot fairly efficiently represent with HTML."

> "The chance of someone actually reading your spec, report or PR writeup is much much higher if it's in HTML."

> "This is honestly one of the biggest downsides of HTML, HTML diffs are noisy and hard to review compared to Markdown."

> "HTML is that I feel much more in the loop with Claude. I had begun to fear that because I had stopped reading plans in depth I would simply have to leave Claude to make its choices."
