package settings

// settings has no readlinker-style collaborator seams today: the package
// does not touch the symlink graph (it only inspects file metadata under
// ~/.agents/settings/<scope>/ via internal/platform helpers).
//
// This file exists so the subpackage matches the agents/ + skills/ shape
// referenced by plan root-command-decomposition t10b. The interface-DI
// follow-up (t15) is the natural place to introduce real seams here once
// settings-specific collaborators surface.
