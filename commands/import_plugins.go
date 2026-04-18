package commands

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/NikashPrakash/dot-agents/internal/platform"
	"go.yaml.in/yaml/v3"
)

type importedPackagePluginManifest struct {
	Name        string                      `json:"name"`
	Version     string                      `json:"version,omitempty"`
	Description string                      `json:"description,omitempty"`
	DisplayName string                      `json:"display_name,omitempty"`
	Authors     []string                    `json:"authors,omitempty"`
	Author      importedPackagePluginAuthor `json:"author,omitempty"`
	Homepage    string                      `json:"homepage,omitempty"`
	Repository  string                      `json:"repository,omitempty"`
	License     string                      `json:"license,omitempty"`
	Keywords    []string                    `json:"keywords,omitempty"`
	Agents      string                      `json:"agents,omitempty"`
	Skills      string                      `json:"skills,omitempty"`
	Commands    string                      `json:"commands,omitempty"`
	Hooks       string                      `json:"hooks,omitempty"`
	MCPServers  string                      `json:"mcpServers,omitempty"`
	Apps        string                      `json:"apps,omitempty"`
}

type importedPackagePluginAuthor struct {
	Name  string `json:"name"`
	Email string `json:"email,omitempty"`
	URL   string `json:"url,omitempty"`
}

func supportsCanonicalImportPath(rel string) bool {
	if platformID, _, kind := packagePluginLayout(rel); platformID != "" && kind != "" {
		return true
	}
	return supportsCanonicalImportPathNonPlugin(rel)
}

func supportsCanonicalImportPathNonPlugin(rel string) bool {
	switch {
	case rel == relCursorHooksJSON, rel == relCodexHooksJSON:
		return true
	case rel == relClaudeSettingsLocal, rel == relClaudeSettingsJSON:
		return true
	case strings.HasPrefix(rel, relGitHubHooksDir):
		return true
	case strings.HasPrefix(rel, relOpenCodePluginsDir):
		return true
	default:
		return false
	}
}

type directPackagePluginRef struct {
	platformID string
	name       string
	component  string
	relPath    string
	destKind   string
	destPath   string
	dir        bool
}

const (
	directPackageDestResource    = "resource"
	directPackageDestPlatform    = "platform"
	packagePluginManifestFile    = "manifest"
	packagePluginMarketplaceFile = "marketplace"
	packagePluginComponentFile   = "component"
	packagePluginOverlayFile     = "overlay"
)

func gatherDirectPackagePluginCandidates(project, projectPath string) []importCandidate {
	refs, err := directPackagePluginRefs(projectPath)
	if err != nil || len(refs) == 0 {
		return nil
	}

	seen := map[string]struct{}{}
	out := []importCandidate{}
	appendCandidate := func(src string) {
		if _, ok := seen[src]; ok {
			return
		}
		seen[src] = struct{}{}
		out = append(out, importCandidate{
			project:    project,
			sourceRoot: projectPath,
			sourcePath: src,
		})
	}

	for _, ref := range refs {
		if ref.dir {
			root := filepath.Join(projectPath, filepath.FromSlash(ref.relPath))
			_ = filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
				if walkErr != nil || d.IsDir() || isBackupArtifact(d.Name()) {
					return nil
				}
				rel, relErr := filepath.Rel(projectPath, path)
				if relErr != nil {
					return nil
				}
				if isProjectImportRelCovered(filepath.ToSlash(rel)) {
					return nil
				}
				appendCandidate(path)
				return nil
			})
			continue
		}

		if isProjectImportRelCovered(ref.relPath) {
			continue
		}
		src := filepath.Join(projectPath, filepath.FromSlash(ref.relPath))
		info, statErr := os.Lstat(src)
		if statErr != nil || info.IsDir() || isBackupArtifact(filepath.Base(src)) {
			continue
		}
		appendCandidate(src)
	}

	return out
}

