package mcp

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	appconfig "SentinelOps/internal/config"
)

// EndpointPolicy 是连接前和 HTTP redirect 每跳复用的 SSRF 策略。
type EndpointPolicy struct {
	LookupIP           func(context.Context, string) ([]net.IP, error)
	AllowedHosts       []string
	AllowedCIDRs       []string
	AllowedPorts       []int
	InsecureTLSForTest bool
}

// ValidateEndpoint 校验 scheme、host、解析结果、metadata/link-local 和端口 allowlist。
func ValidateEndpoint(ctx context.Context, rawURL string, policy EndpointPolicy) error {
	u, err := url.Parse(rawURL)
	if err != nil || !u.IsAbs() || u.Hostname() == "" {
		return fmt.Errorf("mcp endpoint URL must be absolute")
	}
	if u.User != nil {
		return fmt.Errorf("mcp endpoint URL must not contain userinfo")
	}
	if u.Scheme != "https" {
		// 显式测试开关只允许回环地址的明文 MCP；生产配置仍强制 HTTPS。
		if !policy.InsecureTLSForTest || !isLoopbackHost(u.Hostname()) {
			return fmt.Errorf("mcp endpoint must use HTTPS")
		}
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	if isMetadataHost(host) {
		return fmt.Errorf("mcp endpoint metadata address is forbidden")
	}
	port := 443
	if rawPort := u.Port(); rawPort != "" {
		if _, err := fmt.Sscanf(rawPort, "%d", &port); err != nil {
			return fmt.Errorf("mcp endpoint port is invalid")
		}
	}
	if (u.Port() != "" || len(policy.AllowedPorts) > 0) && !containsInt(policy.AllowedPorts, port) {
		return fmt.Errorf("mcp endpoint port %d is not allowlisted", port)
	}
	lookup := policy.LookupIP
	if lookup == nil {
		lookup = func(ctx context.Context, host string) ([]net.IP, error) {
			return net.DefaultResolver.LookupIP(ctx, "ip", host)
		}
	}
	ips, err := lookup(ctx, host)
	if err != nil || len(ips) == 0 {
		if err == nil {
			err = errors.New("no address")
		}
		return fmt.Errorf("mcp endpoint DNS lookup failed: %w", err)
	}
	for _, ip := range ips {
		if err := validateResolvedIP(ip, host, policy); err != nil {
			return err
		}
	}
	return nil
}

func validateResolvedIP(ip net.IP, host string, policy EndpointPolicy) error {
	if ip == nil {
		return errors.New("mcp endpoint resolved to empty address")
	}
	if isMetadataIP(ip) {
		return fmt.Errorf("mcp endpoint resolved to metadata address %s", ip)
	}
	if isPrivateOrLocal(ip) && !hostAllowed(host, ip, policy) {
		return fmt.Errorf("mcp endpoint resolved to private or localhost address %s without allowlist", ip)
	}
	if !isPrivateOrLocal(ip) && len(policy.AllowedCIDRs) > 0 && !cidrAllowed(ip, policy.AllowedCIDRs) {
		return fmt.Errorf("mcp endpoint address %s is outside CIDR allowlist", ip)
	}
	return nil
}

func isMetadataHost(host string) bool {
	return host == "metadata.google.internal" || host == "metadata.google" || host == "169.254.169.254" || host == "fd00:ec2::254"
}

func isLoopbackHost(host string) bool {
	switch strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), ".")) {
	case "localhost", "127.0.0.1", "::1", "0:0:0:0:0:0:0:1":
		return true
	default:
		return false
	}
}

func isMetadataIP(ip net.IP) bool {
	return ip.IsLinkLocalUnicast() || ip.Equal(net.ParseIP("169.254.169.254")) || ip.Equal(net.ParseIP("100.100.100.200")) || ip.Equal(net.ParseIP("fd00:ec2::254"))
}

func isPrivateOrLocal(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() || ip.IsLinkLocalUnicast()
}

func hostAllowed(host string, ip net.IP, policy EndpointPolicy) bool {
	for _, allowed := range policy.AllowedHosts {
		if strings.EqualFold(strings.TrimSpace(allowed), host) || strings.EqualFold(strings.TrimSpace(allowed), ip.String()) {
			return true
		}
	}
	return cidrAllowed(ip, policy.AllowedCIDRs)
}

func cidrAllowed(ip net.IP, cidrs []string) bool {
	for _, raw := range cidrs {
		_, network, err := net.ParseCIDR(strings.TrimSpace(raw))
		if err == nil && network.Contains(ip) {
			return true
		}
	}
	return false
}

func containsInt(values []int, want int) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

