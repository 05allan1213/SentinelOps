package ops_pipeline

import "testing"

func TestNextAutoOpsStatus(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "new 事件自动运维完成后转为 resolved", input: "new", want: "resolved"},
		{name: "processing 事件自动运维完成后转为 resolved", input: "processing", want: "resolved"},
		{name: "resolved 事件保持 resolved", input: "resolved", want: "resolved"},
		{name: "ignored 事件保持 ignored", input: "ignored", want: "ignored"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := nextAutoOpsStatus(tt.input)
			if got != tt.want {
				t.Fatalf("nextAutoOpsStatus(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
