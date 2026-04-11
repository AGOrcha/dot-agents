package platform

// Platform defines the interface all AI agent platforms must implement.
type Platform interface {
	// ID returns the platform identifier (e.g. "cursor", "claude").
	ID() string
	// DisplayName returns the human-readable name.
	DisplayName() string
	// IsInstalled checks if this platform is installed on the system.
	IsInstalled() bool
	// Version returns the detected version string, or empty string.
	Version() string
	// CreateLinks creates all managed links for a project in repoPath.
	CreateLinks(project, repoPath string) error
	// RemoveLinks removes all managed links for a project from repoPath.
	RemoveLinks(project, repoPath string) error
	// HasDeprecatedFormat checks if the project has deprecated config files.
	HasDeprecatedFormat(repoPath string) bool
	// DeprecatedDetails returns a description of the deprecated format.
	DeprecatedDetails(repoPath string) string
	// SharedTargetIntents returns the ResourceIntents this platform would write
	// to shared (cross-platform) repo-local targets such as .agents/skills/*.
	// These intents are aggregated by the command layer into a single
	// ResourcePlan so compatible targets are deduped and conflicts are caught
	// before any filesystem writes occur.
	SharedTargetIntents(project string) ([]ResourceIntent, error)
}

// All returns the ordered list of all supported platforms.
func All() []Platform {
	return []Platform{
		NewCursor(),
		NewClaude(),
		NewCodex(),
		NewOpenCode(),
		NewCopilot(),
	}
}

// ByID returns the platform with the given ID, or nil.
func ByID(id string) Platform {
	for _, p := range All() {
		if p.ID() == id {
			return p
		}
	}
	return nil
}
