package platform

import (
	"os"
	"path/filepath"
	"strings"
)

// HookProvenance answers exactly one question: was this hook command found
// in a harness config file put there by `da refresh` rendering an existing
// canonical hook bundle?
//
// The import path needs that answer because rendering and importing are
// inverses of each other operating on DIFFERENT name spaces. A bundle's
// directory name is chosen by its author (`isp-gate`); the name the
// importer derives for a captured entry is a heuristic over the canonical
// event plus the command stem (`pre-compact-gate`). Whenever those two
// disagree — the normal case, and unavoidable for a multi-event bundle
// that renders under several event names — an import run over freshly
// rendered config sees an entry that no existing bundle path "owns" and
// mints a brand new bundle for it. That bundle renders on the next
// refresh, is captured again under a name that now collides, and the
// non-destructive `-N` alternate naming turns the mismatch into unbounded
// bundle growth (`pre-compact-gate-2`, `-3`, … ) on every cycle.
//
// Name equality can therefore never be the ownership test. The stable
// identity of a rendered entry is its COMMAND: every renderer emits
// exactly ResolveHookCommand(spec) into every harness shape (Claude
// settings, Codex/Cursor hooks.json, Copilot per-hook files), so a command
// is bundle-owned when either
//
//   - it equals the resolved command of some existing bundle, or
//   - it executes a file that lives inside some existing bundle's
//     directory — the shape every scaffolded bundle uses, where a relative
//     `./script.sh` is resolved by ResolveHookCommand into an absolute path
//     under <agentsHome>/hooks/<scope>/<bundle>/.
//
// Both signals are derived from what is on disk at the moment the question
// is asked, so the answer stays correct as bundles are renamed, added, or
// removed, and no provenance marker has to be smuggled into vendor config
// schemas that would reject it.
type HookProvenance struct {
	// commands maps a fully resolved hook command to "<scope>/<name>".
	commands map[string]string
	// dirs maps an absolute canonical bundle directory to "<scope>/<name>".
	dirs map[string]string
}

// NewHookProvenance indexes the canonical hook bundles that currently exist
// under agentsHome. An empty agentsHome, a missing hooks bucket, or a scope
// whose manifests fail to load all degrade to "owns nothing" — provenance
// never fails the caller, it only ever declines to claim ownership.
func NewHookProvenance(agentsHome string) HookProvenance {
	p := HookProvenance{
		commands: map[string]string{},
		dirs:     map[string]string{},
	}
	agentsHome = strings.TrimSpace(agentsHome)
	if agentsHome == "" {
		return p
	}
	entries, err := os.ReadDir(filepath.Join(agentsHome, "hooks"))
	if err != nil {
		return p
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		p.indexScope(agentsHome, entry.Name())
	}
	return p
}

// indexScope records every canonical bundle in one scope. A scope whose
// manifests do not load is skipped: reporting that fault belongs to the
// hooks loader on the render path, and an import must not be aborted by it.
func (p HookProvenance) indexScope(agentsHome, scope string) {
	specs, err := listCanonicalHookSpecs(agentsHome, scope)
	if err != nil {
		return
	}
	for _, spec := range specs {
		owner := scope + "/" + spec.Name
		p.dirs[filepath.Clean(filepath.Dir(spec.SourcePath))] = owner
		if command := strings.TrimSpace(ResolveHookCommand(spec)); command != "" {
			p.commands[command] = owner
		}
	}
}

// Owner reports the "<scope>/<name>" bundle a rendered hook command belongs
// to. A false result means no canonical bundle explains this command, so it
// is genuinely hand-authored content that import should capture.
func (p HookProvenance) Owner(command string) (string, bool) {
	command = strings.TrimSpace(command)
	if command == "" {
		return "", false
	}
	if owner, ok := p.commands[command]; ok {
		return owner, true
	}
	for _, field := range strings.Fields(command) {
		if owner, ok := p.ownerOfPath(field); ok {
			return owner, true
		}
	}
	return "", false
}

// ownerOfPath reports the bundle whose directory contains token. Only
// absolute paths are considered: a relative token in harness config is not
// something a renderer emitted, since ResolveHookCommand absolutizes every
// bundle-relative command before it reaches config.
func (p HookProvenance) ownerOfPath(token string) (string, bool) {
	if !filepath.IsAbs(token) {
		return "", false
	}
	token = filepath.Clean(token)
	for dir, owner := range p.dirs {
		if strings.HasPrefix(token, dir+string(filepath.Separator)) {
			return owner, true
		}
	}
	return "", false
}
