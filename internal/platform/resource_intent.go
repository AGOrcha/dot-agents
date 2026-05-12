package platform

import (
	"fmt"
	"path/filepath"
)

type ResourceOwnership string

const (
	ResourceOwnershipSharedRepo   ResourceOwnership = "shared_repo"
	ResourceOwnershipPlatformRepo ResourceOwnership = "platform_repo"
	ResourceOwnershipUserHome     ResourceOwnership = "user_home"
)

type ResourceShape string

const (
	ResourceShapeDirectDir    ResourceShape = "direct_dir"
	ResourceShapeDirectFile   ResourceShape = "direct_file"
	ResourceShapeRenderSingle ResourceShape = "render_single"
	ResourceShapeRenderFanout ResourceShape = "render_fanout"
)

type ResourceTransport string

const (
	ResourceTransportSymlink  ResourceTransport = "symlink"
	ResourceTransportHardlink ResourceTransport = "hardlink"
	ResourceTransportWrite    ResourceTransport = "write"
)

type ResourceReplacePolicy string

const (
	ResourceReplaceNever                      ResourceReplacePolicy = "never"
	ResourceReplaceIfManaged                  ResourceReplacePolicy = "if_managed"
	ResourceReplaceAllowlistedImportedDirOnly ResourceReplacePolicy = "allowlisted_imported_dir_only"
)

type ResourcePrunePolicy string

const (
	ResourcePruneNone              ResourcePrunePolicy = "none"
	ResourcePruneTarget            ResourcePrunePolicy = "target_only"
	ResourcePruneGeneratedChildren ResourcePrunePolicy = "generated_children"
)

type ResourceSourceKind string

const (
	ResourceSourceCanonicalFile   ResourceSourceKind = "canonical_file"
	ResourceSourceCanonicalDir    ResourceSourceKind = "canonical_dir"
	ResourceSourceCanonicalBundle ResourceSourceKind = "canonical_bundle"
)

type ResourceSourceRef struct {
	Scope        string             `json:"scope"`
	Bucket       string             `json:"bucket"`
	RelativePath string             `json:"relative_path"`
	Kind         ResourceSourceKind `json:"kind"`
	Origin       string             `json:"origin,omitempty"`
}

func (r ResourceSourceRef) CanonicalPath(agentsHome string) string {
	if agentsHome == "" || r.Bucket == "" || r.Scope == "" || r.RelativePath == "" {
		return ""
	}
	return filepath.Join(agentsHome, r.Bucket, r.Scope, r.RelativePath)
}

func (r ResourceSourceRef) Validate() error {
	if r.Scope == "" {
		return fmt.Errorf("source_ref.scope is required")
	}
	if r.Bucket == "" {
		return fmt.Errorf("source_ref.bucket is required")
	}
	if r.RelativePath == "" {
		return fmt.Errorf("source_ref.relative_path is required")
	}
	switch r.Kind {
	case ResourceSourceCanonicalFile, ResourceSourceCanonicalDir, ResourceSourceCanonicalBundle:
		return nil
	case "":
		return fmt.Errorf("source_ref.kind is required")
	default:
		return fmt.Errorf("source_ref.kind %q is unsupported", r.Kind)
	}
}

type ResourceProvenance struct {
	Emitter   string `json:"emitter,omitempty"`
	Operation string `json:"operation,omitempty"`
	Detail    string `json:"detail,omitempty"`
}

type ResourceIntent struct {
	IntentID      string                `json:"intent_id"`
	Project       string                `json:"project"`
	Bucket        string                `json:"bucket"`
	LogicalName   string                `json:"logical_name"`
	TargetPath    string                `json:"target_path"`
	Ownership     ResourceOwnership     `json:"ownership"`
	SourceRef     ResourceSourceRef     `json:"source_ref"`
	Shape         ResourceShape         `json:"shape"`
	Transport     ResourceTransport     `json:"transport"`
	Materializer  string                `json:"materializer"`
	ReplacePolicy ResourceReplacePolicy `json:"replace_policy"`
	PrunePolicy   ResourcePrunePolicy   `json:"prune_policy"`
	Provenance    ResourceProvenance    `json:"provenance"`
	Precedence    int                   `json:"precedence,omitempty"`
	ConflictKey   string                `json:"conflict_key,omitempty"`
	MarkerFiles   []string              `json:"marker_files,omitempty"`
	EnabledOn     []string              `json:"enabled_on,omitempty"`
	ReviewHint    string                `json:"review_hint,omitempty"`
}

