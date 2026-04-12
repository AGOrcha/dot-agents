package platform

import "path/filepath"

type CanonicalBucket string

const (
	CanonicalBucketRules        CanonicalBucket = "rules"
	CanonicalBucketSettings     CanonicalBucket = "settings"
	CanonicalBucketMCP          CanonicalBucket = "mcp"
	CanonicalBucketSkills       CanonicalBucket = "skills"
	CanonicalBucketAgents       CanonicalBucket = "agents"
	CanonicalBucketHooks        CanonicalBucket = "hooks"
	CanonicalBucketCommands     CanonicalBucket = "commands"
	CanonicalBucketOutputStyles CanonicalBucket = "output-styles"
	CanonicalBucketIgnore       CanonicalBucket = "ignore"
	CanonicalBucketModes        CanonicalBucket = "modes"
	CanonicalBucketPlugins      CanonicalBucket = "plugins"
	CanonicalBucketThemes       CanonicalBucket = "themes"
	CanonicalBucketPrompts      CanonicalBucket = "prompts"
)

type CanonicalBucketSpec struct {
	Name        CanonicalBucket
	Stage       int
	CountDirs   bool
	MarkerFile  string
	Description string
}

func CanonicalStoreBucketSpecs() []CanonicalBucketSpec {
	return append(append([]CanonicalBucketSpec{}, canonicalStoreStage1BucketSpecs()...), canonicalStoreStage2BucketSpecs()...)
}

func CanonicalStoreStage1BucketSpecs() []CanonicalBucketSpec {
	return append([]CanonicalBucketSpec{}, canonicalStoreStage1BucketSpecs()...)
}

func CanonicalStoreStage2BucketSpecs() []CanonicalBucketSpec {
	return append([]CanonicalBucketSpec{}, canonicalStoreStage2BucketSpecs()...)
}

func CanonicalBucketRoot(agentsHome string, bucket CanonicalBucket) string {
	return filepath.Join(agentsHome, string(bucket))
}

func CanonicalBucketScopeRoot(agentsHome string, bucket CanonicalBucket, scope string) string {
	return filepath.Join(agentsHome, string(bucket), scope)
}

func CanonicalBucketPath(bucket CanonicalBucket, parts ...string) string {
	elems := append([]string{string(bucket)}, parts...)
	return filepath.Join(elems...)
}

func CanonicalBucketScopePath(bucket CanonicalBucket, scope string, parts ...string) string {
	elems := append([]string{string(bucket), scope}, parts...)
	return filepath.Join(elems...)
}

func ListScopedResourceDirsForBucket(agentsHome string, bucket CanonicalBucket, scope, marker string) ([]resourceDir, error) {
	return listScopedResourceDirs(agentsHome, string(bucket), scope, marker)
}

func canonicalStoreStage1BucketSpecs() []CanonicalBucketSpec {
	return []CanonicalBucketSpec{
		{Name: CanonicalBucketRules, Stage: 1, Description: "Rules and instructions"},
		{Name: CanonicalBucketSettings, Stage: 1, Description: "Platform settings and config"},
		{Name: CanonicalBucketMCP, Stage: 1, Description: "MCP configs"},
		{Name: CanonicalBucketSkills, Stage: 1, CountDirs: true, MarkerFile: "SKILL.md", Description: "Canonical skills"},
		{Name: CanonicalBucketAgents, Stage: 1, CountDirs: true, MarkerFile: "AGENT.md", Description: "Canonical agents"},
		{Name: CanonicalBucketHooks, Stage: 1, CountDirs: true, MarkerFile: "HOOK.yaml", Description: "Canonical hook bundles"},
	}
}

func canonicalStoreStage2BucketSpecs() []CanonicalBucketSpec {
	return []CanonicalBucketSpec{
		{Name: CanonicalBucketCommands, Stage: 2, Description: "Command bundles"},
		{Name: CanonicalBucketOutputStyles, Stage: 2, Description: "Claude output styles"},
		{Name: CanonicalBucketIgnore, Stage: 2, Description: "Ignore files"},
		{Name: CanonicalBucketModes, Stage: 2, Description: "OpenCode modes"},
		{Name: CanonicalBucketPlugins, Stage: 2, CountDirs: true, MarkerFile: PluginManifestName, Description: "Plugin bundles"},
		{Name: CanonicalBucketThemes, Stage: 2, Description: "OpenCode themes"},
		{Name: CanonicalBucketPrompts, Stage: 2, Description: "Copilot prompt files"},
	}
}
