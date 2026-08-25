package skill_pipeline

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"SentinelOps/internal/ai/policy"
	airuntime "SentinelOps/internal/ai/runtime"

	localbackend "github.com/cloudwego/eino-ext/adk/backend/local"
	"github.com/cloudwego/eino/adk/filesystem"
	"github.com/cloudwego/eino/adk/middlewares/skill"
	"github.com/cloudwego/eino/schema"
	"gopkg.in/yaml.v3"
)

const defaultSkillMaxBytes int64 = 64 * 1024

// ValidationPolicy 描述项目对官方 Skill Backend 结果追加的安全约束。
type ValidationPolicy struct {
	BaseDir            string
	MaxBytes           int64
	AllowedFrontMatter map[string]struct{}
}

// ValidatedSkills 是启动时逐个 Get 后冻结的 Skill 只读目录。
type ValidatedSkills struct {
	Skills    []skill.Skill
	Snapshots []airuntime.SkillSnapshot
}

// ValidateBackend 对官方 Backend.List 的每个结果主动调用 Get，并只追加项目 Policy 校验。
func ValidateBackend(ctx context.Context, backend skill.Backend, policyConfig ValidationPolicy) (ValidatedSkills, error) {
	if ctx == nil {
		return ValidatedSkills{}, errors.New("skill validation context is required")
	}
	if backend == nil {
		return ValidatedSkills{}, errors.New("skill backend is required")
	}
	baseDir, err := cleanAbsoluteDir(policyConfig.BaseDir)
	if err != nil {
		return ValidatedSkills{}, err
	}
	maxBytes := policyConfig.MaxBytes
	if maxBytes <= 0 {
		maxBytes = defaultSkillMaxBytes
	}
	allowed := policyConfig.AllowedFrontMatter
	if len(allowed) == 0 {
		allowed = defaultAllowedFrontMatter()
	}
	matters, err := backend.List(ctx)
	if err != nil {
		return ValidatedSkills{}, fmt.Errorf("list skills: %w", err)
	}
	result := ValidatedSkills{Skills: make([]skill.Skill, 0, len(matters)), Snapshots: make([]airuntime.SkillSnapshot, 0, len(matters))}
	seen := make(map[string]struct{}, len(matters))
	provider, _ := backend.(rawFrontMatterProvider)
	for index, matter := range matters {
		name := strings.TrimSpace(matter.Name)
		if name == "" {
			return ValidatedSkills{}, fmt.Errorf("skill %d has empty name", index)
		}
		loaded, getErr := backend.Get(ctx, name)
		if getErr != nil {
			return ValidatedSkills{}, fmt.Errorf("get skill %q: %w", name, getErr)
		}
		if strings.TrimSpace(loaded.Name) != name {
			return ValidatedSkills{}, fmt.Errorf("skill %q Get returned name %q", name, loaded.Name)
		}
		if _, duplicate := seen[name]; duplicate {
			return ValidatedSkills{}, fmt.Errorf("duplicate skill name %q", name)
		}
		seen[name] = struct{}{}
		if err := validateSkill(ctx, loaded, baseDir, maxBytes, allowed, provider); err != nil {
			return ValidatedSkills{}, fmt.Errorf("skill %q: %w", name, err)
		}
		result.Skills = append(result.Skills, loaded)
		result.Snapshots = append(result.Snapshots, airuntime.SkillSnapshot{Name: name, ContentHash: airuntime.SkillContentHash(loaded.Content)})
	}
	sort.Slice(result.Skills, func(i, j int) bool { return result.Skills[i].Name < result.Skills[j].Name })
	sort.Slice(result.Snapshots, func(i, j int) bool { return result.Snapshots[i].Name < result.Snapshots[j].Name })
	return result, nil
}

// ValidateL0ToolNames 只允许从既有严格 Registry 获取 L0 durable Tool。
func ValidateL0ToolNames(names []string) error {
	seen := make(map[string]struct{}, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			return errors.New("skill Tool name is required")
		}
		if _, duplicate := seen[name]; duplicate {
			return fmt.Errorf("duplicate skill Tool %q", name)
		}
		seen[name] = struct{}{}
		if err := policy.RequireExecutable(name); err != nil {
			return fmt.Errorf("skill Tool %q is not L0: %w", name, err)
		}
	}
	return nil
}