func isProjectImportRelCovered(rel string) bool {
	for _, single := range projectImportSingles {
		if rel == single {
			return true
		}
	}
	for _, walkDir := range projectImportWalkDirs {
		prefix := strings.TrimSuffix(filepath.ToSlash(walkDir), "/") + "/"
		if strings.HasPrefix(rel, prefix) {
			return true
		}
	}
	return false
}

func directPackagePluginRefs(sourceRoot string) ([]directPackagePluginRef, error) {
	out := []directPackagePluginRef{}

	refs, err := directPackagePluginRefsForManifest(sourceRoot, "copilot", filepath.Join(sourceRoot, relCopilotPluginManifest), func(manifest importedPackagePluginManifest, name string) []directPackagePluginRef {
		return []directPackagePluginRef{
			directPackagePluginDirRef("copilot", name, "agents", manifest.Agents),
			directPackagePluginDirRef("copilot", name, "skills", manifest.Skills),
			directPackagePluginDirRef("copilot", name, "commands", manifest.Commands),
			directPackagePluginFileRef("copilot", name, manifest.Hooks, "hooks.json"),
			directPackagePluginFileRef("copilot", name, manifest.MCPServers, relMCPJSON),
		}
	})
	if err != nil {
		return nil, err
	}
	out = append(out, refs...)

	refs, err = directPackagePluginRefsForManifest(sourceRoot, "copilot", filepath.Join(sourceRoot, relGitHubPluginManifest), func(manifest importedPackagePluginManifest, name string) []directPackagePluginRef {
		return []directPackagePluginRef{
			directPackagePluginDirRef("copilot", name, "agents", manifest.Agents),
			directPackagePluginDirRef("copilot", name, "skills", manifest.Skills),
			directPackagePluginDirRef("copilot", name, "commands", manifest.Commands),
			directPackagePluginFileRef("copilot", name, manifest.Hooks, "hooks.json"),
			directPackagePluginFileRef("copilot", name, manifest.MCPServers, relMCPJSON),
		}
	})
	if err != nil {
		return nil, err
	}
	out = append(out, refs...)

	refs, err = directPackagePluginRefsForManifest(sourceRoot, "codex", filepath.Join(sourceRoot, relCodexPluginDir[:len(relCodexPluginDir)-1], relCopilotPluginManifest), func(manifest importedPackagePluginManifest, name string) []directPackagePluginRef {
		return []directPackagePluginRef{
			directPackagePluginDirRef("codex", name, "skills", manifest.Skills),
			directPackagePluginFileRef("codex", name, manifest.Hooks, "hooks.json"),
			directPackagePluginFileRef("codex", name, manifest.MCPServers, relMCPJSON),
			directPackagePluginFileRef("codex", name, manifest.Apps, ".app.json"),
		}
	})
	if err != nil {
		return nil, err
	}
	out = append(out, refs...)

	filtered := make([]directPackagePluginRef, 0, len(out))
	for _, ref := range out {
		if ref.relPath == "" || ref.name == "" {
			continue
		}
		filtered = append(filtered, ref)
	}
	return filtered, nil
}

func directPackagePluginRefsForManifest(sourceRoot, platformID, manifestPath string, build func(importedPackagePluginManifest, string) []directPackagePluginRef) ([]directPackagePluginRef, error) {
	manifest, ok, err := loadImportedPackagePluginManifest(manifestPath)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}

	name := strings.TrimSpace(manifest.Name)
	if name == "" {
		name, err = packagePluginNameFromMarketplace(manifestPath, platformID, manifestPath)
		if err != nil {
			return nil, err
		}
	}
	if name == "" {
		return nil, nil
	}

	return build(manifest, name), nil
}

func directPackagePluginDirRef(platformID, name, component, rawPath string) directPackagePluginRef {
	return directPackagePluginRef{
		platformID: platformID,
		name:       name,
		component:  component,
		relPath:    normalizeImportedPackagePluginPath(rawPath),
		destKind:   directPackageDestResource,
		dir:        true,
	}
}

