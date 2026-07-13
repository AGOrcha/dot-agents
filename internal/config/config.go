package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// configSchemaVersion is the current config.json shape. v2 is the L3 home-config
// split: config.json becomes the SYNCED portable identity registry (id +
// portable key, NO path), and the per-machine id→path rows move to the
// machine-local binding table (~/.agents/local/bindings.json). A decoded v1
// config (paths inline) is migrated in memory and persisted on the next mutating
// Save (FORK-5 — Load stays pure).
const configSchemaVersion = 2

// Config represents the ~/.agents/config.json structure — the SYNCED portable
// surface. Absolute project paths NEVER appear here; they live in the
// machine-local binding table loaded from ~/.agents/local/ (home-config defects
// 1 & 2). The unexported bindings/upgradeNeeded fields are deliberately not
// JSON-tagged so json.Marshal can never leak a path into the synced tree.
type Config struct {
	Version  int                `json:"version"`
	Defaults Defaults           `json:"defaults,omitempty"`
	Projects map[string]Project `json:"projects"`
	Agents   map[string]Agent   `json:"agents,omitempty"`
	Features Features           `json:"features,omitempty"`

	// bindings is the machine-local id→absolute-path table. Loaded by Load from
	// ~/.agents/local/bindings.json, persisted by Save, and NEVER marshaled into
	// config.json — it must not reach the synced git tree (R5/R7).
	bindings map[string]Binding
	// upgradeNeeded records that Load decoded a legacy v1 (path-bearing)
	// config.json. Load itself writes nothing (FORK-5); the next Save persists
	// the split shape.
	upgradeNeeded bool
}

type Defaults struct {
	Agent string `json:"agent,omitempty"`
}

// Project is one row of the SYNCED portable identity registry (§15 D13/A2
// project-set unit). The stable id is the map key; RepoID is the portable
// rebind key (FORK-1 hybrid). It carries NO absolute path — paths live in the
// machine-local binding table (Binding), never in the synced config.json.
type Project struct {
	// RepoID is the canonical git repo_id used as the portable rebind key.
	// It is recorded ONLY when the project's git origin is unambiguous (R12);
	// otherwise it is empty and rebinding falls back to the logical id (the
	// registry map key).
	RepoID string `json:"repo_id,omitempty"`
}

// Binding is one row of the MACHINE-LOCAL binding table: a project id resolved
// to its absolute path on THIS machine. Never synced (R5/R7) — it lives under
// the gitignored ~/.agents/local/ (FORK-3).
type Binding struct {
	Path  string    `json:"path"`
	Added time.Time `json:"added"`
}

type Agent struct {
	Enabled bool   `json:"enabled"`
	Version string `json:"version,omitempty"`
}

type Features struct {
	Tasks   bool `json:"tasks,omitempty"`
	History bool `json:"history,omitempty"`
	Sync    bool `json:"sync,omitempty"`
}

// projectWire decodes a config.json project row that may be in either shape: the
// v2 identity (repo_id only) or the legacy v1 row carrying the machine-local
// path/added inline. Load splits a v1 row into (identity, binding) in memory so
// the path is migrated rather than dropped (R9).
type projectWire struct {
	RepoID string    `json:"repo_id,omitempty"`
	Path   string    `json:"path,omitempty"`
	Added  time.Time `json:"added,omitempty"`
}

type configWire struct {
	Version  int                    `json:"version"`
	Defaults Defaults               `json:"defaults,omitempty"`
	Projects map[string]projectWire `json:"projects"`
	Agents   map[string]Agent       `json:"agents,omitempty"`
	Features Features               `json:"features,omitempty"`
}

// bindingTableFile is the on-disk shape of the machine-local binding table.
type bindingTableFile struct {
	Version  int                `json:"version"`
	Bindings map[string]Binding `json:"bindings"`
}

func newEmptyConfig() *Config {
	return &Config{
		Version:  configSchemaVersion,
		Projects: make(map[string]Project),
		Agents:   make(map[string]Agent),
		bindings: make(map[string]Binding),
	}
}

