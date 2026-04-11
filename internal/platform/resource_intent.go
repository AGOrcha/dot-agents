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
	if i.IntentID == "" {
		return fmt.Errorf("intent_id is required")
	}
	if i.Project == "" {
		return fmt.Errorf("project is required")
	}
	if i.Bucket == "" {
		return fmt.Errorf("bucket is required")
	}
	if i.LogicalName == "" {
		return fmt.Errorf("logical_name is required")
	}
	if i.TargetPath == "" {
		return fmt.Errorf("target_path is required")
	}
	switch i.Ownership {
	case ResourceOwnershipSharedRepo, ResourceOwnershipPlatformRepo, ResourceOwnershipUserHome:
	case "":
		return fmt.Errorf("ownership is required")
	default:
		return fmt.Errorf("ownership %q is unsupported", i.Ownership)
	}
	if err := i.SourceRef.Validate(); err != nil {
		return err
	}
	switch i.Shape {
	case ResourceShapeDirectDir, ResourceShapeDirectFile, ResourceShapeRenderSingle, ResourceShapeRenderFanout:
	case "":
		return fmt.Errorf("shape is required")
	default:
		return fmt.Errorf("shape %q is unsupported", i.Shape)
	}
	switch i.Transport {
	case ResourceTransportSymlink, ResourceTransportHardlink, ResourceTransportWrite:
	case "":
		return fmt.Errorf("transport is required")
	default:
		return fmt.Errorf("transport %q is unsupported", i.Transport)
	}
	switch i.ReplacePolicy {
	case ResourceReplaceNever, ResourceReplaceIfManaged, ResourceReplaceAllowlistedImportedDirOnly:
	case "":
		return fmt.Errorf("replace_policy is required")
	default:
		return fmt.Errorf("replace_policy %q is unsupported", i.ReplacePolicy)
	}
	switch i.PrunePolicy {
	case ResourcePruneNone, ResourcePruneTarget, ResourcePruneGeneratedChildren:
	case "":
		return fmt.Errorf("prune_policy is required")
	default:
		return fmt.Errorf("prune_policy %q is unsupported", i.PrunePolicy)
	}
	if i.Materializer == "" {
		return fmt.Errorf("materializer is required")
	}

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