func directPackagePluginFileRef(platformID, name, rawPath, destPath string) directPackagePluginRef {
	return directPackagePluginRef{
		platformID: platformID,
		name:       name,
		relPath:    normalizeImportedPackagePluginPath(rawPath),
		destKind:   directPackageDestPlatform,
		destPath:   destPath,
	}
}

func normalizeImportedPackagePluginPath(rawPath string) string {
	trimmed := filepath.ToSlash(strings.TrimSpace(rawPath))
	if trimmed == "" {
		return ""
	}
	for strings.HasPrefix(trimmed, "./") {
		trimmed = strings.TrimPrefix(trimmed, "./")
	}
	cleaned := filepath.ToSlash(filepath.Clean(trimmed))
	if cleaned == "." || cleaned == "" || strings.HasPrefix(cleaned, "../") || cleaned == ".." || strings.HasPrefix(cleaned, "/") {
		return ""
	}
	return strings.TrimSuffix(cleaned, "/")
}

func canonicalPluginOutputs(c importCandidate, rel string) ([]importOutput, bool, error) {
	if strings.HasPrefix(rel, relOpenCodePluginsDir) {
		return canonicalPluginOutputsFromOpenCodeFile(c.project, rel, c.sourcePath)
	}

	platformID, rootRel, kind := packagePluginLayout(rel)
	if platformID == "" {
		return nil, false, nil
	}

	manifestPath := packagePluginManifestPath(c.sourceRoot, rootRel, platformID)
	manifest, manifestOK, err := loadImportedPackagePluginManifest(manifestPath)
	if err != nil {
		return nil, true, err
	}

	name := strings.TrimSpace(manifest.Name)
	if name == "" {
		if name, err = packagePluginNameFromMarketplace(c.sourcePath, platformID, manifestPath); err != nil {
			return nil, true, err
		}
	}
	if name == "" {
		return nil, false, nil
	}

	switch kind {
	case packagePluginManifestFile:
		return canonicalPackagePluginManifestOutputs(c, platformID, name, manifest, manifestOK)
	case packagePluginMarketplaceFile:
		return canonicalPackagePluginMarketplaceOutputs(c, platformID, name, manifestPath)
	case packagePluginComponentFile:
		return canonicalPackagePluginComponentOutput(c, platformID, name, rootRel, rel)
	case packagePluginOverlayFile:
		return canonicalPackagePluginOverlayOutput(c, platformID, name, rootRel, rel)
	default:
		return nil, false, nil
	}
}

func canonicalPluginOutputsFromOpenCodeFile(scope, relPath, sourcePath string) ([]importOutput, bool, error) {
	trimmed := strings.TrimPrefix(relPath, relOpenCodePluginsDir)
	parts := strings.SplitN(trimmed, "/", 2)
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return nil, false, nil
	}

	content, err := os.ReadFile(sourcePath)
	if err != nil {
		return nil, true, err
	}

	manifestContent, err := yaml.Marshal(platform.PluginSpec{
		SchemaVersion: 1,
		Kind:          platform.PluginKindNative,
		Name:          strings.TrimSpace(parts[0]),
		Platforms:     []string{"opencode"},
	})
	if err != nil {
		return nil, true, err
	}

	base := filepath.ToSlash(filepath.Join("plugins", scope, parts[0]))
	return []importOutput{
		{
			destRel: filepath.ToSlash(filepath.Join(base, platform.PluginManifestName)),
			content: append(manifestContent, '\n'),
		},
		{
			destRel: filepath.ToSlash(filepath.Join(base, "files", parts[1])),
			content: content,
		},
	}, true, nil
}

