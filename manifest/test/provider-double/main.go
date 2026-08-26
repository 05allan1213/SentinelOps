package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"
	"sync/atomic"
)

type chatRequest struct {
	Model    string           `json:"model"`
	Messages []map[string]any `json:"messages"`
	Tools    []map[string]any `json:"tools"`
	Stream   bool             `json:"stream"`
}

type response struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	Model   string `json:"model"`
	Choices []struct {
		Index        int    `json:"index"`
		FinishReason string `json:"finish_reason"`
		Message      struct {
			Role      string `json:"role"`
			Content   string `json:"content"`
			ToolCalls []struct {
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls,omitempty"`
		} `json:"message"`
	} `json:"choices"`
	Usage map[string]any `json:"usage,omitempty"`
}

var sequence atomic.Uint64

func main() {
	http.HandleFunc("/v1/chat/completions", completions)
	http.HandleFunc("/v1/embeddings", embeddings)
	http.HandleFunc("/v1/reranks", reranks)
	http.HandleFunc("/unknown", func(w http.ResponseWriter, _ *http.Request) {
		// 故障注入：连接在收到请求后关闭，驱动真实 external Effect unknown 窗口。
		hijacker, ok := w.(http.Hijacker)
		if !ok {
			return
		}
		connection, _, err := hijacker.Hijack()
		if err == nil {
			_ = connection.Close()
		}
	})
	http.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func embeddings(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Input any    `json:"input"`
		Model string `json:"model"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	vector := make([]float64, 2048)
	if len(vector) > 0 {
		vector[0] = 1
	}
	item := map[string]any{"object": "list", "data": []any{map[string]any{"object": "embedding", "index": 0, "embedding": vector}}, "model": request.Model, "usage": map[string]any{"prompt_tokens": 8, "total_tokens": 8}}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(item)
}

func reranks(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Documents []string `json:"documents"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	results := make([]map[string]any, 0, len(request.Documents))
	for index := range request.Documents {
		results = append(results, map[string]any{"index": index, "relevance_score": float64(len(request.Documents)-index) / float64(max(1, len(request.Documents)))})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"results": results, "usage": map[string]any{"total_tokens": 8}})
}

func max(left, right int) int {
	if left > right {
		return left
	}
	return right
}

