export {
  AGENTS_RC_FILE,
  AGENTS_RC_KNOWN_KEYS,
  type AgentsRc,
  type Source,
  type StringsOrBool,
  loadAgentsRc,
  parseAgentsRcJson,
  saveAgentsRc,
  serializeAgentsRc,
} from "./core/agentsrc.js";

export { detectMCPServers, readMCPScope } from "./core/mcp.js";

export { detectHookEvents } from "./core/hooks.js";

export {
  extractAgentBody,
  parseFrontmatter,
  renderCodexAgentToml,
  renderCodexAgentTomlFromContent,
  tomlMultilineString,
} from "./platforms/codex.js";

export {
  agentsHome,
  userHome,
  expandPath,
  displayPath,
  loadConfig,
  saveConfig,
  listProjects,
  addProject,
  removeProject,
  getProjectPath,
  type Config,
  type ConfigProject,
  type ConfigAgent,
} from "./core/config.js";

export { runInit, standardDirs, type InitOptions, type InitResult } from "./commands/init.js";
export { runAdd, type AddOptions, type AddResult, type AddStatus } from "./commands/add.js";
export { runRefresh, type RefreshOptions, type RefreshResult, type ProjectRefreshEntry } from "./commands/refresh.js";
export { runStatus, type StatusOptions, type StatusResult, type StatusProject, type CanonicalStoreEntry } from "./commands/status.js";
export { runDoctor, type DoctorOptions, type DoctorResult, type DoctorCheck } from "./commands/doctor.js";
export { runSkillsList, runSkillsNew, type SkillsOptions, type SkillEntry, type SkillsListResult } from "./commands/skills.js";
export { runAgentsList, runAgentsNew, type AgentsOptions, type AgentEntry, type AgentsListResult } from "./commands/agents.js";
export { runHooksList, type HooksOptions, type HooksListResult, type HookBundle, type LegacyHookEvent } from "./commands/hooks.js";
