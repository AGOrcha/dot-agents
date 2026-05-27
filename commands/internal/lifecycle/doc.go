// Package lifecycle hosts the project-lifecycle command cluster extracted
// from the root commands package as part of the root-command-decomposition
// plan (SHAPE.md §1, §4a).
//
// Lifecycle commands mutate or inspect a managed project's on-disk state:
// add, doctor, import, import_plugins, init, install, refresh, remove,
// status, and the platform-tagged linkcount helpers. They share the
// project-mutation helpers in internal/links, internal/platform,
// internal/projectsync, and internal/scaffold/{home,hooks}, which is why
// they cluster into a single subpackage rather than per-command
// subpackages (contrast with commands/agents/, commands/skills/, etc.,
// which are single-resource browsers).
//
// This file is part of the t02 skeleton: only doc.go and deps.go land
// here. Individual command moves arrive in tasks t03 through t09, and
// the root re-export shims are deleted in t13 once commands/root.go
// switches to importing the lifecycle.New*Cmd constructors directly.
package lifecycle
