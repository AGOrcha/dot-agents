package config

import (
	"github.com/AGOrcha/dot-agents/commands/internal/cmdutil"
	"github.com/AGOrcha/dot-agents/internal/platform"
)

// Closed-set (and config-derived) flag vocabularies for the `da config`
// surface. See docs/CLI_HELP_CONVENTIONS.md.
//
// The four profile-selector dimensions — role, app_type, stage, harness — have
// no compiled-in vocabulary: their values are whatever the merged
// .agentsrc.json declares. Rather than list values that would be wrong for the
// next repo, each flag names the command that prints its live set.

// relevanceFilterEnum picks which facet of the relevance view to render.
var relevanceFilterEnum = cmdutil.EnumSpec{
	Name:    "filter",
	Usage:   "Facet to render",
	Values:  validFilters,
	Default: filterAll,
}

// profileAppTypeSelector narrows profile resolution to one app_type.
var profileAppTypeSelector = cmdutil.EnumSpec{
	Name:        "app-type",
	Usage:       "Resolve the effective profile bundle for this app_type",
	DynamicFrom: "da workflow app-types",
}

// profileStageSelector narrows profile resolution to one pipeline stage. Stage
// keys are declared by the repo under stage_profiles, so the live set is
// whatever that block holds.
var profileStageSelector = cmdutil.EnumSpec{
	Name:        "stage",
	Usage:       "Resolve the effective profile bundle for this stage",
	DynamicFrom: "da config explain stage_profiles",
}

// profileRoleSelector narrows profile resolution to one runtime role. Roles are
// free-form selector values declared by the repo's profile fragments.
var profileRoleSelector = cmdutil.EnumSpec{
	Name:        "role",
	Usage:       "Resolve the effective profile bundle for this runtime role",
	DynamicFrom: "da config explain --all",
}

// profileHarnessSelector narrows profile resolution to one agent harness. The
// harness vocabulary is the supported-platform set, not config-derived.
var profileHarnessSelector = cmdutil.EnumSpec{
	Name:   "harness",
	Usage:  "Resolve the effective profile bundle for this harness",
	Values: platform.IDs(),
}

// relevanceStageSelector narrows the units facet to one stage.
var relevanceStageSelector = cmdutil.EnumSpec{
	Name:        "stage",
	Usage:       "Restrict the units facet to one stage",
	DynamicFrom: "da config explain stage_profiles",
}

// relevanceAppTypeSelector resolves the profile the relevance view renders.
var relevanceAppTypeSelector = cmdutil.EnumSpec{
	Name:        "app-type",
	Usage:       "app_type to resolve the profile for",
	DynamicFrom: "da workflow app-types",
	Note:        "overridden by --task's own app_type",
}