func packagePluginLayout(rel string) (platformID, rootRel, kind string) {
	switch {
	case rel == relCopilotPluginManifest || rel == relGitHubPluginManifest:
		if rel == relGitHubPluginManifest {
			return "copilot", strings.TrimSuffix(relGitHubPluginDir, "/"), packagePluginManifestFile
		}
		return "copilot", "", packagePluginManifestFile
	case rel == relCopilotPluginMarket:
		return "copilot", "", packagePluginMarketplaceFile
	case rel == relCodexPluginMarket:
		return "codex", strings.TrimSuffix(relCodexPluginDir, "/"), packagePluginMarketplaceFile
	case strings.HasPrefix(rel, relGitHubPluginDir):
		return "copilot", strings.TrimSuffix(relGitHubPluginDir, "/"), packagePluginLayoutKind(rel, relGitHubPluginDir)
	case strings.HasPrefix(rel, "agents/"), strings.HasPrefix(rel, "skills/"), strings.HasPrefix(rel, "commands/"):
		return "copilot", "", packagePluginComponentFile
	case strings.HasPrefix(rel, relClaudePluginDir):
		return "claude", strings.TrimSuffix(relClaudePluginDir, "/"), packagePluginLayoutKind(rel, relClaudePluginDir)
	case strings.HasPrefix(rel, relCursorPluginDir):
		return "cursor", strings.TrimSuffix(relCursorPluginDir, "/"), packagePluginLayoutKind(rel, relCursorPluginDir)
	case strings.HasPrefix(rel, relCodexPluginDir):
		return "codex", strings.TrimSuffix(relCodexPluginDir, "/"), packagePluginLayoutKind(rel, relCodexPluginDir)
	default:
		return "", "", ""
	}
}

func packagePluginLayoutKind(rel, rootPrefix string) string {
	trimmed := strings.TrimPrefix(rel, rootPrefix)
	switch {
	case trimmed == "plugin.json":
		return packagePluginManifestFile
	case trimmed == "marketplace.json":
		return packagePluginMarketplaceFile
	case trimmed == "commands/plugin.json":
		return packagePluginComponentFile
	case trimmed == "agents/plugin.json":
		return packagePluginComponentFile
	case trimmed == "skills/plugin.json":
		return packagePluginComponentFile
	case trimmed == "hooks/plugin.json":
		return packagePluginComponentFile
	case trimmed == "rules/plugin.json":
		return packagePluginComponentFile
	case trimmed == "mcp.json", trimmed == ".mcp.json":
		return packagePluginComponentFile
	default:
		if strings.HasPrefix(trimmed, "commands/") || strings.HasPrefix(trimmed, "agents/") || strings.HasPrefix(trimmed, "skills/") || strings.HasPrefix(trimmed, "hooks/") || strings.HasPrefix(trimmed, "rules/") {
			return packagePluginComponentFile
		}
		if strings.HasPrefix(trimmed, "mcp/") {
			return packagePluginComponentFile
		}
		if trimmed != "" {
			return packagePluginOverlayFile
		}
		return ""
	}
}

func packagePluginManifestPath(sourceRoot, rootRel, platformID string) string {
	switch platformID {
	case "copilot":
		if rootRel == "" {
			return filepath.Join(sourceRoot, relCopilotPluginManifest)
		}
		return filepath.Join(sourceRoot, rootRel, "plugin.json")
	case "codex":
		if rootRel == "" {
			return filepath.Join(sourceRoot, relCodexPluginDir[:len(relCodexPluginDir)-1], relCopilotPluginManifest)
		}
		return filepath.Join(sourceRoot, rootRel, relCopilotPluginManifest)
	default:
		return filepath.Join(sourceRoot, rootRel, relCopilotPluginManifest)
	}
}

func loadImportedPackagePluginManifest(path string) (importedPackagePluginManifest, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return importedPackagePluginManifest{}, false, nil
		}
		return importedPackagePluginManifest{}, false, err
	}
	var manifest importedPackagePluginManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return importedPackagePluginManifest{}, false, nil
	}
	return manifest, true, nil
}

