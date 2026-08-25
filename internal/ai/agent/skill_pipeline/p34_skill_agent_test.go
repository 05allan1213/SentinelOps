package skill_pipeline

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	airuntime "SentinelOps/internal/ai/runtime"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/adk/middlewares/skill"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

type p34Backend struct {
	matters []skill.FrontMatter
	items   map[string]skill.Skill
	gets    int
}

func (b *p34Backend) List(context.Context) ([]skill.FrontMatter, error) {
	return append([]skill.FrontMatter(nil), b.matters...), nil
}

func (b *p34Backend) Get(_ context.Context, name string) (skill.Skill, error) {
	b.gets++
	item, ok := b.items[name]
	if !ok {
		return skill.Skill{}, errors.New("missing skill")
	}
	return item, nil
}

type p34Model struct{}

func (*p34Model) Generate(context.Context, []*schema.Message, ...model.Option) (*schema.Message, error) {
	return schema.AssistantMessage("ok", nil), nil
}

func (*p34Model) Stream(context.Context, []*schema.Message, ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return schema.StreamReaderFromArray([]*schema.Message{schema.AssistantMessage("ok", nil)}), nil
}

func (*p34Model) BindTools([]*schema.ToolInfo) error { return nil }

type p34Tool struct{ name string }

func (t *p34Tool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{Name: t.name, Desc: t.name}, nil
}

func (*p34Tool) InvokableRun(context.Context, string, ...tool.Option) (string, error) {
	return "ok", nil
}

func TestSkillBackendListGetAndValidatorLoadLegalSkill(t *testing.T) {
	backend := &p34Backend{
		matters: []skill.FrontMatter{{Name: "incident-triage", Description: "triage"}},
		items: map[string]skill.Skill{
			"incident-triage": {FrontMatter: skill.FrontMatter{Name: "incident-triage", Description: "triage"}, Content: "read-only SOP", BaseDirectory: "/skills/incident-triage"},
		},
	}
	validated, err := ValidateBackend(context.Background(), backend, ValidationPolicy{BaseDir: "/skills", MaxBytes: 1024})
	if err != nil {
		t.Fatal(err)
	}
	if backend.gets != 1 {
		t.Fatalf("validator Get count = %d, want 1", backend.gets)
	}
	if len(validated.Snapshots) != 1 || validated.Snapshots[0].Name != "incident-triage" {
		t.Fatalf("validated snapshot = %#v", validated.Snapshots)
	}
}

