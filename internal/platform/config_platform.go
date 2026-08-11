package platform

import "github.com/AGOrcha/dot-agents/internal/config"

// EnabledPlatforms returns the platforms enabled in cfg, WITHOUT the
// is-installed probe InstalledEnabledPlatforms applies. Order matches All().
//
// This is the set the managed-.gitignore block is keyed off (§15 D14): the
// block is committed, so it must describe what the CONFIG says this project
// projects, not what happens to be installed on the machine running the
// command. Keying it off install state would make the same repo produce a
// different block on a teammate's laptop — a permanent diff instead of a
// converging one. `da install` and `da refresh` both derive their block input
// here so the two commands cannot drift apart.
func EnabledPlatforms(cfg *config.Config) []Platform {
	var out []Platform
	for _, p := range All() {
		if cfg.IsPlatformEnabled(p.ID()) {
			out = append(out, p)
		}
	}
	return out
}

// InstalledEnabledPlatforms returns platforms that are enabled in cfg and detected
// as installed on this machine. Order matches All().
func InstalledEnabledPlatforms(cfg *config.Config) []Platform {
	var out []Platform
	for _, p := range All() {
		if !cfg.IsPlatformEnabled(p.ID()) {
			continue
		}
		if p.IsInstalled() {
			out = append(out, p)
		}
	}
	return out
}