func validateSkill(ctx context.Context, item skill.Skill, baseDir string, maxBytes int64, allowed map[string]struct{}, provider rawFrontMatterProvider) error {
	if strings.TrimSpace(item.Description) == "" {
		return errors.New("description is empty")
	}
	if item.Context != "" {
		return fmt.Errorf("context mode %q is disabled", item.Context)
	}
	if strings.TrimSpace(item.Agent) != "" {
		return errors.New("agent override is disabled")
	}
	if strings.TrimSpace(item.Model) != "" {
		return errors.New("model override is disabled")
	}
	if int64(len([]byte(item.Content))) > maxBytes {
		return fmt.Errorf("content size exceeds %d bytes", maxBytes)
	}
	if err := ensurePathWithin(baseDir, item.BaseDirectory); err != nil {
		return err
	}
	if provider != nil {
		keys, err := provider.FrontMatterKeys(ctx, item.Name)
		if err != nil {
			return err
		}
		for _, key := range keys {
			if _, ok := allowed[key]; !ok {
				return fmt.Errorf("frontmatter field %q is not allowed", key)
			}
		}
	}
	return nil
}

func defaultAllowedFrontMatter() map[string]struct{} {
	return map[string]struct{}{"name": {}, "description": {}, "context": {}, "agent": {}, "model": {}}
}

func cleanAbsoluteDir(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", errors.New("skill BaseDir is required")
	}
	if !filepath.IsAbs(value) {
		return "", fmt.Errorf("skill BaseDir must be absolute: %q", value)
	}
	return filepath.Clean(value), nil
}

func ensurePathWithin(baseDir, candidate string) error {
	if strings.TrimSpace(candidate) == "" || !filepath.IsAbs(candidate) {
		return errors.New("skill BaseDirectory must be absolute")
	}
	if resolvedBase, baseErr := filepath.EvalSymlinks(baseDir); baseErr == nil {
		if resolvedCandidate, candidateErr := filepath.EvalSymlinks(candidate); candidateErr == nil {
			baseDir, candidate = resolvedBase, resolvedCandidate
		}
	}
	rel, err := filepath.Rel(baseDir, filepath.Clean(candidate))
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("skill BaseDirectory %q escapes BaseDir %q", candidate, baseDir)
	}
	return nil
}

// ReadOnlyFilesystemBackend 是 local backend 的最薄只读适配；Skill middleware 只能看到 filesystem.Backend 的读接口。
type ReadOnlyFilesystemBackend struct {
	delegate filesystem.Backend
}

// NewReadOnlyLocalBackend 创建 local backend 并关闭 Write/Edit/Shell 能力。
func NewReadOnlyLocalBackend(ctx context.Context) (*ReadOnlyFilesystemBackend, error) {
	if ctx == nil {
		return nil, errors.New("filesystem backend context is required")
	}
	delegate, err := localbackend.NewBackend(ctx, &localbackend.Config{ValidateCommand: func(string) error {
		return errors.New("skill backend command execution is disabled")
	}})
	if err != nil {
		return nil, err
	}
	return &ReadOnlyFilesystemBackend{delegate: delegate}, nil
}

// NewSkillBackendFromFilesystem 使用官方 Skill Backend 读取一层版本化 Skill 目录。
func NewSkillBackendFromFilesystem(ctx context.Context, baseDir string) (skill.Backend, error) {
	baseDir, err := cleanAbsoluteDir(baseDir)
	if err != nil {
		return nil, err
	}
	backend, err := NewReadOnlyLocalBackend(ctx)
	if err != nil {
		return nil, err
	}
	delegate, err := skill.NewBackendFromFilesystem(ctx, &skill.BackendFromFilesystemConfig{Backend: backend, BaseDir: baseDir})
	if err != nil {
		return nil, err
	}
	return &validatedSkillBackend{delegate: delegate, raw: backend}, nil
}

func (b *ReadOnlyFilesystemBackend) LsInfo(ctx context.Context, req *filesystem.LsInfoRequest) ([]filesystem.FileInfo, error) {
	return b.delegate.LsInfo(ctx, req)
}