// NewSecureHTTPClient creates a client whose redirect callback revalidates every target.
func NewSecureHTTPClient(policy EndpointPolicy) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = secureDialContext(policy, transport.DialContext)
	if policy.InsecureTLSForTest {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} // #nosec G402 -- test-only opt-in is explicit.
	}
	client := &http.Client{Transport: transport}
	client.CheckRedirect = func(req *http.Request, _ []*http.Request) error {
		if err := ValidateEndpoint(req.Context(), req.URL.String(), policy); err != nil {
			return err
		}
		return nil
	}
	return client
}

func secureDialContext(policy EndpointPolicy, fallback func(context.Context, string, string) (net.Conn, error)) func(context.Context, string, string) (net.Conn, error) {
	lookup := policy.LookupIP
	if lookup == nil {
		lookup = func(ctx context.Context, host string) ([]net.IP, error) {
			return net.DefaultResolver.LookupIP(ctx, "ip", host)
		}
	}
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		ips, err := lookup(ctx, strings.Trim(host, "[]"))
		if err != nil || len(ips) == 0 {
			return nil, fmt.Errorf("mcp endpoint DNS lookup failed: %w", err)
		}
		for _, ip := range ips {
			if err := validateResolvedIP(ip, strings.Trim(host, "[]"), policy); err != nil {
				continue
			}
			return fallback(ctx, network, net.JoinHostPort(ip.String(), port))
		}
		return nil, fmt.Errorf("mcp endpoint has no allowed resolved address for %s", host)
	}
}

// ValidateStdio 校验无 shell 的固定命令、cwd 和环境 allowlist。
func ValidateStdio(cfg TransportConfig) error {
	if cfg.Type != TransportStdio {
		return fmt.Errorf("stdio validation requires stdio transport")
	}
	if !filepath.IsAbs(cfg.Command) || strings.ContainsAny(cfg.Command, "\x00\r\n") {
		return errors.New("stdio command must be an absolute executable path")
	}
	if cfg.CWD == "" || !filepath.IsAbs(cfg.CWD) {
		return errors.New("stdio cwd must be an absolute path")
	}
	if info, err := os.Stat(cfg.Command); err != nil || info.IsDir() || info.Mode()&0o111 == 0 {
		return fmt.Errorf("stdio command is not executable: %s", cfg.Command)
	}
	if info, err := os.Stat(cfg.CWD); err != nil || !info.IsDir() {
		return fmt.Errorf("stdio cwd is not a directory: %s", cfg.CWD)
	}
	allowed := make(map[string]struct{}, len(cfg.EnvAllowlist))
	for _, key := range cfg.EnvAllowlist {
		if strings.TrimSpace(key) == "" || strings.TrimSpace(key) != key || strings.ContainsAny(key, "=\x00\r\n") {
			return errors.New("stdio environment allowlist contains invalid name")
		}
		if _, exists := allowed[key]; exists {
			return fmt.Errorf("stdio environment allowlist contains duplicate name %q", key)
		}
		allowed[key] = struct{}{}
	}
	for _, arg := range cfg.Args {
		if strings.ContainsRune(arg, '\x00') {
			return errors.New("stdio argument contains NUL")
		}
	}
	for key := range cfg.Env {
		if _, ok := allowed[key]; !ok {
			return fmt.Errorf("stdio environment %q is not allowlisted", key)
		}
	}
	return nil
}

func buildStdioCommand(cfg TransportConfig) (*exec.Cmd, error) {
	if err := ValidateStdio(cfg); err != nil {
		return nil, err
	}
	env := make([]string, 0, len(cfg.Env))
	for _, key := range append([]string(nil), cfg.EnvAllowlist...) {
		if value, ok := cfg.Env[key]; ok {
			env = append(env, key+"="+value)
		}
	}
	sort.Strings(env)
	return &exec.Cmd{Path: cfg.Command, Args: append([]string{cfg.Command}, cfg.Args...), Dir: cfg.CWD, Env: env}, nil
}

// secretRoundTripper resolves the header only while an HTTP request is in flight.
type secretRoundTripper struct {
	base     http.RoundTripper
	name     string
	ref      appconfig.SecretRef
	resolver func(context.Context, appconfig.SecretRef, func([]byte) error) error
}

func (t secretRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	var response *http.Response
	err := t.resolver(req.Context(), t.ref, func(value []byte) error {
		cloned := req.Clone(req.Context())
		cloned.Header.Set(t.name, string(value))
		var err error
		response, err = t.base.RoundTrip(cloned)
		return err
	})
	return response, err
}

// withSecretHeader decorates a client without retaining the resolved bytes.
func withSecretHeader(client *http.Client, name string, ref appconfig.SecretRef) *http.Client {
	if client == nil {
		client = &http.Client{}
	}
	copied := *client
	base := copied.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	copied.Transport = secretRoundTripper{base: base, name: name, ref: ref, resolver: appconfig.UseSecret}
	return &copied
}