// Load reads config.json from AgentsHome and the machine-local binding table
// from ~/.agents/local/. It is a PURE read (FORK-5): a legacy v1 config is
// migrated in memory (paths folded into the binding table) and flagged via
// UpgradeNeeded, but nothing is written until the next Save.
func Load() (*Config, error) {
	path := filepath.Join(AgentsHome(), "config.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			cfg := newEmptyConfig()
			bindings, berr := loadBindings()
			if berr != nil {
				return nil, berr
			}
			cfg.bindings = bindings
			return cfg, nil
		}
		return nil, fmt.Errorf("reading config: %w", err)
	}

	var wire configWire
	if err := json.Unmarshal(data, &wire); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	cfg := &Config{
		Version:  wire.Version,
		Defaults: wire.Defaults,
		Projects: make(map[string]Project, len(wire.Projects)),
		Agents:   wire.Agents,
		Features: wire.Features,
	}
	if cfg.Agents == nil {
		cfg.Agents = make(map[string]Agent)
	}

	legacy := map[string]Binding{}
	for name, p := range wire.Projects {
		cfg.Projects[name] = Project{RepoID: p.RepoID}
		if p.Path != "" {
			legacy[name] = Binding{Path: filepath.Clean(p.Path), Added: p.Added}
		}
	}

	bindings, err := loadBindings()
	if err != nil {
		return nil, err
	}
	cfg.bindings = bindings
	foldLegacyBindings(cfg, legacy, wire.Version)
	return cfg, nil
}

// foldLegacyBindings merges any legacy inline project paths the binding table
// does not already carry (first migration on this machine) into cfg.bindings,
// in memory only — persisted by Save. It also flags an upgrade when the config
// predates the current schema version or any legacy path was folded.
func foldLegacyBindings(cfg *Config, legacy map[string]Binding, wireVersion int) {
	for name, b := range legacy {
		if _, ok := cfg.bindings[name]; !ok {
			cfg.bindings[name] = b
		}
	}
	if wireVersion < configSchemaVersion || len(legacy) > 0 {
		cfg.upgradeNeeded = true
	}
}

// bindingsPath is the machine-local binding table file. It lives under
// ~/.agents/local/, which `da sync init` gitignores, so it never enters the
// synced tree (R7, FORK-3).
func bindingsPath() string {
	return filepath.Join(AgentsHome(), "local", "bindings.json")
}

// loadBindings reads the machine-local binding table; a missing file is an
// empty table (the common case on a freshly-synced machine B).
func loadBindings() (map[string]Binding, error) {
	data, err := os.ReadFile(bindingsPath())
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]Binding), nil
		}
		return nil, fmt.Errorf("reading binding table: %w", err)
	}
	var f bindingTableFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parsing binding table: %w", err)
	}
	if f.Bindings == nil {
		return make(map[string]Binding), nil
	}
	return f.Bindings, nil
}

// Save persists the SYNCED identity registry (config.json) and the
// MACHINE-LOCAL binding table (~/.agents/local/bindings.json), clearing
// UpgradeNeeded once a migrated v1 config has been rewritten in the v2 shape.
//
// Ordering is load-bearing: the binding table is written FIRST, then the
// path-free config.json. A legacy v1 config carries the project path INLINE; if
// the path-free config.json were written first and the binding write then
// failed, the path would be gone from BOTH files (data loss). Writing bindings
// first means any failure leaves config.json untouched — the recoverable old
// shape still carries the path, so a retried Save recovers cleanly.
func (c *Config) Save() error {
	if err := c.saveBindings(); err != nil {
		return err
	}
	if err := c.saveConfig(); err != nil {
		return err
	}
	c.upgradeNeeded = false
	return nil
}

func (c *Config) saveConfig() error {
	path := filepath.Join(AgentsHome(), "config.json")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("creating config dir: %w", err)
	}
	c.Version = configSchemaVersion
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0644)
}

func (c *Config) saveBindings() error {
	path := bindingsPath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("creating binding table dir: %w", err)
	}
	f := bindingTableFile{Version: configSchemaVersion, Bindings: c.bindings}
	if f.Bindings == nil {
		f.Bindings = make(map[string]Binding)
	}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling binding table: %w", err)
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0644)
}

func (c *Config) ensureMaps() {
	if c.Projects == nil {
		c.Projects = make(map[string]Project)
	}
	if c.bindings == nil {
		c.bindings = make(map[string]Binding)
	}
}