func packagePluginNameFromMarketplace(sourcePath, platformID, manifestPath string) (string, error) {
	paths := []string{sourcePath}
	switch platformID {
	case "copilot", "codex", "claude", "cursor":
		paths = append(paths, filepath.Join(filepath.Dir(manifestPath), "marketplace.json"))
	}

	for _, path := range paths {
		if name, ok, err := nameFromMarketplace(path, platformID); err != nil {
			return "", err
		} else if ok && name != "" {
			return name, nil
		}
	}
	return "", nil
}

func nameFromMarketplace(path, platformID string) (string, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, err
	}
	var payload struct {
		Plugins []struct {
			Name string `json:"name"`
		} `json:"plugins"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return "", false, nil
	}
	if len(payload.Plugins) == 0 {
		return "", false, nil
	}
	return strings.TrimSpace(payload.Plugins[0].Name), true, nil
}

func canonicalPackagePluginManifestOutputs(c importCandidate, platformID, name string, manifest importedPackagePluginManifest, manifestOK bool) ([]importOutput, bool, error) {
	spec := platform.PluginSpec{
		SchemaVersion: 1,
		Kind:          platform.PluginKindPackage,
		Name:          name,
		Platforms:     []string{platformID},
	}
	if manifestOK {
		spec.Version = strings.TrimSpace(manifest.Version)
		spec.Description = strings.TrimSpace(manifest.Description)
		spec.Homepage = strings.TrimSpace(manifest.Homepage)
		spec.License = strings.TrimSpace(manifest.License)
		spec.Marketplace = platform.PluginMarketplace{
			Repo: strings.TrimSpace(manifest.Repository),
			Tags: sortedUniqueStrings(append([]string(nil), manifest.Keywords...)),
		}
		if display := strings.TrimSpace(manifest.DisplayName); display != "" {
			spec.DisplayName = display
		}
		spec.Authors = importedPackageAuthors(manifest)
	}

	yamlContent, err := yaml.Marshal(spec)
	if err != nil {
		return nil, true, err
	}
	base := filepath.ToSlash(filepath.Join("plugins", c.project, name))
	outputs := []importOutput{
		{
			destRel: filepath.ToSlash(filepath.Join(base, platform.PluginManifestName)),
			content: append(yamlContent, '\n'),
		},
	}
	raw, err := os.ReadFile(c.sourcePath)
	if err != nil {
		return nil, true, err
	}
	outputs = append(outputs, importOutput{
		destRel: filepath.ToSlash(filepath.Join(base, "platforms", platformID, "plugin.json")),
		content: raw,
	})
	return outputs, true, nil
}

func canonicalPackagePluginMarketplaceOutputs(c importCandidate, platformID, name, manifestPath string) ([]importOutput, bool, error) {
	raw, err := os.ReadFile(c.sourcePath)
	if err != nil {
		return nil, true, err
	}
	base := filepath.ToSlash(filepath.Join("plugins", c.project, name))
	return []importOutput{{
		destRel: filepath.ToSlash(filepath.Join(base, "platforms", platformID, "marketplace.json")),
		content: raw,
	}}, true, nil
}

func canonicalPackagePluginComponentOutput(c importCandidate, platformID, name, rootRel, rel string) ([]importOutput, bool, error) {
	trimmed := rel
	if rootRel != "" {
		trimmed = strings.TrimPrefix(rel, rootRel+"/")
		if trimmed == rel {
			return nil, false, nil
		}
	}

	component, rest, ok := packagePluginComponentPath(trimmed, platformID)
	if !ok {
		return nil, false, nil
	}
	raw, err := os.ReadFile(c.sourcePath)
	if err != nil {
		return nil, true, err
	}
	base := filepath.ToSlash(filepath.Join("plugins", c.project, name, "resources", component))
	return []importOutput{{
		destRel: filepath.ToSlash(filepath.Join(base, rest)),
		content: raw,
	}}, true, nil
}

func canonicalPackagePluginOverlayOutput(c importCandidate, platformID, name, rootRel, rel string) ([]importOutput, bool, error) {
	trimmed := strings.TrimPrefix(rel, rootRel+"/")
	if trimmed == rel || trimmed == "" {
		return nil, false, nil
	}
	raw, err := os.ReadFile(c.sourcePath)
	if err != nil {
		return nil, true, err
	}
	base := filepath.ToSlash(filepath.Join("plugins", c.project, name, "platforms", platformID))
	return []importOutput{{
		destRel: filepath.ToSlash(filepath.Join(base, trimmed)),
		content: raw,
	}}, true, nil
}

func packagePluginComponentPath(trimmed, platformID string) (component, rest string, ok bool) {
	switch platformID {
	case "claude":
		switch {
		case strings.HasPrefix(trimmed, "commands/"):
			return "commands", strings.TrimPrefix(trimmed, "commands/"), true
		case strings.HasPrefix(trimmed, "agents/"):
			return "agents", strings.TrimPrefix(trimmed, "agents/"), true
		case strings.HasPrefix(trimmed, "skills/"):
			return "skills", strings.TrimPrefix(trimmed, "skills/"), true
		case strings.HasPrefix(trimmed, "hooks/"):
			return "hooks", strings.TrimPrefix(trimmed, "hooks/"), true
		case strings.HasPrefix(trimmed, "mcp/"):
			return "mcp", strings.TrimPrefix(trimmed, "mcp/"), true
		case strings.HasPrefix(trimmed, "rules/"):
			return "rules", strings.TrimPrefix(trimmed, "rules/"), true
		}
	case "cursor":
		switch {
		case strings.HasPrefix(trimmed, "rules/"):
			return "rules", strings.TrimPrefix(trimmed, "rules/"), true
		case strings.HasPrefix(trimmed, "commands/"):
			return "commands", strings.TrimPrefix(trimmed, "commands/"), true
		case strings.HasPrefix(trimmed, "agents/"):
			return "agents", strings.TrimPrefix(trimmed, "agents/"), true
		case strings.HasPrefix(trimmed, "skills/"):
			return "skills", strings.TrimPrefix(trimmed, "skills/"), true
		case strings.HasPrefix(trimmed, "hooks/"):
			return "hooks", strings.TrimPrefix(trimmed, "hooks/"), true
		case strings.HasPrefix(trimmed, "mcp/"):
			return "mcp", strings.TrimPrefix(trimmed, "mcp/"), true
		case trimmed == "mcp.json", trimmed == ".mcp.json":
			return "mcp", trimmed, true
		}
	case "codex":
		if strings.HasPrefix(trimmed, "skills/") {
			return "skills", strings.TrimPrefix(trimmed, "skills/"), true
		}
		if strings.HasPrefix(trimmed, "agents/") {
			return "agents", strings.TrimPrefix(trimmed, "agents/"), true
		}
		if strings.HasPrefix(trimmed, "hooks/") {
			return "hooks", strings.TrimPrefix(trimmed, "hooks/"), true
		}
		if strings.HasPrefix(trimmed, "mcp/") {
			return "mcp", strings.TrimPrefix(trimmed, "mcp/"), true
		}
		if strings.HasPrefix(trimmed, "commands/") {
			return "commands", strings.TrimPrefix(trimmed, "commands/"), true
		}
	case "copilot":
		switch {
		case strings.HasPrefix(trimmed, "agents/"):
			return "agents", strings.TrimPrefix(trimmed, "agents/"), true
		case strings.HasPrefix(trimmed, "skills/"):
			return "skills", strings.TrimPrefix(trimmed, "skills/"), true
		case strings.HasPrefix(trimmed, "commands/"):
			return "commands", strings.TrimPrefix(trimmed, "commands/"), true
		}
	}
	return "", "", false
}

func importedPackageAuthors(manifest importedPackagePluginManifest) []string {
	if len(manifest.Authors) > 0 {
		return sortedUniqueStrings(manifest.Authors)
	}
	if name := strings.TrimSpace(manifest.Author.Name); name != "" {
		return []string{name}
	}
	return nil
}

func sortedUniqueStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