func TestSkillValidatorRejectsDuplicatePathDescriptionSizeAndOverrides(t *testing.T) {
	cases := []struct {
		name  string
		items []skill.Skill
		want  string
	}{
		{name: "duplicate", items: []skill.Skill{{FrontMatter: skill.FrontMatter{Name: "dup", Description: "x"}, BaseDirectory: "/skills/dup"}, {FrontMatter: skill.FrontMatter{Name: "dup", Description: "y"}, BaseDirectory: "/skills/dup2"}}, want: "duplicate"},
		{name: "path escape", items: []skill.Skill{{FrontMatter: skill.FrontMatter{Name: "escape", Description: "x"}, BaseDirectory: "/tmp/escape"}}, want: "BaseDir"},
		{name: "empty description", items: []skill.Skill{{FrontMatter: skill.FrontMatter{Name: "empty", Description: " "}, BaseDirectory: "/skills/empty"}}, want: "description"},
		{name: "size", items: []skill.Skill{{FrontMatter: skill.FrontMatter{Name: "large", Description: "x"}, Content: strings.Repeat("x", 20), BaseDirectory: "/skills/large"}}, want: "size"},
		{name: "fork", items: []skill.Skill{{FrontMatter: skill.FrontMatter{Name: "fork", Description: "x", Context: skill.ContextModeFork}, BaseDirectory: "/skills/fork"}}, want: "context"},
		{name: "agent override", items: []skill.Skill{{FrontMatter: skill.FrontMatter{Name: "agent", Description: "x", Agent: "other"}, BaseDirectory: "/skills/agent"}}, want: "agent"},
		{name: "model override", items: []skill.Skill{{FrontMatter: skill.FrontMatter{Name: "model", Description: "x", Model: "other"}, BaseDirectory: "/skills/model"}}, want: "model"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			matters := make([]skill.FrontMatter, 0, len(tc.items))
			items := make(map[string]skill.Skill, len(tc.items))
			for _, item := range tc.items {
				matters = append(matters, item.FrontMatter)
				items[item.Name] = item
			}
			_, err := ValidateBackend(context.Background(), &p34Backend{matters: matters, items: items}, ValidationPolicy{BaseDir: "/skills", MaxBytes: 10})
			if err == nil || !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(tc.want)) {
				t.Fatalf("ValidateBackend() error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestSkillValidatorGetsEveryListedSkillBeforeDuplicateRejection(t *testing.T) {
	backend := &p34Backend{
		matters: []skill.FrontMatter{{Name: "dup", Description: "x"}, {Name: "dup", Description: "x"}},
		items:   map[string]skill.Skill{"dup": {FrontMatter: skill.FrontMatter{Name: "dup", Description: "x"}, BaseDirectory: "/skills/dup"}},
	}
	if _, err := ValidateBackend(context.Background(), backend, ValidationPolicy{BaseDir: "/skills", MaxBytes: 100}); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("duplicate Skill accepted: %v", err)
	}
	if backend.gets != 2 {
		t.Fatalf("validator Get count = %d, want 2", backend.gets)
	}
}

func TestReadOnlyBackendDeniesMutationAndExecution(t *testing.T) {
	backend, err := NewReadOnlyLocalBackend(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := backend.Write(context.Background(), nil); err == nil {
		t.Fatal("Write unexpectedly succeeded")
	}
	if err := backend.Edit(context.Background(), nil); err == nil {
		t.Fatal("Edit unexpectedly succeeded")
	}
	if _, err := backend.Execute(context.Background(), nil); err == nil {
		t.Fatal("Execute unexpectedly succeeded")
	}
	if _, err := backend.ExecuteStreaming(context.Background(), nil); err == nil {
		t.Fatal("ExecuteStreaming unexpectedly succeeded")
	}
}

func TestSkillAgentRejectsNonL0ToolsAndUsesOfficialMiddleware(t *testing.T) {
	backend := &p34Backend{
		matters: []skill.FrontMatter{{Name: "incident-triage", Description: "triage"}},
		items:   map[string]skill.Skill{"incident-triage": {FrontMatter: skill.FrontMatter{Name: "incident-triage", Description: "triage"}, Content: "read-only", BaseDirectory: "/skills/incident-triage"}},
	}
	if _, err := BuildSkillAgent(context.Background(), Config{
		Model:          &p34Model{},
		RuntimeHandler: airuntime.NewRuntimeHandler(),
		Backend:        backend,
		BaseDir:        "/skills",
		ToolNames:      []string{"create_report"},
	}); err == nil || !strings.Contains(err.Error(), "L0") {
		t.Fatalf("mutation Tool accepted: %v", err)
	}
	cfg, err := newAgentConfig(context.Background(), Config{
		Model:          &p34Model{},
		RuntimeHandler: airuntime.NewRuntimeHandler(),
		Backend:        backend,
		BaseDir:        "/skills",
		ToolNames:      nil,
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Name != AgentName || len(cfg.Handlers) < 2 {
		t.Fatalf("skill agent config = name %q handlers %d", cfg.Name, len(cfg.Handlers))
	}
}

func TestSkillSnapshotsChangeWithContent(t *testing.T) {
	first := &p34Backend{matters: []skill.FrontMatter{{Name: "summary", Description: "summary"}}, items: map[string]skill.Skill{"summary": {FrontMatter: skill.FrontMatter{Name: "summary", Description: "summary"}, Content: "one", BaseDirectory: "/skills/summary"}}}
	second := &p34Backend{matters: []skill.FrontMatter{{Name: "summary", Description: "summary"}}, items: map[string]skill.Skill{"summary": {FrontMatter: skill.FrontMatter{Name: "summary", Description: "summary"}, Content: "two", BaseDirectory: "/skills/summary"}}}
	a, err := ValidateBackend(context.Background(), first, ValidationPolicy{BaseDir: "/skills", MaxBytes: 100})
	if err != nil {
		t.Fatal(err)
	}
	b, err := ValidateBackend(context.Background(), second, ValidationPolicy{BaseDir: "/skills", MaxBytes: 100})
	if err != nil {
		t.Fatal(err)
	}
	if reflect.DeepEqual(a.Snapshots, b.Snapshots) {
		t.Fatal("skill snapshots did not change after content change")
	}
}

func TestManifestSkillsUseOfficialReadOnlyBackend(t *testing.T) {
	baseDir, err := filepath.Abs(filepath.Join("..", "..", "..", "..", "manifest", "skills"))
	if err != nil {
		t.Fatal(err)
	}
	backend, err := NewSkillBackendFromFilesystem(context.Background(), baseDir)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := ValidateBackend(context.Background(), backend, ValidationPolicy{BaseDir: baseDir, MaxBytes: 64 * 1024})
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Snapshots) != 3 {
		t.Fatalf("manifest Skill count = %d, want 3", len(loaded.Snapshots))
	}
}

func TestSkillValidatorRejectsUnknownFrontMatterField(t *testing.T) {
	baseDir := t.TempDir()
	skillDir := filepath.Join(baseDir, "bad")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: bad\ndescription: bad\ntools: [shell]\n---\nbody\n"
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	backend, err := NewSkillBackendFromFilesystem(context.Background(), baseDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateBackend(context.Background(), backend, ValidationPolicy{BaseDir: baseDir, MaxBytes: 100}); err == nil || !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("unknown frontmatter field accepted: %v", err)
	}
}

var _ adk.Agent
