// Package actions nginx 黑名单管理辅助函数
package actions

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/gogf/gf/v2/frame/g"
)

// NginxBlocklistManager nginx 黑名单管理器
type NginxBlocklistManager struct {
	blocklistPath string
	containerName string
	backend       string
}

// NewNginxBlocklistManager 从配置创建管理器
func NewNginxBlocklistManager(ctx context.Context) *NginxBlocklistManager {
	return &NginxBlocklistManager{
		// 与 nginx 容器共享同一挂载目录，后端写入 deny 规则文件后由 nginx include 生效
		blocklistPath: g.Cfg().MustGet(ctx, "soar.ai_ops.nginx_blocklist_path", "/app/nginx_blocklist/blocked_ips.conf").String(),
		containerName: g.Cfg().MustGet(ctx, "soar.ai_ops.nginx_container", "nginx").String(),
		backend:       g.Cfg().MustGet(ctx, "soar.ai_ops.block_ip_backend", "none").String(),
	}
}

// IsEnabled 检查 nginx 后端是否启用
func (m *NginxBlocklistManager) IsEnabled() bool {
	return m.backend == "nginx"
}

// AddIP 添加 IP 到黑名单并 reload
func (m *NginxBlocklistManager) AddIP(ctx context.Context, ip string) error {
	if !m.IsEnabled() {
		return nil // 后端未启用，静默跳过
	}

	// 检查是否已存在，避免重复追加相同的 deny 规则
	existing, _ := os.ReadFile(m.blocklistPath)
	line := fmt.Sprintf("deny %s;", ip)
	if strings.Contains(string(existing), line) {
		return nil // 已存在
	}

	// 追加写入
	f, err := os.OpenFile(m.blocklistPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("打开黑名单文件失败: %w", err)
	}
	_, werr := fmt.Fprintln(f, line)
	f.Close()
	if werr != nil {
		return fmt.Errorf("写入黑名单失败: %w", werr)
	}

	// 写入黑名单后向 nginx 发送 SIGHUP，让新追加的 deny 规则立即生效
	return m.reloadNginx(ctx)
}

// RemoveIP 从黑名单移除 IP 并 reload
func (m *NginxBlocklistManager) RemoveIP(ctx context.Context, ip string) error {
	if !m.IsEnabled() {
		return nil
	}

	content, err := os.ReadFile(m.blocklistPath)
	if err != nil {
		return fmt.Errorf("读取黑名单文件失败: %w", err)
	}

	line := fmt.Sprintf("deny %s;", ip)
	newContent := strings.ReplaceAll(string(content), line+"\n", "")
	newContent = strings.ReplaceAll(newContent, line, "") // 处理无换行符情况

	if err := os.WriteFile(m.blocklistPath, []byte(newContent), 0644); err != nil {
		return fmt.Errorf("写入黑名单文件失败: %w", err)
	}

	return m.reloadNginx(ctx)
}

// ListBlockedIPs 列出所有已封禁 IP
func (m *NginxBlocklistManager) ListBlockedIPs() ([]string, error) {
	content, err := os.ReadFile(m.blocklistPath)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, err
	}

	var ips []string
	lines := strings.Split(string(content), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "deny ") && strings.HasSuffix(line, ";") {
			ip := strings.TrimSuffix(strings.TrimPrefix(line, "deny "), ";")
			ips = append(ips, strings.TrimSpace(ip))
		}
	}
	return ips, nil
}

// reloadNginx 通过 Docker socket 向 nginx 容器发 SIGHUP，使 include 的黑名单文件重新加载
func (m *NginxBlocklistManager) reloadNginx(ctx context.Context) error {
	out, err := exec.CommandContext(ctx,
		"docker", "kill", "--signal=HUP", m.containerName,
	).CombinedOutput()
	if err != nil {
		return fmt.Errorf("nginx reload 失败 (container=%s): %w, output: %s", m.containerName, err, out)
	}
	return nil
}
