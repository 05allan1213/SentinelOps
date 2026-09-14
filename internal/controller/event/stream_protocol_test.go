package event

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestStreamEventPayloadMatchesFrontendProtocol 固定前后端流式协议：
// 前端只解析 data 行里的 JSON，并用 payload.type 分派 content/done/error。
// 任何一帧退回纯文本都会让前端静默丢弃该帧。
func TestStreamEventPayloadMatchesFrontendProtocol(t *testing.T) {
	cases := []struct {
		name    string
		kind    string
		content string
	}{
		{name: "error frame keeps message", kind: "error", content: "model unavailable"},
		{name: "done frame omits empty content", kind: "done", content: ""},
		{name: "content frame keeps chunk", kind: "content", content: "### 应急响应步骤"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			encoded := streamEventPayload(tc.kind, tc.content)
			if !strings.HasPrefix(encoded, "{") {
				t.Fatalf("payload %q is not a JSON object", encoded)
			}
			payload := map[string]string{}
			if err := json.Unmarshal([]byte(encoded), &payload); err != nil {
				t.Fatalf("payload %q is not parseable JSON: %v", encoded, err)
			}
			if payload["type"] != tc.kind {
				t.Fatalf("type = %q, want %q", payload["type"], tc.kind)
			}
			if tc.content == "" {
				if _, exists := payload["content"]; exists {
					t.Fatalf("empty content should be omitted, got %q", encoded)
				}
				return
			}
			if payload["content"] != tc.content {
				t.Fatalf("content = %q, want %q", payload["content"], tc.content)
			}
		})
	}
}

// TestStreamEventPayloadKeepsMultilineContentSingleFrame 保证错误文本换行不会破坏单帧 JSON。
func TestStreamEventPayloadKeepsMultilineContentSingleFrame(t *testing.T) {
	encoded := streamEventPayload("error", "line one\nline two")
	payload := map[string]string{}
	if err := json.Unmarshal([]byte(encoded), &payload); err != nil {
		t.Fatalf("payload %q is not parseable JSON: %v", encoded, err)
	}
	if payload["content"] != "line one\nline two" {
		t.Fatalf("content = %q", payload["content"])
	}
}
