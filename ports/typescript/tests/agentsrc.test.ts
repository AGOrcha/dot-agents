import { mkdtemp, readFile, writeFile } from "node:fs/promises";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { describe, expect, it } from "vitest";
import {
  loadAgentsRc,
  parseAgentsRcJson,
  saveAgentsRc,
  serializeAgentsRc,
} from "../src/index.js";

describe("parseAgentsRcJson / serializeAgentsRc", () => {
  it("preserves unknown top-level keys across round-trip (parity with TestAgentsRCUnknownFieldsRoundtrip)", () => {
    const input = `{
  "version": 1,
  "project": "myproject",
  "sources": [{"type":"local"}],
  "hooks": false,
  "mcp": false,
  "settings": false,
  "refresh": {"interval": "daily", "auto": true},
  "myteam": "platform"
}`;
    const rc = parseAgentsRcJson(input);

    expect(rc.project).toBe("myproject");
    expect(Object.keys(rc.extraFields).sort()).toEqual(["myteam", "refresh"]);

    rc.project = "renamed";
    const out = serializeAgentsRc(rc);
    const again = parseAgentsRcJson(out);

    expect(again.project).toBe("renamed");
    expect(Object.keys(again.extraFields).sort()).toEqual(["myteam", "refresh"]);

    const refresh = again.extraFields["refresh"] as Record<string, unknown>;
    expect(refresh.interval).toBe("daily");
    expect(refresh.auto).toBe(true);
    expect(again.extraFields["myteam"]).toBe("platform");
  });

  it("does not put known keys into extraFields (parity with TestAgentsRCKnownFieldsNotDuplicated)", () => {
    const input = `{"version":1,"project":"p","sources":[{"type":"local"}],"hooks":false,"mcp":false,"settings":false}`;
    const rc = parseAgentsRcJson(input);
    expect(rc.extraFields).toEqual({});
  });

  it("does not let extraFields override known keys on serialize (MarshalJSON merge rule)", () => {
    const rc = parseAgentsRcJson(
      `{"version":1,"project":"real","sources":[{"type":"local"}],"hooks":false,"mcp":false,"settings":false}`,
    );
    rc.extraFields["project"] = "shadow";
    const out = parseAgentsRcJson(serializeAgentsRc(rc));
    expect(out.project).toBe("real");
  });

  it("loadAgentsRc and saveAgentsRc round-trip on disk", async () => {
    const tmp = await mkdtemp(join(tmpdir(), "agentsrc-ts-"));
    const input = `{
  "version": 1,
  "project": "disk",
  "sources": [{"type":"local"}],
  "hooks": false,
  "mcp": false,
  "settings": false,
  "custom": 42
}`;
    await writeFile(join(tmp, ".agentsrc.json"), input, "utf8");

    const rc = await loadAgentsRc(tmp);
    expect(rc.extraFields.custom).toBe(42);
    rc.project = "disk2";
    await saveAgentsRc(tmp, rc);

    const raw = await readFile(join(tmp, ".agentsrc.json"), "utf8");
    const finalRc = parseAgentsRcJson(raw);
    expect(finalRc.project).toBe("disk2");
    expect(finalRc.extraFields.custom).toBe(42);
  });
});