func (i ResourceIntent) EffectiveConflictKey() string {
	if i.ConflictKey != "" {
		return i.ConflictKey
	}
	return i.TargetPath
}

func (i ResourceIntent) Validate() error {
	if err := i.validateRequiredStrings(); err != nil {
		return err
	}
	if err := i.validateEnums(); err != nil {
		return err
	}
	if err := i.SourceRef.Validate(); err != nil {
		return err
	}
	if i.Materializer == "" {
		return fmt.Errorf("materializer is required")
	}
	return i.validateShapeTransport()
}

// validateRequiredStrings asserts that the always-required identity fields
// are non-empty and returns the matching error otherwise.
func (i ResourceIntent) validateRequiredStrings() error {
	required := []struct {
		field, name string
	}{
		{i.IntentID, "intent_id"},
		{i.Project, "project"},
		{i.Bucket, "bucket"},
		{i.LogicalName, "logical_name"},
		{i.TargetPath, "target_path"},
	}
	for _, r := range required {
		if r.field == "" {
			return fmt.Errorf("%s is required", r.name)
		}
	}
	return nil
}

// validateEnums verifies each policy/enum field is one of the allowed values
// and returns a typed error for the first invalid or empty entry.
func (i ResourceIntent) validateEnums() error {
	if err := validateEnum("ownership", string(i.Ownership), []string{
		string(ResourceOwnershipSharedRepo),
		string(ResourceOwnershipPlatformRepo),
		string(ResourceOwnershipUserHome),
	}); err != nil {
		return err
	}
	if err := validateEnum("shape", string(i.Shape), []string{
		string(ResourceShapeDirectDir),
		string(ResourceShapeDirectFile),
		string(ResourceShapeRenderSingle),
		string(ResourceShapeRenderFanout),
	}); err != nil {
		return err
	}
	if err := validateEnum("transport", string(i.Transport), []string{
		string(ResourceTransportSymlink),
		string(ResourceTransportHardlink),
		string(ResourceTransportWrite),
	}); err != nil {
		return err
	}
	if err := validateEnum("replace_policy", string(i.ReplacePolicy), []string{
		string(ResourceReplaceNever),
		string(ResourceReplaceIfManaged),
		string(ResourceReplaceAllowlistedImportedDirOnly),
	}); err != nil {
		return err
	}
	return validateEnum("prune_policy", string(i.PrunePolicy), []string{
		string(ResourcePruneNone),
		string(ResourcePruneTarget),
		string(ResourcePruneGeneratedChildren),
	})
}

// validateEnum returns an error when value is empty (required) or not one of
// allowed.
func validateEnum(name, value string, allowed []string) error {
	if value == "" {
		return fmt.Errorf("%s is required", name)
	}
	for _, a := range allowed {
		if value == a {
			return nil
		}
	}
	return fmt.Errorf("%s %q is unsupported", name, value)
}

// validateShapeTransport enforces the shape↔transport compatibility matrix:
// direct shapes must not use Write; render shapes must use Write.
func (i ResourceIntent) validateShapeTransport() error {
	switch i.Shape {
	case ResourceShapeDirectDir, ResourceShapeDirectFile:
		if i.Transport == ResourceTransportWrite {
			return fmt.Errorf("shape %q cannot use transport %q", i.Shape, i.Transport)
		}
	case ResourceShapeRenderSingle, ResourceShapeRenderFanout:
		if i.Transport != ResourceTransportWrite {
			return fmt.Errorf("shape %q requires transport %q", i.Shape, ResourceTransportWrite)
		}
	}
	return nil
}
