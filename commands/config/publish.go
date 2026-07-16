package config

import (
	"context"
	"fmt"

	cfg "github.com/AGOrcha/dot-agents/internal/config"
	"github.com/spf13/cobra"
)

// publish.go adds `da config publish` — the OCI producer half of D9 (consume:
// an `oci` source packages ref; publish: this command). Per the package-
// artifact-install spec's BLOCKER #4 owner ruling (2026-07-15), publish lands
// on the canonical unified resource CRUD surface (`da config`, the subtree
// that already owns every other effective-config + lock verb: explain, sync,
// lint, verify) rather than reviving the retired `da packages` command tree.

// PublishReport is the stable JSON shape emitted by `da config publish
// --json`. LayerDigest is the value a subsequent `pinned:sha256:<digest>`
// packages ref should use — it is what the shared content-addressed packages
// cache and the lock key on (see verifyOCIPin's doc comment in
// internal/config/fetcher_oci.go), so pinning it round-trips through the
// offline cache (R8). ManifestDigest is reported too for operator visibility
// and because a pin that names it instead is also honored on pull.
type PublishReport struct {
	OK             bool   `json:"ok"`
	Ref            string `json:"ref"`
	Path           string `json:"path"`
	ManifestDigest string `json:"manifest_digest"`
	LayerDigest    string `json:"layer_digest"`
	PinnedRef      string `json:"pinned_ref"`
}

// runPublishOptions captures one invocation's state. The shared stdout/
// stderr/cwd/json surface comes from the embedded runContext; loadRC and
// publish are injected so the run path is table-drivable without cobra or a
// real registry (tests point publish at an in-process test registry instead
// of stubbing it out entirely, so the real pack+push path is exercised).
type runPublishOptions struct {
	runContext
	path string
	ref  string
	// loadRC loads the project's declared sources; nil falls back to
	// cfg.LoadAgentsRC.
	loadRC func(projectPath string) (*cfg.AgentsRC, error)
	// publish packs dirPath and pushes it per parts to src; nil falls back to
	// cfg.PublishTree.
	publish func(ctx context.Context, src cfg.Source, parts cfg.PackageRefParts, dirPath string) (cfg.PublishResult, error)
}

func newPublishCmd(deps Deps) *cobra.Command {
	opts := &runPublishOptions{}
	cmd := &cobra.Command{
		Use:   "publish <path> <source-id>:<artifact-path>@<version-spec>",
		Short: "Pack a resource tree and push it as an OCI artifact-bundle",
		Long: `Pack the resource tree at <path> into a typed +tar+gzip artifact-bundle
(the same H1 fail-closed format and normalizer a ` + "`packages`" + ` pull decodes
— package-artifact-install spec D9/H1) and push it to the declared ` + "`oci`" + `
source named by <source-id>, at <artifact-path>:<version-spec> (an explicit
tag; publish does not accept a pinned:sha256: target).

On success it prints the pushed manifest digest and the layer/payload
digest. Pin the LAYER digest in a subsequent packages ref
(source-id:artifact-path@pinned:sha256:<layer-digest>) to round-trip through
the offline content-addressed cache (R8).

The source named by <source-id> must already be declared in .agentsrc.json
with type "oci"; this command does not create sources.`,
		Example: exampleBlock(
			"  da config publish ./skills/review-pr acme-oci:skill/review-pr@v1.0.0",
			"  da config publish ./skills/review-pr acme-oci:skill/review-pr@v1.0.0 --json",
		),
		Args: deps.ExactArgsWithHints(2,
			"`da config publish` takes exactly two arguments: the resource tree path and a source-id:artifact-path@version-spec ref.",
		),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.bind(cmd, deps); err != nil {
				return err
			}
			opts.path = args[0]
			opts.ref = args[1]
			return runPublish(opts, deps)
		},
	}
	return cmd
}

func runPublish(opts *runPublishOptions, deps Deps) error {
	parts, err := cfg.ParsePackageRef(opts.ref)
	if err != nil {
		return deps.ErrorWithHints(
			fmt.Sprintf("invalid publish ref %q", opts.ref),
			err.Error(),
			"Refs look like source-id:artifact-path@version-spec, e.g. acme-oci:skill/review-pr@v1.0.0.",
		)
	}

	loadRC := opts.loadRC
	if loadRC == nil {
		loadRC = cfg.LoadAgentsRC
	}
	rc, err := loadRC(opts.cwd)
	if err != nil {
		return deps.ErrorWithHints("could not load .agentsrc.json", err.Error())
	}

	src, ok := cfg.FindSource(rc.Sources, parts.SourceID)
	if !ok {
		return deps.ErrorWithHints(
			fmt.Sprintf("source %q is not declared in .agentsrc.json", parts.SourceID),
			"Declare it under the `sources` array with `type: \"oci\"` before publishing to it.",
		)
	}
	if src.Type != "oci" {
		return deps.ErrorWithHints(
			fmt.Sprintf("source %q is type %q; publish requires an oci source", parts.SourceID, src.Type),
		)
	}

	publish := opts.publish
	if publish == nil {
		publish = cfg.PublishTree
	}
	result, err := publish(context.Background(), src, parts, opts.path)
	if err != nil {
		return deps.ErrorWithHints("publish failed", err.Error())
	}

	report := PublishReport{
		OK:             true,
		Ref:            opts.ref,
		Path:           opts.path,
		ManifestDigest: result.ManifestDigest,
		LayerDigest:    result.LayerDigest,
		PinnedRef:      fmt.Sprintf("%s:%s@pinned:%s", parts.SourceID, parts.ArtifactPath, result.LayerDigest),
	}

	if opts.jsonOut {
		return writeJSON(opts.stdout, report)
	}
	fmt.Fprintf(opts.stdout, "Published %s\n", opts.path)
	fmt.Fprintf(opts.stdout, "  ref:              %s\n", opts.ref)
	fmt.Fprintf(opts.stdout, "  manifest digest:  %s\n", report.ManifestDigest)
	fmt.Fprintf(opts.stdout, "  layer digest:     %s\n", report.LayerDigest)
	fmt.Fprintf(opts.stdout, "  pinned ref:       %s\n", report.PinnedRef)
	return nil
}
