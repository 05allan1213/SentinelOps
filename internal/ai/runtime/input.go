package runtime

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"SentinelOps/internal/dao/mysql"
)

// ImmutableRunInput 是 durable Run 创建时固化的最小任务输入。
// History、偏好和摘要属于 Context Snapshot，不允许进入 Query。
type ImmutableRunInput struct {
	Agent string `json:"agent"`
	Query string `json:"query"`
}

// BuildImmutableRunInput 严格解析 MySQL 中的 immutable_input_json。
// DisallowUnknownFields 使调用方无法把 History、Handle 或 Secret 伪装成任务输入。
func BuildImmutableRunInput(run mysql.WorkflowRun) (ImmutableRunInput, error) {
	if run.ImmutableInputJSON == nil || strings.TrimSpace(*run.ImmutableInputJSON) == "" {
		return ImmutableRunInput{}, fmt.Errorf("immutable durable input is required")
	}
	decoder := json.NewDecoder(strings.NewReader(*run.ImmutableInputJSON))
	decoder.DisallowUnknownFields()
	var input ImmutableRunInput
	if err := decoder.Decode(&input); err != nil {
		return ImmutableRunInput{}, fmt.Errorf("decode immutable durable input: %w", err)
	}
	if err := requireEOF(decoder); err != nil {
		return ImmutableRunInput{}, fmt.Errorf("decode immutable durable input: %w", err)
	}
	if strings.TrimSpace(input.Agent) == "" || strings.TrimSpace(input.Query) == "" {
		return ImmutableRunInput{}, fmt.Errorf("immutable durable input requires agent and query")
	}
	return input, nil
}

func requireEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("trailing JSON value")
		}
		return err
	}
	return nil
}
