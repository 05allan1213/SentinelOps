package workflow

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSessionRevisionPayloadAppendsCurrentTurnOnce(t *testing.T) {
	base := json.RawMessage(`{"schema":"fo/session-state/v1","revision":4,"preference":{"output_style":"concise"},"summary":"prior","history":[{"role":"user","content":"old"}],"provenance":{"history":"durable"}}`)
	payload, err := BuildSessionRevisionPayload(base, 5, "current task", "current answer")
	if err != nil {
		t.Fatalf("build revision payload: %v", err)
	}
	var state struct {
		Schema     string            `json:"schema"`
		Revision   uint64            `json:"revision"`
		Preference map[string]any    `json:"preference"`
		Summary    string            `json:"summary"`
		History    []map[string]any  `json:"history"`
		Provenance map[string]string `json:"provenance"`
	}
	if err := json.Unmarshal(payload, &state); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if state.Schema != SessionStateSchemaV1 || state.Revision != 5 || state.Summary != "prior" || state.Preference["output_style"] != "concise" {
		t.Fatalf("payload lost durable fields: %+v", state)
	}
	if len(state.History) != 3 || state.History[1]["content"] != "current task" || state.History[2]["content"] != "current answer" {
		t.Fatalf("history = %+v", state.History)
	}
	encoded := string(payload)
	if strings.Count(encoded, "current task") != 1 || strings.Count(encoded, "current answer") != 1 {
		t.Fatalf("current turn was duplicated: %s", encoded)
	}
}

func TestSessionRevisionPayloadRejectsInvalidBase(t *testing.T) {
	base := json.RawMessage(`{"schema":"fo/session-state/v1","revision":0,"history":[]}`)
	if _, err := BuildSessionRevisionPayload(append(base, 'x'), 1, "current task", "answer"); err == nil {
		t.Fatal("invalid base revision was accepted")
	}
}
