package graphstore

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestVenvBinSubdirs_OrderByOS(t *testing.T) {
	got := venvBinSubdirs()
	if len(got) != 2 {
		t.Fatalf("want 2 subdirs, got %v", got)
	}
	first := "bin"
	if runtime.GOOS == "windows" {
		first = "Scripts"
	}
	if got[0] != first {
		t.Errorf("on %s first subdir = %q, want %q", runtime.GOOS, got[0], first)
	}
}

func TestVenvExeCandidates_CoversLayouts(t *testing.T) {
	cands := venvExeCandidates("/venv", "code-review-graph")
	if len(cands) == 0 {
		t.Fatal("no candidates")
	}
	// bin/ and Scripts/ both represented regardless of host.
	var hasBin, hasScripts bool
	for _, c := range cands {
		if filepath.Base(filepath.Dir(c)) == "bin" {
			hasBin = true
		}
		if filepath.Base(filepath.Dir(c)) == "Scripts" {
			hasScripts = true
		}
	}
	if !hasBin || !hasScripts {
		t.Errorf("candidates must cover bin+Scripts: %v", cands)
	}
	if runtime.GOOS == "windows" {
		var hasExe bool
		for _, c := range cands {
			if filepath.Ext(c) == ".exe" {
				hasExe = true
			}
		}
		if !hasExe {
			t.Errorf("windows candidates must include .exe: %v", cands)
		}
	}
}

// crgBinFileName returns the host-correct executable name for the CRG binary.
func crgBinFileName() string {
	name := crgBinName
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return name
}

// writeVenvCRGBin creates root/.venv/<sub>/<crgBinFileName> and returns its path.
func writeVenvCRGBin(t *testing.T, root, sub string) string {
	t.Helper()
	binDir := filepath.Join(root, ".venv", sub)
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(binDir, crgBinFileName())
	if err := os.WriteFile(exe, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return exe
}

func TestDiscoverCRGBin_RepoVenvAndParentAndMissing(t *testing.T) {
	sub := venvBinSubdirs()[0] // host-correct primary layout

	t.Run("repo_root_venv_hit", func(t *testing.T) {
		repo := t.TempDir()
		exe := writeVenvCRGBin(t, repo, sub)
		got, err := DiscoverCRGBin(repo)
		if err != nil || got != exe {
			t.Fatalf("repo .venv: got %q err %v, want %q", got, err, exe)
		}
	})

	t.Run("parent_venv_hit", func(t *testing.T) {
		parent := t.TempDir()
		child := filepath.Join(parent, "child")
		if err := os.MkdirAll(child, 0o755); err != nil {
			t.Fatal(err)
		}
		pexe := writeVenvCRGBin(t, parent, sub)
		got, err := DiscoverCRGBin(child)
		if err != nil || got != pexe {
			t.Fatalf("parent .venv: got %q err %v, want %q", got, err, pexe)
		}
	})

	t.Run("missing_and_not_on_path_errors", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())
		if _, err := DiscoverCRGBin(t.TempDir()); err == nil {
			t.Error("expected not-found error when no .venv and not on PATH")
		}
	})
}

func TestPythonBin_ResolvesSiblingThenFallback(t *testing.T) {
	dir := t.TempDir()
	venvBin := filepath.Join(dir, ".venv", "bin")
	if err := os.MkdirAll(venvBin, 0o755); err != nil {
		t.Fatal(err)
	}
	crg := filepath.Join(venvBin, "code-review-graph")
	if err := os.WriteFile(crg, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	pyName := "python3"
	if runtime.GOOS == "windows" {
		pyName = "python.exe"
	}
	py := filepath.Join(venvBin, pyName)
	if err := os.WriteFile(py, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	b := &CRGBridge{RepoRoot: dir, Bin: crg}
	if got := b.pythonBin(); got != py {
		t.Errorf("pythonBin sibling = %q, want %q", got, py)
	}

	// No sibling python → bare-name fallback.
	b2 := &CRGBridge{RepoRoot: dir, Bin: filepath.Join(t.TempDir(), "code-review-graph")}
	got := b2.pythonBin()
	if runtime.GOOS == "windows" {
		if got != "python" {
			t.Errorf("windows fallback = %q, want python", got)
		}
	} else if got != "python3" {
		t.Errorf("posix fallback = %q, want python3", got)
	}
}
