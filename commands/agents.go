package commands

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/NikashPrakash/dot-agents/internal/config"
	"github.com/NikashPrakash/dot-agents/internal/platform"
	"github.com/NikashPrakash/dot-agents/internal/ui"
	"github.com/spf13/cobra"
)

func NewAgentsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agents",
		Short: "Manage agents in ~/.agents/agents/",
		Long: `Lists and creates reusable agent definitions inside the canonical
~/.agents/agents tree. These definitions can then be distributed into projects
through refresh or install flows.`,
		Example: ExampleBlock(
			"  dot-agents agents list",
			"  dot-agents agents new reviewer",
			"  dot-agents agents promote reviewer",
			"  dot-agents agents new repo-owner billing-api",
		),
	}
	cmd.AddCommand(newAgentsListCmd())
	cmd.AddCommand(newAgentsNewCmd())
	cmd.AddCommand(newAgentsPromoteCmd())
	return cmd
}

func newAgentsListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list [project]",
		Short: "List agents",
		Example: ExampleBlock(
			"  dot-agents agents list",
			"  dot-agents agents list billing-api",
		),
		Args: MaximumNArgsWithHints(1, "Optionally pass a project scope to list project-local agents."),
		RunE: func(cmd *cobra.Command, args []string) error {
			return listAgents(scopeFromArgs(args))
		},
	}
}

func listAgents(scope string) error {
	agentsHome := config.AgentsHome()
	agentsDir := filepath.Join(agentsHome, "agents", scope)

	entries, err := os.ReadDir(agentsDir)
	if err != nil {
		ui.Info("No agents found in ~/.agents/agents/" + scope + "/")
		return nil
	}

	ui.Header("Agents (" + scope + ")")
	count := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		agentPath := filepath.Join(agentsDir, e.Name())
		agentMD := filepath.Join(agentPath, "AGENT.md")
		if _, err := os.Stat(agentMD); err == nil {
			desc := readFrontmatterDescription(agentMD)
			if desc != "" {
				ui.Bullet("ok", fmt.Sprintf("%s  %s%s%s", e.Name(), ui.Dim, desc, ui.Reset))
			} else {
				ui.Bullet("ok", e.Name())
			}
		} else {
			ui.Bullet("warn", e.Name()+" (no AGENT.md)")
		}
		count++
	}
	fmt.Fprintf(os.Stdout, "\n  %s%d agent(s) in %s scope%s\n\n", ui.Dim, count, scope, ui.Reset)
	return nil
}

func newAgentsNewCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "new <name> [project]",
		Short: "Create a new agent",
		Example: ExampleBlock(
			"  dot-agents agents new reviewer",
			"  dot-agents agents new doc-writer billing-api",
		),
		Args: RangeArgsWithHints(1, 2, "Pass an agent name and optionally a project scope."),
		RunE: func(cmd *cobra.Command, args []string) error {
			return createAgent(args[0], scopeFromArgs(args[1:]))
		},
	}
}

func createAgent(name, scope string) error {
	agentsHome := config.AgentsHome()
	agentDir := filepath.Join(agentsHome, "agents", scope, name)

	if err := os.MkdirAll(agentDir, 0755); err != nil {
		return fmt.Errorf("creating agent directory: %w", err)
	}

	agentMD := filepath.Join(agentDir, "AGENT.md")
	if err := writeAgentMDIfAbsent(agentMD, name); err != nil {
		return err
	}

	ui.SuccessBox(
		fmt.Sprintf("Created agent '%s' in ~/.agents/agents/%s/%s/", name, scope, name),
		createAgentNextSteps(agentMD, name, scope)...,
	)
	return nil
}

func scopeFromArgs(args []string) string {
	if len(args) == 0 {
		return "global"
	}
	return args[0]
}

func createAgentNextSteps(agentMD, name, scope string) []string {
	nextSteps := []string{"Edit the agent: " + config.DisplayPath(agentMD)}
	return appendAgentsRCStep(nextSteps, name, scope)
}

// writeAgentMDIfAbsent creates AGENT.md with default content when it does not yet exist.
func writeAgentMDIfAbsent(agentMD, name string) error {
	if _, err := os.Stat(agentMD); !os.IsNotExist(err) {
		return nil
	}
	content := fmt.Sprintf("---\nname: %s\ndescription: \"\"\n---\n\n# %s\n\nAgent instructions here.\n", name, name)
	if err := os.WriteFile(agentMD, []byte(content), 0644); err != nil {
		return fmt.Errorf("creating AGENT.md: %w", err)
	}
	return nil
}

func newAgentsPromoteCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "promote <name>",
		Short: "Promote a repo-local agent to shared storage",
		Long: `Promotes an agent from .agents/agents/<name>/ in the current repo to
~/.agents/agents/<project>/<name>/, registers it in .agentsrc.json, and
ensures repo symlinks under .claude/agents/.`,
		Example: ExampleBlock(
			"  dot-agents agents promote reviewer",
			"  dot-agents agents promote reviewer --force",
		),
		Args: ExactArgsWithHints(1, "Run this from the project repository that owns `.agents/agents/<name>/`."),
		RunE: func(cmd *cobra.Command, args []string) error {
			projectPath, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("resolving project path: %w", err)
			}
			return promoteAgentIn(args[0], projectPath, force)
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "Replace an existing real directory at the canonical path (destructive)")
	return cmd
}

