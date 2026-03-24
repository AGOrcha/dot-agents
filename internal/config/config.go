package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Config represents the ~/.agents/config.json structure.
type Config struct {
	Version  int                `json:"version"`
	Defaults Defaults           `json:"defaults,omitempty"`
	Projects map[string]Project `json:"projects"`
	Agents   map[string]Agent   `json:"agents,omitempty"`
	Features Features           `json:"features,omitempty"`
}

type Defaults struct {
	Agent string `json:"agent,omitempty"`
}

type Project struct {
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

// Load reads config.json from AgentsHome.
func Load() (*Config, error) {
	path := filepath.Join(AgentsHome(), "config.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Config{
				Version:  1,
				Projects: make(map[string]Project),
				Agents:   make(map[string]Agent),
			}, nil
		}
		return nil, fmt.Errorf("reading config: %w", err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}
	if cfg.Projects == nil {
		cfg.Projects = make(map[string]Project)
	}
	if cfg.Agents == nil {
		cfg.Agents = make(map[string]Agent)
	}
	return &cfg, nil
}

// Save writes config.json to AgentsHome.
func (c *Config) Save() error {
	path := filepath.Join(AgentsHome(), "config.json")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("creating config dir: %w", err)
	}

	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}
	data = append(data, '\n')

	return os.WriteFile(path, data, 0644)
}

// AddProject registers a project in the config.
func (c *Config) AddProject(name, path string) {
	if c.Projects == nil {
		c.Projects = make(map[string]Project)
	}
	c.Projects[name] = Project{
		Path:  path,
		Added: time.Now().UTC(),
	}
}

// RemoveProject unregisters a project from the config.
func (c *Config) RemoveProject(name string) {
	delete(c.Projects, name)
}

// GetProjectPath returns the path for a registered project, or empty string.
func (c *Config) GetProjectPath(name string) string {
	if p, ok := c.Projects[name]; ok {
		return p.Path
	}
	return ""
}

// ListProjects returns all registered project names.
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