func completions(w http.ResponseWriter, r *http.Request) {
	var request chatRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	toolName, arguments, content := nextResponse(request)
	if request.Stream {
		writeStream(w, request.Model, toolName, arguments, content)
		return
	}
	item := response{ID: fmt.Sprintf("e2e-%d", sequence.Add(1)), Object: "chat.completion", Model: request.Model}
	item.Usage = map[string]any{"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15}
	item.Choices = append(item.Choices, struct {
		Index        int    `json:"index"`
		FinishReason string `json:"finish_reason"`
		Message      struct {
			Role      string `json:"role"`
			Content   string `json:"content"`
			ToolCalls []struct {
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls,omitempty"`
		} `json:"message"`
	}{Index: 0, FinishReason: "stop"})
	item.Choices[0].Message.Role = "assistant"
	item.Choices[0].Message.Content = content
	if toolName != "" {
		item.Choices[0].FinishReason = "tool_calls"
		item.Choices[0].Message.ToolCalls = append(item.Choices[0].Message.ToolCalls, struct {
			ID       string `json:"id"`
			Type     string `json:"type"`
			Function struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			} `json:"function"`
		}{ID: fmt.Sprintf("call-%d", sequence.Load()), Type: "function", Function: struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		}{Name: toolName, Arguments: arguments}})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(item)
}

func writeStream(w http.ResponseWriter, model, toolName, arguments, content string) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	flusher, _ := w.(http.Flusher)
	writeChunk := func(chunk map[string]any) {
		payload, _ := json.Marshal(chunk)
		_, _ = fmt.Fprintf(w, "data: %s\n\n", payload)
		if flusher != nil {
			flusher.Flush()
		}
	}
	chunk := func(delta map[string]any, finish any) map[string]any {
		choice := map[string]any{"index": 0, "delta": delta}
		if finish != nil {
			choice["finish_reason"] = finish
		}
		return map[string]any{"id": fmt.Sprintf("e2e-%d", sequence.Add(1)), "object": "chat.completion.chunk", "model": model, "choices": []any{choice}}
	}
	writeChunk(chunk(map[string]any{"role": "assistant"}, nil))
	if toolName != "" {
		writeChunk(chunk(map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": fmt.Sprintf("call-%d", sequence.Load()), "type": "function", "function": map[string]any{"name": toolName, "arguments": arguments}}}}, nil))
		writeChunk(chunk(map[string]any{}, "tool_calls"))
	} else {
		writeChunk(chunk(map[string]any{"content": content}, nil))
		writeChunk(chunk(map[string]any{}, "stop"))
	}
	writeChunk(map[string]any{"id": fmt.Sprintf("e2e-%d", sequence.Add(1)), "object": "chat.completion.chunk", "model": model, "choices": []any{}, "usage": map[string]any{"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15}})
	_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	if flusher != nil {
		flusher.Flush()
	}
}

func nextResponse(request chatRequest) (string, string, string) {
	unknown := false
	blockIPResultSeen := false
	opsAgentResultSeen := false
	webhookResultSeen := false
	markerCounts := map[string]int{}
	for _, message := range request.Messages {
		content := strings.ToLower(contentText(message["content"]))
		role := fmt.Sprint(message["role"])
		for _, marker := range []string{"unknown", "未知", "外部", "封禁", "webhook"} {
			if strings.Contains(content, marker) {
				markerCounts[role+":"+marker]++
			}
		}
		if role == "user" && (strings.Contains(content, "unknown") || strings.Contains(content, "未知")) {
			unknown = true
		}
		if message["role"] == "tool" && (message["name"] == "block_ip" || strings.Contains(content, "192.0.2.38")) {
			blockIPResultSeen = true
		}
		if message["role"] == "tool" && (message["name"] == "ops_agent" || strings.Contains(content, "ops_agent")) {
			opsAgentResultSeen = true
		}
		if message["role"] == "tool" && (message["name"] == "webhook_out" || strings.Contains(content, "webhook_out")) {
			webhookResultSeen = true
		}
	}
	toolNames := make(map[string]bool)
	for _, tool := range request.Tools {
		if function, ok := tool["function"].(map[string]any); ok {
			if name, ok := function["name"].(string); ok {
				toolNames[name] = true
			}
		}
	}
	log.Printf("e2e response tools=%v markers=%v unknown=%t block_result_seen=%t ops_result_seen=%t webhook_result_seen=%t prior_block=%t prior_ops=%t prior_webhook=%t", sortedToolNames(toolNames), markerCounts, unknown, blockIPResultSeen, opsAgentResultSeen, webhookResultSeen, priorToolCall(request, "block_ip"), priorToolCall(request, "ops_agent"), priorToolCall(request, "webhook_out"))
	// Replanner exposes both tools. A completed child result is terminal for
	// this deterministic fixture, so prefer the response tool before planning.
	if toolNames["respond"] {
		return "respond", `{"response":"approval effect completed"}`, ""
	}
	if toolNames["plan"] {
		plan := "调用 ops_agent 规划并执行一次需要审批的封禁动作"
		if unknown {
			plan = "调用 ops_agent 执行一次 unknown 外部 Effect"
		}
		return "plan", fmt.Sprintf(`{"steps":[%q]}`, plan), ""
	}
	if toolNames["webhook_out"] && unknown && !webhookResultSeen && !priorToolCall(request, "webhook_out") {
		return "webhook_out", `{"url":"http://provider-double:8080/unknown","payload":"{}","method":"POST"}`, ""
	}
	if toolNames["block_ip"] && !blockIPResultSeen && !priorToolCall(request, "block_ip") {
		return "block_ip", `{"ip":"192.0.2.38","reason":"P38 deterministic approval fixture"}`, ""
	}
	if blockIPResultSeen {
		if toolNames["respond"] {
			return "respond", `{"response":"approval effect completed"}`, ""
		}
		return "", "", "approval effect completed"
	}
	if toolNames["ops_agent"] && !opsAgentResultSeen && !priorToolCall(request, "ops_agent") {
		if unknown {
			return "ops_agent", `{"request":"执行一次 unknown 外部 Effect"}`, ""
		}
		return "ops_agent", `{"request":"对事件 e2e-event 执行一次需要审批的封禁动作"}`, ""
	}
	if opsAgentResultSeen {
		return "", "", "approval effect completed"
	}
	if toolNames["replan"] {
		return "replan", `{"steps":[]}`, ""
	}
	for _, message := range request.Messages {
		if strings.Contains(strings.ToLower(contentText(message["content"])), "approval") {
			return "", "", "approval effect completed"
		}
	}
	return "", "", "e2e provider double response"
}

func sortedToolNames(toolNames map[string]bool) []string {
	result := make([]string, 0, len(toolNames))
	for name := range toolNames {
		result = append(result, name)
	}
	sort.Strings(result)
	return result
}

// contentText 兼容 OpenAI-compatible 消息的 string 与 content-part 两种编码。
// Provider double 只读取控制标记，不把模型输入原文写入日志或证据。
func contentText(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case []any:
		var parts []string
		for _, part := range typed {
			parts = append(parts, contentText(part))
		}
		return strings.Join(parts, " ")
	case map[string]any:
		var parts []string
		for _, key := range []string{"text", "content", "value"} {
			if part, ok := typed[key]; ok {
				parts = append(parts, contentText(part))
			}
		}
		return strings.Join(parts, " ")
	default:
		return fmt.Sprint(value)
	}
}

// priorToolCall 判断本轮对话是否已经生成过指定 Tool call；Tool result
// 消息在 OpenAI 兼容请求中不携带 Eino ToolName，assistant tool_calls 才是
// provider double 可稳定观察且不会混淆嵌套 Agent 的事实。
func priorToolCall(request chatRequest, name string) bool {
	for _, message := range request.Messages {
		calls, ok := message["tool_calls"].([]any)
		if !ok {
			continue
		}
		for _, raw := range calls {
			call, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			function, ok := call["function"].(map[string]any)
			if ok && function["name"] == name {
				return true
			}
		}
	}
	return false
}
