package platform

import "github.com/NikashPrakash/dot-agents/internal/config"

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