// promoteAgentIn promotes a repo-local agent (.agents/agents/<name>/) into the
// shared agents store. The canonical location (~/.agents/agents/<project>/<name>/)
// becomes the real directory, and the repo-local path is converted to a managed
// symlink pointing at it.
func promoteAgentIn(name, projectPath string, force bool) error {
	sourcePath := filepath.Join(projectPath, ".agents", "agents", name)

	sourceInfo, err := os.Lstat(sourcePath)
	if err != nil {
		return fmt.Errorf("agent %q not found in .agents/agents/: %w", name, err)
	}

	rc, err := config.LoadAgentsRC(projectPath)
	if err != nil {
		return fmt.Errorf("loading .agentsrc.json: %w", err)
	}
	projectName := rc.Project
	if projectName == "" {
		return fmt.Errorf(".agentsrc.json has no project name set")
	}

	agentsHome := config.AgentsHome()
	destDir := filepath.Join(agentsHome, "agents", projectName)
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return fmt.Errorf("creating agents directory: %w", err)
	}
	canonicalPath := filepath.Join(destDir, name)

	if sourceInfo.Mode()&os.ModeSymlink != 0 {
		existing, err := os.Readlink(sourcePath)
		if err != nil {
			return fmt.Errorf("reading existing symlink for agent %q: %w", name, err)
		}
		if existing != canonicalPath {
			return fmt.Errorf("agent %q is already a symlink but points to %q, not the canonical path %q", name, existing, canonicalPath)
		}
	} else {
		if _, err := os.Stat(filepath.Join(sourcePath, "AGENT.md")); err != nil {
			return fmt.Errorf("agent %q not found in .agents/agents/ (expected AGENT.md at %s/AGENT.md)", name, sourcePath)
		}
		if fi, err := os.Lstat(canonicalPath); err == nil {
			if fi.Mode()&os.ModeSymlink != 0 {
				if err := os.Remove(canonicalPath); err != nil {
					return fmt.Errorf("removing stale canonical symlink for agent %q: %w", name, err)
				}
			} else if fi.IsDir() {
				if !force {
					return fmt.Errorf("agent %q already exists at canonical path %s as a real directory; use --force to overwrite", name, canonicalPath)
				}
				if err := os.RemoveAll(canonicalPath); err != nil {
					return fmt.Errorf("removing existing canonical directory for agent %q: %w", name, err)
				}
			} else {
				return fmt.Errorf("agent %q already exists at canonical path %s; remove the file and retry", name, canonicalPath)
			}
		}
		if err := copyAgentDir(sourcePath, canonicalPath); err != nil {
			return fmt.Errorf("copying agent %q to canonical path: %w", name, err)
		}
		if err := os.RemoveAll(sourcePath); err != nil {
			return fmt.Errorf("removing repo-local agent directory for %q: %w", name, err)
		}
		if err := os.Symlink(canonicalPath, sourcePath); err != nil {
			return fmt.Errorf("creating repo-local managed symlink for agent %q: %w", name, err)
		}
	}

	rc.Agents = config.AppendUnique(rc.Agents, name)
	if err := rc.Save(projectPath); err != nil {
		return fmt.Errorf("updating .agentsrc.json: %w", err)
	}

	intents, err := platform.BuildSharedAgentMirrorIntents(projectName, filepath.Join(".claude", "agents"))
	if err != nil {
		ui.Bullet("warn", "building agent mirror intents: "+err.Error())
	} else {
		plan, perr := platform.BuildResourcePlan(intents)
		if perr != nil {
			ui.Bullet("warn", "agent mirror plan: "+perr.Error())
		} else if err := plan.Execute(projectPath, config.AgentsHome()); err != nil {
			ui.Bullet("warn", "platform agent symlink refresh failed: "+err.Error())
		}
	}

	ui.SuccessBox(
		fmt.Sprintf("Promoted agent '%s' for project '%s'", name, projectName),
		fmt.Sprintf("Registered in .agentsrc.json (%d agent(s) total)", len(rc.Agents)),
		"Run 'dot-agents refresh' to sync across all platforms",
	)
	return nil
}

// copyAgentDir recursively copies the directory tree at src to dst, preserving
// file modes. Symlinks in the source tree are skipped.
func copyAgentDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		destPath := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(destPath, 0755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		return os.WriteFile(destPath, data, info.Mode())
	})
}

// appendAgentsRCStep auto-updates .agentsrc.json for project-scoped agents and
// returns nextSteps with an optional confirmation message appended.
func appendAgentsRCStep(nextSteps []string, name, scope string) []string {
	if scope == "global" {
		return nextSteps
	}
	cfg, err := config.Load()
	if err != nil {
		return nextSteps
	}
	projPath := cfg.GetProjectPath(scope)
	if projPath == "" {
		return nextSteps
	}
	rc, err := config.LoadAgentsRC(projPath)
	if err != nil {
		return nextSteps
	}
	rc.Agents = config.AppendUnique(rc.Agents, name)
	if err := rc.Save(projPath); err == nil {
		nextSteps = append(nextSteps, "Updated .agentsrc.json with agent '"+name+"'")
	}
	return nextSteps
}
