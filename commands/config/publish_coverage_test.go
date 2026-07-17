package config

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	cfg "github.com/AGOrcha/dot-agents/internal/config"
)

// pubCovOptions builds a runPublishOptions with the injected stdout/cwd surface
// and the loadRC/publish seams stubbed so runPublish is exercised without cobra
// or a real OCI registry. cwd is a placeholder — a stubbed loadRC ignores it.
func pubCovOptions(path, ref string, jsonOut bool) *runPublishOptions {
	return &runPublishOptions{
		runContext: runContext{
			jsonOut: jsonOut,
			stdout:  &bytes.Buffer{},
			stderr:  &bytes.Buffer{},
			cwd:     "/pub-cov",
		},
		path: path,
		ref:  ref,
	}
}

// pubCovRC returns a loadRC seam yielding an AgentsRC with the given sources.
func pubCovRC(sources ...cfg.Source) func(string) (*cfg.AgentsRC, error) {
	return func(string) (*cfg.AgentsRC, error) {
		return &cfg.AgentsRC{Sources: sources}, nil
	}
}

func pubCovOut(opts *runPublishOptions) string {
	return opts.stdout.(*bytes.Buffer).String()
}

// ---------- runPublish: arg/validation error branches ----------

func TestRunPublish_BadRefRejected(t *testing.T) {
	opts := pubCovOptions("./tree", "no-at-version", false)
	opts.loadRC = pubCovRC() // must not be reached
	err := runPublish(opts, testDeps())
	he, ok := err.(*hintError)
	if !ok || !strings.Contains(he.message, "invalid publish ref") {
		t.Fatalf("expected invalid-ref hintError, got %v", err)
	}
}

func TestRunPublish_LoadRCError(t *testing.T) {
	opts := pubCovOptions("./tree", "acme-oci:skill/x@v1.0.0", false)
	opts.loadRC = func(string) (*cfg.AgentsRC, error) {
		return nil, errors.New("boom reading manifest")
	}
	err := runPublish(opts, testDeps())
	he, ok := err.(*hintError)
	if !ok || !strings.Contains(he.message, "could not load .agentsrc.json") {
		t.Fatalf("expected load-error hintError, got %v", err)
	}
}

func TestRunPublish_SourceNotDeclared(t *testing.T) {
	opts := pubCovOptions("./tree", "acme-oci:skill/x@v1.0.0", false)
	opts.loadRC = pubCovRC(cfg.Source{ID: "other", Type: "oci"})
	err := runPublish(opts, testDeps())
	he, ok := err.(*hintError)
	if !ok || !strings.Contains(he.message, `source "acme-oci" is not declared`) {
		t.Fatalf("expected undeclared-source hintError, got %v", err)
	}
}

func TestRunPublish_SourceWrongType(t *testing.T) {
	opts := pubCovOptions("./tree", "acme-oci:skill/x@v1.0.0", false)
	opts.loadRC = pubCovRC(cfg.Source{ID: "acme-oci", Type: "git"})
	err := runPublish(opts, testDeps())
	he, ok := err.(*hintError)
	if !ok || !strings.Contains(he.message, `is type "git"; publish requires an oci source`) {
		t.Fatalf("expected wrong-type hintError, got %v", err)
	}
}

func TestRunPublish_PublishSeamError(t *testing.T) {
	opts := pubCovOptions("./tree", "acme-oci:skill/x@v1.0.0", false)
	opts.loadRC = pubCovRC(cfg.Source{ID: "acme-oci", Type: "oci"})
	opts.publish = func(context.Context, cfg.Source, cfg.PackageRefParts, string) (cfg.PublishResult, error) {
		return cfg.PublishResult{}, errors.New("registry rejected push")
	}
	err := runPublish(opts, testDeps())
	he, ok := err.(*hintError)
	if !ok || !strings.Contains(he.message, "publish failed") {
		t.Fatalf("expected publish-failed hintError, got %v", err)
	}
}

// ---------- runPublish: happy paths ----------

