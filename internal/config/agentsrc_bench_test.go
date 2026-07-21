package config

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

const agentsRCBenchSchema = "https://agorcha.dev/schemas/agentsrc.schema.json"

func writeAgentsRCBenchFile(b testing.TB, path, content string) {
	b.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		b.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		b.Fatal(err)
	}
}

func buildLoadAgentsRCFixture(b testing.TB, n int) string {
	b.Helper()
	projectDir := b.TempDir()
	manifest := &AgentsRC{
		Schema:   agentsRCBenchSchema,
		Version:  2,
		Project:  testProject,
		Sources:  []Source{{Type: testSourceTypeLocal}},
		Skills:   make([]string, n),
		Agents:   make([]string, n),
		Packages: make([]PackageRef, n),
		Features: make(map[string]string, n),
	}
	for i := range n {
		manifest.Skills[i] = fmt.Sprintf("skill-%04d", i)
		manifest.Agents[i] = fmt.Sprintf("agent-%04d", i)
		manifest.Packages[i] = PackageRef{Ref: fmt.Sprintf("bench:package-%04d@v1", i)}
		manifest.Features[fmt.Sprintf("feature-%04d", i)] = "enabled"
	}
	if err := manifest.Save(projectDir); err != nil {
		b.Fatalf("Save fixture: %v", err)
	}
	return projectDir
}

func buildGenerateAgentsRCFixture(b testing.TB, n int) (string, string) {
	b.Helper()
	home := b.TempDir()
	projectDir := b.TempDir()
	for i := range n {
		name := fmt.Sprintf("resource-%04d", i)
		writeAgentsRCBenchFile(b, filepath.Join(home, "skills", testProject, name, testSkillMarkerFile), "# skill\n")
		writeAgentsRCBenchFile(b, filepath.Join(home, "agents", testProject, name, "AGENT.md"), "# agent\n")
		writeAgentsRCBenchFile(b, filepath.Join(home, "rules", testProject, name+".md"), "# rule\n")
	}
	writeAgentsRCBenchFile(b, filepath.Join(home, "settings", testProject, "claude-code.json"), `{"hooks":{"PreToolUse":[{"command":"true"}],"PostToolUse":[{"command":"true"}]}}`)
	writeAgentsRCBenchFile(b, filepath.Join(home, "settings", testProject, "cursor.json"), "{}")
	writeAgentsRCBenchFile(b, filepath.Join(home, "mcp", testProject, testMCPJSONFile), `{"servers":{"server-a":{},"server-b":{}}}`)
	return home, projectDir
}

func BenchmarkLoadAgentsRC(b *testing.B) {
	for _, n := range []int{10, 50, 200} {
		b.Run(fmt.Sprintf("N=%d", n), func(b *testing.B) {
			projectDir := buildLoadAgentsRCFixture(b, n)
			if _, err := LoadAgentsRC(projectDir); err != nil {
				b.Fatalf("warm LoadAgentsRC: %v", err)
			}

			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				if _, err := LoadAgentsRC(projectDir); err != nil {
					b.Fatalf("LoadAgentsRC: %v", err)
				}
			}
		})
	}
}

func BenchmarkGenerateAgentsRC(b *testing.B) {
	for _, n := range []int{10, 50, 200} {
		b.Run(fmt.Sprintf("N=%d", n), func(b *testing.B) {
			home, projectDir := buildGenerateAgentsRCFixture(b, n)
			b.Setenv("AGENTS_HOME", home)
			if _, err := GenerateAgentsRC(testProject, projectDir); err != nil {
				b.Fatalf("warm GenerateAgentsRC: %v", err)
			}

			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				if _, err := GenerateAgentsRC(testProject, projectDir); err != nil {
					b.Fatalf("GenerateAgentsRC: %v", err)
				}
			}
		})
	}
}
