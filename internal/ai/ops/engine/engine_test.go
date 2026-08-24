package engine

import "testing"

func TestCanAutoStartOpsForStatus(t *testing.T) {
	tests := []struct {
		name   string
		status string
		want   bool
	}{
		{name: "new 事件允许自动启动 AI 运维", status: "new", want: true},
		{name: "processing 事件不允许重复自动启动 AI 运维", status: "processing", want: false},
		{name: "resolved 事件不允许自动启动 AI 运维", status: "resolved", want: false},
		{name: "ignored 事件不允许自动启动 AI 运维", status: "ignored", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := canAutoStartOpsForStatus(tt.status)
			if got != tt.want {
				t.Fatalf("canAutoStartOpsForStatus(%q) = %v, want %v", tt.status, got, tt.want)
			}
		})
	}
}

func TestCanManualStartOpsForStatus(t *testing.T) {
	for _, status := range []string{"new", "processing", "resolved", "ignored"} {
		t.Run(status, func(t *testing.T) {
			if !canManualStartOpsForStatus(status) {
				t.Fatalf("canManualStartOpsForStatus(%q) = false, want true", status)
			}
		})
	}
}
