package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

const (
	errReadUnits = "ReadUnits: %v"
	digestBase   = "sha256:base"
	refArtifact  = "oci:tool/fmt@1.2.3"
	digestInputs = "sha256:inputs"
	refUnitGitA1 = "git:a@1"
)

func TestWriteUnitsLockRoundTrip(t *testing.T) {
	dir := t.TempDir()
	in := UnitsLock{
		InputsDigest: digestInputs,
		Units: map[string]LockedUnit{
			"acme:org/base@a1b2": {
				Kind:          UnitKindLayer,
				Digest:        digestBase,
				FetchedAt:     "2026-05-30T10:00:00Z",
				LastCheckedAt: "2026-05-30T10:00:00Z",
			},
			refArtifact: {
				Kind:      UnitKindArtifact,
				Digest:    "sha256:fmt",
				FetchedAt: "2026-05-30T11:00:00Z",
			},
		},
	}
	if err := WriteUnitsLock(dir, in); err != nil {
		t.Fatalf("WriteUnitsLock: %v", err)
	}

	got, err := ReadUnits(dir)
	if err != nil {
		t.Fatalf(errReadUnits, err)
	}
	if got.InputsDigest != digestInputs {
		t.Fatalf("inputs_digest = %q", got.InputsDigest)
	}
	layer := got.Units["acme:org/base@a1b2"]
	if layer.Kind != UnitKindLayer || layer.Digest != digestBase {
		t.Fatalf("layer round-trip mismatch: %+v", layer)
	}
	art := got.Units[refArtifact]
	if art.Kind != UnitKindArtifact || art.LastCheckedAt != "" {
		t.Fatalf("artifact round-trip mismatch: %+v", art)
	}
}

func TestWriteUnitsLockNilUnits(t *testing.T) {
	dir := t.TempDir()
	if err := WriteUnitsLock(dir, UnitsLock{InputsDigest: "sha256:x"}); err != nil {
		t.Fatalf("WriteUnitsLock nil units: %v", err)
	}
	// The section must serialize as an empty object, not null.
	raw, _ := os.ReadFile(AgentsLockPath(dir))
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		t.Fatalf("lockfile invalid: %v", err)
	}
	if string(top[LockSectionUnits]) != "{}" {
		t.Fatalf("units section = %s, want {}", top[LockSectionUnits])
	}
}

func TestWriteUnitsLockPreservesAdapters(t *testing.T) {
	dir := t.TempDir()
	// Graph lifecycle already wrote an adapters section; the units writer must
	// leave it intact (§7A.3 — separate writer).
	seed := `{"lock_version":1,"adapters":{"kuzu":{"source_digest":"sha256:aa"}}}`
	if err := os.WriteFile(AgentsLockPath(dir), []byte(seed), 0o600); err != nil {
		t.Fatal(err)
	}
	in := UnitsLock{
		InputsDigest: digestInputs,
		Units:        map[string]LockedUnit{"git:a/b@c": {Kind: UnitKindLayer, Digest: "sha256:d"}},
	}
	if err := WriteUnitsLock(dir, in); err != nil {
		t.Fatalf("WriteUnitsLock: %v", err)
	}
	raw, _ := os.ReadFile(AgentsLockPath(dir))
	var top map[string]struct {
		Kuzu struct {
			SourceDigest string `json:"source_digest"`
		} `json:"kuzu"`
	}
	_ = json.Unmarshal(raw, &top)
	if top["adapters"].Kuzu.SourceDigest != "sha256:aa" {
		t.Fatalf("adapters clobbered: %s", raw)
	}
}

func TestReadUnitsEmptyLockfile(t *testing.T) {
	dir := t.TempDir() // no lockfile at all
	got, err := ReadUnits(dir)
	if err != nil {
		t.Fatalf(errReadUnits, err)
	}
	if got.Units == nil || len(got.Units) != 0 || got.InputsDigest != "" {
		t.Fatalf("absent lockfile must yield empty units/digest: %+v", got)
	}
}