func (b *ReadOnlyFilesystemBackend) Read(ctx context.Context, req *filesystem.ReadRequest) (*filesystem.FileContent, error) {
	return b.delegate.Read(ctx, req)
}

func (b *ReadOnlyFilesystemBackend) GrepRaw(ctx context.Context, req *filesystem.GrepRequest) ([]filesystem.GrepMatch, error) {
	return b.delegate.GrepRaw(ctx, req)
}

func (b *ReadOnlyFilesystemBackend) GlobInfo(ctx context.Context, req *filesystem.GlobInfoRequest) ([]filesystem.FileInfo, error) {
	return b.delegate.GlobInfo(ctx, req)
}

func (*ReadOnlyFilesystemBackend) Write(context.Context, *filesystem.WriteRequest) error {
	return errors.New("skill backend Write is disabled")
}

func (*ReadOnlyFilesystemBackend) Edit(context.Context, *filesystem.EditRequest) error {
	return errors.New("skill backend Edit is disabled")
}

// Execute 保留官方 Shell 形状但始终拒绝，防止类型断言绕过只读策略。
func (*ReadOnlyFilesystemBackend) Execute(context.Context, *filesystem.ExecuteRequest) (*filesystem.ExecuteResponse, error) {
	return nil, errors.New("skill backend Execute is disabled")
}

// ExecuteStreaming 保留官方 StreamingShell 形状但始终拒绝。
func (*ReadOnlyFilesystemBackend) ExecuteStreaming(context.Context, *filesystem.ExecuteRequest) (*schema.StreamReader[*filesystem.ExecuteResponse], error) {
	return nil, errors.New("skill backend ExecuteStreaming is disabled")
}

type rawFrontMatterProvider interface {
	FrontMatterKeys(context.Context, string) ([]string, error)
}

type validatedSkillBackend struct {
	delegate skill.Backend
	raw      *ReadOnlyFilesystemBackend
}

func (b *validatedSkillBackend) List(ctx context.Context) ([]skill.FrontMatter, error) {
	return b.delegate.List(ctx)
}

func (b *validatedSkillBackend) Get(ctx context.Context, name string) (skill.Skill, error) {
	return b.delegate.Get(ctx, name)
}

func (b *validatedSkillBackend) FrontMatterKeys(ctx context.Context, name string) ([]string, error) {
	item, err := b.delegate.Get(ctx, name)
	if err != nil {
		return nil, err
	}
	return b.raw.FrontMatterKeys(ctx, filepath.Join(item.BaseDirectory, "SKILL.md"))
}

func (b *ReadOnlyFilesystemBackend) FrontMatterKeys(ctx context.Context, path string) ([]string, error) {
	content, err := b.Read(ctx, &filesystem.ReadRequest{FilePath: path})
	if err != nil {
		return nil, err
	}
	return frontMatterKeys(content.Content)
}

func frontMatterKeys(data string) ([]string, error) {
	data = strings.TrimSpace(data)
	if !strings.HasPrefix(data, "---") {
		return nil, errors.New("skill file does not start with frontmatter delimiter")
	}
	rest := data[len("---"):]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return nil, errors.New("skill frontmatter closing delimiter not found")
	}
	var node yaml.Node
	if err := yaml.Unmarshal([]byte(strings.TrimSpace(rest[:end])), &node); err != nil {
		return nil, fmt.Errorf("parse frontmatter keys: %w", err)
	}
	if len(node.Content) == 0 || node.Content[0].Kind != yaml.MappingNode {
		return nil, errors.New("skill frontmatter must be a mapping")
	}
	keys := make([]string, 0, len(node.Content[0].Content)/2)
	for index := 0; index+1 < len(node.Content[0].Content); index += 2 {
		keys = append(keys, node.Content[0].Content[index].Value)
	}
	return keys, nil
}

var _ filesystem.Backend = (*ReadOnlyFilesystemBackend)(nil)
var _ filesystem.Shell = (*ReadOnlyFilesystemBackend)(nil)
var _ filesystem.StreamingShell = (*ReadOnlyFilesystemBackend)(nil)
