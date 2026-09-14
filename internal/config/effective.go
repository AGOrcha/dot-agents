package config

// effective.go is the ONE layered-config read seam for consumers that are not
// already holding a resolved snapshot.
//
// Before it existed, every such consumer called LoadAgentsRC and read the flat
// repo-local manifest, so an org/team layer that supplied the key was invisible
// to them. Adding an independent EnsureResolved call per consumer is the wrong
// repair in the other direction: the default mode REWRITES .agentsrc.lock when
// the lock is stale, which would turn read-only, best-effort, hook-driven
// commands (workflow publication, `da observability status`) into lock writers
// and network fetchers.
//
// LoadEffectiveAgentsRC therefore fixes the access MODE once, for all of them:
// read-only, offline, no lock write, no fetch.

// LoadEffectiveAgentsRC returns the LAYERED effective manifest for projectPath:
// product defaults → user-local → imported `extends` layers → repo-local →
// project-local overlay, merged under the §7.2 category rules.
//
// Access mode is Frozen (EnsureResolved → ResolveLocked): the committed lock is
// taken as authoritative, nothing is fetched, and neither .agentsrc.lock nor the
// layer cache is written. A project with no `extends` degrades to the FLAT layer
// set and needs no lock at all, so flat projects behave exactly as they did
// under LoadAgentsRC apart from now honoring the user-local layer.
//
// A project that DOES declare `extends` but has no usable lock/cache returns an
// error instead of silently falling back to the flat repo manifest: a raw
// fallback would answer with a different layer precedence than the one the
// caller asked for, which is precisely the bug this seam exists to remove. The
// lock is produced by `da install` / `da refresh` / `da config sync`.
func LoadEffectiveAgentsRC(projectPath string) (*AgentsRC, error) {
	res, err := EnsureResolved(projectPath, EnsureOpts{Frozen: true})
	if err != nil {
		return nil, err
	}
	return &res.Snapshot.Effective, nil
}
