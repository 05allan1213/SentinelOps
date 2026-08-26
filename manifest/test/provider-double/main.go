package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
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
	for _, message := range request.Messages {
		if strings.Contains(strings.ToLower(fmt.Sprint(message["content"])), "unknown") {
			unknown = true
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
	if toolNames["plan"] {
		return "plan", `{"steps":["调用 ops_agent 规划并执行一次需要审批的封禁动作"]}`, ""
	}
	if toolNames["webhook_out"] && unknown {
		return "webhook_out", `{"url":"http://provider-double:8080/unknown","payload":"{}","method":"POST"}`, ""
	}
	if toolNames["block_ip"] {
		return "block_ip", `{"ip":"192.0.2.38","reason":"P38 deterministic approval fixture"}`, ""
	}
	if toolNames["ops_agent"] {
		if unknown {
			return "ops_agent", `{"query":"执行一次 unknown 外部 Effect"}`, ""
		}
		return "ops_agent", `{"query":"对事件 e2e-event 执行一次需要审批的封禁动作"}`, ""
	}
	if toolNames["respond"] {
		return "respond", `{"response":"approval effect completed"}`, ""
	}
	if toolNames["replan"] {
		return "replan", `{"steps":[]}`, ""
	}
	for _, message := range request.Messages {
		if strings.Contains(fmt.Sprint(message["content"]), "approval") {
			return "", "", "approval effect completed"
		}
	}
	return "", "", "e2e provider double response"
}