func TestRunPublish_HumanSuccess(t *testing.T) {
	opts := pubCovOptions("./skills/review-pr", "acme-oci:skill/review-pr@v1.0.0", false)
	opts.loadRC = pubCovRC(cfg.Source{ID: "acme-oci", Type: "oci"})
	var gotParts cfg.PackageRefParts
	opts.publish = func(_ context.Context, src cfg.Source, parts cfg.PackageRefParts, dirPath string) (cfg.PublishResult, error) {
		gotParts = parts
		if src.ID != "acme-oci" || dirPath != "./skills/review-pr" {
			t.Fatalf("publish seam got src=%+v dir=%q", src, dirPath)
		}
		return cfg.PublishResult{ManifestDigest: "sha256:mmm", LayerDigest: "sha256:lll"}, nil
	}
	if err := runPublish(opts, testDeps()); err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	out := pubCovOut(opts)
	for _, want := range []string{
		"Published ./skills/review-pr",
		"acme-oci:skill/review-pr@v1.0.0",
		"sha256:mmm",
		"sha256:lll",
		"acme-oci:skill/review-pr@pinned:sha256:lll",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("human output missing %q:\n%s", want, out)
		}
	}
	if gotParts.SourceID != "acme-oci" || gotParts.ArtifactPath != "skill/review-pr" || gotParts.VersionSpec != "v1.0.0" {
		t.Fatalf("parsed parts wrong: %+v", gotParts)
	}
}

func TestRunPublish_JSONSuccess(t *testing.T) {
	opts := pubCovOptions("./tree", "acme-oci:skill/x@v2", true)
	opts.loadRC = pubCovRC(cfg.Source{ID: "acme-oci", Type: "oci"})
	opts.publish = func(context.Context, cfg.Source, cfg.PackageRefParts, string) (cfg.PublishResult, error) {
		return cfg.PublishResult{ManifestDigest: "sha256:MAN", LayerDigest: "sha256:LAY"}, nil
	}
	if err := runPublish(opts, testDeps()); err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	var report PublishReport
	if err := json.Unmarshal([]byte(pubCovOut(opts)), &report); err != nil {
		t.Fatalf("json decode: %v (%s)", err, pubCovOut(opts))
	}
	if !report.OK || report.Ref != "acme-oci:skill/x@v2" || report.Path != "./tree" {
		t.Fatalf("unexpected report: %+v", report)
	}
	if report.ManifestDigest != "sha256:MAN" || report.LayerDigest != "sha256:LAY" {
		t.Fatalf("unexpected digests: %+v", report)
	}
	if report.PinnedRef != "acme-oci:skill/x@pinned:sha256:LAY" {
		t.Fatalf("unexpected pinned ref: %q", report.PinnedRef)
	}
}

// TestRunPublish_NilSeamsFallBack drives the nil-loadRC / nil-publish default
// branches: with no injected seams, runPublish falls back to cfg.LoadAgentsRC
// (reading the temp project's real .agentsrc.json) and, once it resolves a real
// oci source, to cfg.PublishTree — which fails pre-network on a nonexistent
// resource-tree path, covering the fallback assignment lines without a registry.
func TestRunPublish_NilSeamsFallBack(t *testing.T) {
	project := withRepoLayer(t, `{"project":"demo","version":2,"sources":[{"id":"acme-oci","type":"oci","url":"example.com/repo"}]}`, "")
	opts := pubCovOptions("./no-such-tree", "acme-oci:skill/x@v1.0.0", false)
	opts.cwd = project // real load path
	err := runPublish(opts, testDeps())
	if err == nil {
		t.Fatalf("expected an error from the real PublishTree fallback on a missing tree")
	}
	if _, ok := err.(*hintError); !ok {
		t.Fatalf("expected a hintError from the publish fallback, got %T: %v", err, err)
	}
}

// ---------- newPublishCmd wiring ----------

func TestNewPublishCmd_Execute(t *testing.T) {
	project := withRepoLayer(t, `{"project":"demo","version":2,"sources":[{"id":"acme-oci","type":"git","url":"u"}]}`, "")
	prev, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(prev) })
	if err := os.Chdir(project); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	cmd := newPublishCmd(testDeps())
	if !strings.Contains(cmd.Use, "publish") {
		t.Fatalf("unexpected Use: %q", cmd.Use)
	}
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	// Source resolves but is type "git", so RunE (bind → runPublish) returns the
	// wrong-type hintError before any network — proving the full cobra wiring.
	cmd.SetArgs([]string{"./tree", "acme-oci:skill/x@v1.0.0"})
	err := cmd.Execute()
	if err == nil {
		t.Fatalf("expected a wrong-source-type error from the wired command")
	}
	if !strings.Contains(err.Error(), "publish requires an oci source") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestNewPublishCmd_BindError covers the RunE's bind-failure branch: with an
// unset cwd and getwd returning an error, bind fails and RunE returns before
// touching runPublish.
func TestNewPublishCmd_BindError(t *testing.T) {
	orig := getwd
	t.Cleanup(func() { getwd = orig })
	getwd = func() (string, error) { return "", errors.New("no cwd") }

	cmd := newPublishCmd(testDeps())
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"./tree", "acme-oci:skill/x@v1.0.0"})
	if err := cmd.Execute(); err == nil {
		t.Fatalf("expected bind to fail when getwd errors")
	}
}
