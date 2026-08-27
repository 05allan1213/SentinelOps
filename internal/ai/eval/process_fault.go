package eval

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"syscall"
)

// LocalProcessTarget 绑定本轮显式 PID 与非敏感命令片段，避免 PID 复用时误伤其他进程。
type LocalProcessTarget struct {
	PID             int    `json:"pid"`
	CommandContains string `json:"command_contains"`
}

// LocalProcessManifest 只列出 sentinelops-final 本轮允许暂停的 Worker 和依赖进程。
type LocalProcessManifest struct {
	Workers      map[string]LocalProcessTarget `json:"workers"`
	Dependencies map[string]LocalProcessTarget `json:"dependencies"`
}

// LocalProcessFaultController 使用 SIGSTOP/SIGCONT 制造可恢复故障，不启动或替换 Runtime。
type LocalProcessFaultController struct {
	manifest LocalProcessManifest
}

// LoadLocalProcessFaultController 严格读取不含凭据的本地进程清单。
func LoadLocalProcessFaultController(reader io.Reader) (*LocalProcessFaultController, error) {
	if reader == nil {
		return nil, fmt.Errorf("local scenario process manifest is required")
	}
	decoder := json.NewDecoder(io.LimitReader(reader, 1<<20))
	decoder.DisallowUnknownFields()
	var manifest LocalProcessManifest
	if err := decoder.Decode(&manifest); err != nil {
		return nil, fmt.Errorf("decode local scenario process manifest: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err != nil {
			return nil, fmt.Errorf("decode local scenario process manifest: %w", err)
		}
		return nil, fmt.Errorf("multiple local scenario process documents are not supported")
	}
	for group, targets := range map[string]map[string]LocalProcessTarget{
		"worker": manifest.Workers, "dependency": manifest.Dependencies,
	} {
		for name, target := range targets {
			if strings.TrimSpace(name) == "" || name != strings.TrimSpace(name) {
				return nil, fmt.Errorf("local scenario %s name is invalid", group)
			}
			if err := target.validate(); err != nil {
				return nil, fmt.Errorf("local scenario %s %q: %w", group, name, err)
			}
		}
	}
	return &LocalProcessFaultController{manifest: manifest}, nil
}

// SuspendWorker 暂停当前 lease owner 对应的显式 Worker，返回幂等恢复函数。
func (c *LocalProcessFaultController) SuspendWorker(_ context.Context, attempt AttemptTruth) (func() error, error) {
	if c == nil || strings.TrimSpace(attempt.LeaseOwner) == "" {
		return nil, fmt.Errorf("scenario Worker lease owner is required")
	}
	target, ok := c.manifest.Workers[attempt.LeaseOwner]
	if !ok {
		return nil, fmt.Errorf("scenario Worker lease owner is not present in the local process manifest")
	}
	return suspendLocalProcess(target)
}

// SuspendDependency 暂停清单中明确命名的本地依赖进程。
func (c *LocalProcessFaultController) SuspendDependency(_ context.Context, dependency string, _ AttemptTruth) (func() error, error) {
	if c == nil {
		return nil, fmt.Errorf("local process fault controller is not initialized")
	}
	target, ok := c.manifest.Dependencies[strings.TrimSpace(dependency)]
	if !ok {
		return nil, fmt.Errorf("scenario dependency is not present in the local process manifest")
	}
	return suspendLocalProcess(target)
}

func (t LocalProcessTarget) validate() error {
	if t.PID <= 1 || t.PID == os.Getpid() || t.PID == os.Getppid() {
		return fmt.Errorf("pid must identify a non-parent child or sibling process")
	}
	if strings.TrimSpace(t.CommandContains) == "" || t.CommandContains != strings.TrimSpace(t.CommandContains) || len(t.CommandContains) < 4 {
		return fmt.Errorf("command_contains must contain at least four unpadded bytes")
	}
	if strings.ContainsRune(t.CommandContains, '\x00') || looksLikeSecret(t.CommandContains) {
		return fmt.Errorf("command_contains is invalid")
	}
	return nil
}

func suspendLocalProcess(target LocalProcessTarget) (func() error, error) {
	if err := target.validate(); err != nil {
		return nil, err
	}
	if err := target.requireCommandMatch(); err != nil {
		return nil, err
	}
	process, err := os.FindProcess(target.PID)
	if err != nil {
		return nil, fmt.Errorf("find local scenario process: %w", err)
	}
	if err := process.Signal(syscall.SIGSTOP); err != nil {
		return nil, fmt.Errorf("suspend local scenario process: %w", err)
	}
	var once sync.Once
	var restoreErr error
	return func() error {
		once.Do(func() {
			if err := target.requireCommandMatch(); err != nil {
				restoreErr = err
				return
			}
			restoreErr = process.Signal(syscall.SIGCONT)
			if restoreErr != nil {
				restoreErr = fmt.Errorf("resume local scenario process: %w", restoreErr)
			}
		})
		return restoreErr
	}, nil
}

func (t LocalProcessTarget) requireCommandMatch() error {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(t.PID) + "/cmdline")
	if err != nil {
		return fmt.Errorf("inspect local scenario process: %w", err)
	}
	command := string(bytes.ReplaceAll(data, []byte{0}, []byte{' '}))
	clear(data)
	if !strings.Contains(command, t.CommandContains) {
		return fmt.Errorf("local scenario process command does not match its manifest")
	}
	return nil
}
