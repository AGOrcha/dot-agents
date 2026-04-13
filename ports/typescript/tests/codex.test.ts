import { describe, expect, it } from "vitest";
import {
  extractAgentBody,
  parseFrontmatter,
  renderCodexAgentTomlFromContent,
  tomlMultilineString,
} from "../src/index.js";

describe("parseFrontmatter", () => {
  it("extracts key-value pairs from frontmatter block", () => {
    const content = `---
name: reviewer
description: reviews changes
model: gpt-5.1-codex
is_background: true
---

# Reviewer
`;
    const meta = parseFrontmatter(content);
    expect(meta["name"]).toBe("reviewer");
    expect(meta["description"]).toBe("reviews changes");
    expect(meta["model"]).toBe("gpt-5.1-codex");
    expect(meta["is_background"]).toBe("true");
  });

  it("returns empty map when no frontmatter", () => {
    expect(parseFrontmatter("# Just a doc\nNo frontmatter here.")).toEqual({});
  });

  it("strips surrounding quotes from values", () => {
    const content = `---\nname: "quoted"\n---\n`;
    expect(parseFrontmatter(content)["name"]).toBe("quoted");
  });
});

describe("extractAgentBody", () => {
  it("returns content after the closing ---", () => {
    const content = `---
name: reviewer
---

# Reviewer

Body text here.
`;
    const body = extractAgentBody(content);
    expect(body).toContain("# Reviewer");
    expect(body).toContain("Body text here.");
    expect(body).not.toContain("name:");
  });

  it("returns full content when no frontmatter", () => {
    const content = "# Plain doc\nNo frontmatter.";
    expect(extractAgentBody(content)).toBe(content);
  });
});

describe("tomlMultilineString", () => {
  it("wraps value in triple-quotes", () => {
    const result = tomlMultilineString("hello");
    expect(result).toBe('"""\nhello\n"""');
  });

  it('escapes embedded triple-quotes', () => {
    const result = tomlMultilineString('say """hi"""');
    expect(result).toContain('\\"\\"\\"hi\\"\\"\\"');
  });

  it("escapes backslashes", () => {
    const result = tomlMultilineString("path\\to\\file");
    expect(result).toContain("path\\\\to\\\\file");
  });
});

describe("renderCodexAgentTomlFromContent", () => {
  it("emits developer_instructions from body, not is_background (parity with TestRenderCodexAgentTomlUsesFrontmatterAndBody)", () => {
    const content = `---
name: reviewer
description: reviews changes
model: gpt-5.1-codex
is_background: true
---

# Reviewer

Use "safe" defaults and avoid shell footguns.
`;
    const out = renderCodexAgentTomlFromContent(content, "/agents/global/reviewer/AGENT.md");

    expect(out).toContain(`name = "reviewer"`);
    expect(out).toContain(`description = "reviews changes"`);
    expect(out).toContain(`model = "gpt-5.1-codex"`);
    expect(out).toContain(`developer_instructions = """`);
    expect(out).toContain("# Reviewer");
    expect(out).toContain('Use "safe" defaults and avoid shell footguns.');
    expect(out).not.toContain("is_background");
  });

  it("omits model when not set", () => {
    const content = `---
name: simple
description: a simple agent
---

Do stuff.
`;
    const out = renderCodexAgentTomlFromContent(content, "/agents/global/simple/AGENT.md");
    expect(out).not.toContain("model =");
    expect(out).toContain(`name = "simple"`);
  });

  it("omits developer_instructions when body is blank", () => {
    const content = `---
name: noops
description: no body
---
`;
    const out = renderCodexAgentTomlFromContent(content, "/agents/global/noops/AGENT.md");
    expect(out).not.toContain("developer_instructions");
  });

  it("derives name from directory when frontmatter name is absent", () => {
    const content = `---
description: no name in frontmatter
---

Body.
`;
    const out = renderCodexAgentTomlFromContent(content, "/agents/global/derived-name/AGENT.md");
    expect(out).toContain(`name = "derived-name"`);
  });

  it("negative: is_background in frontmatter is ignored (not emitted)", () => {
    const content = `---
name: bg
description: background agent
is_background: false
---
`;
    const out = renderCodexAgentTomlFromContent(content, "/agents/global/bg/AGENT.md");
    expect(out).not.toContain("is_background");
  });
});