func TestReadUnitsMigratesLegacyV1(t *testing.T) {
	dir := t.TempDir()
	// A v1 lockfile with the old per-tier config + packages sections and no
	// units section — ReadUnits must migrate it in memory (§7A.3).
	seed := `{
  "lock_version": 1,
  "config": {
    "acme:org/base": {
      "resolved_sha": "sha256:base",
      "fetched_at": "2026-05-01T00:00:00Z",
      "ttl_expires_at": "2026-05-02T00:00:00Z",
      "cache_key": "ck:git:sha256:base"
    }
  },
  "packages": {
    "oci:tool/fmt@1.2.3": {
      "resolved_sha": "sha256:fmt",
      "fetched_at": "2026-05-01T01:00:00Z"
    }
  }
}`
	if err := os.WriteFile(AgentsLockPath(dir), []byte(seed), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := ReadUnits(dir)
	if err != nil {
		t.Fatalf(errReadUnits, err)
	}
	if len(got.Units) != 2 {
		t.Fatalf("expected 2 migrated units, got %d: %+v", len(got.Units), got.Units)
	}
	layer := got.Units["acme:org/base"]
	if layer.Kind != UnitKindLayer {
		t.Fatalf("config layer not tagged layer: %+v", layer)
	}
	// resolved_sha → digest; ttl_expires_at is dropped; fetched_at seeds
	// last_checked_at (review-nudge basis).
	if layer.Digest != digestBase || layer.LastCheckedAt != "2026-05-01T00:00:00Z" {
		t.Fatalf("layer migration mismatch: %+v", layer)
	}
	// cache_key MUST survive legacy→units migration so the §7A.4 cache-key
	// staleness gate keeps working on an upgraded lock; ttl_expires_at must NOT.
	if layer.CacheKey != "ck:git:sha256:base" {
		t.Fatalf("cache_key dropped in migration: %+v", layer)
	}
	art := got.Units[refArtifact]
	if art.Kind != UnitKindArtifact || art.Digest != "sha256:fmt" {
		t.Fatalf("package not migrated to artifact: %+v", art)
	}
}

func TestReadUnitsPrefersUnitsOverLegacy(t *testing.T) {
	dir := t.TempDir()
	// When both a units section and a legacy config section exist (mid-migration
	// on-disk), the units section wins — no double-counting from migration.
	seed := `{
  "lock_version": 1,
  "inputs_digest": "sha256:in",
  "units": {"git:a@1": {"kind": "layer", "digest": "sha256:new"}},
  "config": {"git:a@1": {"resolved_sha": "sha256:old", "fetched_at": "t"}}
}`
	if err := os.WriteFile(AgentsLockPath(dir), []byte(seed), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := ReadUnits(dir)
	if err != nil {
		t.Fatalf(errReadUnits, err)
	}
	if len(got.Units) != 1 || got.Units[refUnitGitA1].Digest != "sha256:new" {
		t.Fatalf("units section must win over legacy config: %+v", got.Units)
	}
	if got.InputsDigest != "sha256:in" {
		t.Fatalf("inputs_digest = %q", got.InputsDigest)
	}
}

func TestReadUnitsLegacyConfigOnly(t *testing.T) {
	dir := t.TempDir()
	// Only a legacy config section, no packages — migration yields one layer.
	seed := `{"lock_version":1,"config":{"acme:base":{"resolved_sha":"s","fetched_at":"t"}}}`
	if err := os.WriteFile(AgentsLockPath(dir), []byte(seed), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := ReadUnits(dir)
	if err != nil {
		t.Fatalf(errReadUnits, err)
	}
	if len(got.Units) != 1 || got.Units["acme:base"].Kind != UnitKindLayer {
		t.Fatalf("legacy-config-only migration: %+v", got.Units)
	}
}

func TestReadUnitsOpenError(t *testing.T) {
	// A directory at the lockfile path makes agentslock.Open fail to read.
	dir := t.TempDir()
	if err := os.MkdirAll(AgentsLockPath(dir), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadUnits(dir); err == nil {
		t.Fatal("expected ReadUnits error when lockfile path is a directory")
	}
}

func TestReadUnitsMalformedUnitsSection(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(AgentsLockPath(dir), []byte(`{"lock_version":1,"units":"nope"}`), 0o600)
	if _, err := ReadUnits(dir); err == nil {
		t.Fatal("expected decode error for malformed units section")
	}
}

func TestReadUnitsMalformedLegacyConfigSection(t *testing.T) {
	dir := t.TempDir()
	// No units section so migration runs; legacy config is the wrong shape.
	_ = os.WriteFile(AgentsLockPath(dir), []byte(`{"lock_version":1,"config":"nope"}`), 0o600)
	if _, err := ReadUnits(dir); err == nil {
		t.Fatal("expected migration decode error for malformed legacy config section")
	}
}

func TestReadUnitsMalformedLegacyPackagesSection(t *testing.T) {
	dir := t.TempDir()
	// Valid (absent) config but malformed packages — exercises the second
	// mergeLegacySection branch during migration.
	_ = os.WriteFile(AgentsLockPath(dir), []byte(`{"lock_version":1,"packages":"nope"}`), 0o600)
	if _, err := ReadUnits(dir); err == nil {
		t.Fatal("expected migration decode error for malformed legacy packages section")
	}
}

func TestWriteUnitsLockOpenError(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(AgentsLockPath(dir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := WriteUnitsLock(dir, UnitsLock{}); err == nil {
		t.Fatal("expected WriteUnitsLock error when lockfile path is a directory")
	}
}

func TestWriteUnitsLockFlushError(t *testing.T) {
	// Parent dir does not exist → Flush's atomic write fails.
	missing := filepath.Join(t.TempDir(), "no-such-dir")
	if err := WriteUnitsLock(missing, UnitsLock{}); err == nil {
		t.Fatal("expected flush error when project dir is missing")
	}
}