// AddProject registers a project: it records the portable identity (deriving a
// trustworthy repo_id per the FORK-1 hybrid / R12 guard) in the synced registry
// AND the machine-local id→path binding. The two surfaces are written together
// by the next Save.
func (c *Config) AddProject(name, path string) {
	c.ensureMaps()
	clean := filepath.Clean(path)
	repoID, ambiguous := DeriveTrustedRepoID(clean)
	if ambiguous {
		repoID = "" // R12: an ambiguous/multi-remote origin is not a trusted key
	}
	c.Projects[name] = Project{RepoID: repoID}
	c.bindings[name] = Binding{Path: clean, Added: time.Now().UTC()}
}

// BindProject establishes (or repairs) ONLY the machine-local path binding for a
// project whose identity is already known — e.g. rebinding on machine B. It does
// NOT mutate the synced identity registry (R5).
func (c *Config) BindProject(name, path string) {
	c.ensureMaps()
	b := c.bindings[name]
	b.Path = filepath.Clean(path)
	if b.Added.IsZero() {
		b.Added = time.Now().UTC()
	}
	c.bindings[name] = b
}

// RemoveProject unregisters a project from both surfaces.
func (c *Config) RemoveProject(name string) {
	delete(c.Projects, name)
	delete(c.bindings, name)
}

// DropLocalBindings clears the machine-local binding table while preserving the
// synced identity registry. `da init --from` calls it when Load reports a legacy
// v1 home was cloned (UpgradeNeeded): the inline paths a v1 config carries are
// the SOURCE machine's absolute paths and are invalid on this machine, so they
// must be discarded rather than inherited (home-config defect 1). The project
// identities still travel; each project is then reported known-but-unbound until
// rebound on THIS machine (R5/R7). It does not touch the synced registry.
func (c *Config) DropLocalBindings() {
	c.bindings = make(map[string]Binding)
}

// GetProjectPath returns the machine-local path bound to a project, or "" when
// the project is known but unbound on this machine.
func (c *Config) GetProjectPath(name string) string {
	if b, ok := c.bindings[name]; ok {
		return filepath.Clean(b.Path)
	}
	return ""
}

// ProjectBinding returns the machine-local binding row for a project.
func (c *Config) ProjectBinding(name string) (Binding, bool) {
	b, ok := c.bindings[name]
	return b, ok
}

// ProjectRepoID returns the portable rebind key recorded for a project, or "".
func (c *Config) ProjectRepoID(name string) string {
	return c.Projects[name].RepoID
}

// IsProjectKnown reports whether a project is in the synced identity registry.
func (c *Config) IsProjectKnown(name string) bool {
	_, ok := c.Projects[name]
	return ok
}

// IsProjectBound reports whether a project has a machine-local path binding.
func (c *Config) IsProjectBound(name string) bool {
	_, ok := c.bindings[name]
	return ok
}

// UpgradeNeeded reports whether Load decoded a legacy v1 config.json whose
// machine-local paths must be persisted into the split shape by the next Save.
func (c *Config) UpgradeNeeded() bool {
	return c.upgradeNeeded
}

// ListProjects returns all registered project names from the identity registry.
func (c *Config) ListProjects() []string {
	names := make([]string, 0, len(c.Projects))
	for name := range c.Projects {
		names = append(names, name)
	}
	return names
}

// SetPlatformState updates enabled/version for a platform in config.
func (c *Config) SetPlatformState(platform string, enabled bool, version string) {
	if c.Agents == nil {
		c.Agents = make(map[string]Agent)
	}
	c.Agents[platform] = Agent{Enabled: enabled, Version: version}
}

// IsPlatformEnabled checks if a platform is enabled. Defaults to true if not set.
func (c *Config) IsPlatformEnabled(platform string) bool {
	a, ok := c.Agents[platform]
	if !ok {
		// Check legacy keys
		switch platform {
		case "claude":
			a, ok = c.Agents["claude-code"]
		case "copilot":
			a, ok = c.Agents["github-copilot"]
		}
		if !ok {
			return true // default to enabled
		}
	}
	return a.Enabled
}
