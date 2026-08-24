package stringutil

import (
	"errors"
	"testing"
)

func TestTruncateRunes(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		maxRunes int
		want     string
	}{
		{"短于限制", "hello", 10, "hello"},
		{"等于限制", "hello", 5, "hello"},
		{"超出限制", "hello world", 5, "hello..."},
		{"中文字符", "你好世界", 2, "你好..."},
		{"空字符串", "", 5, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := TruncateRunes(tt.input, tt.maxRunes); got != tt.want {
				t.Errorf("TruncateRunes() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTruncateBytes(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		maxBytes int
		want     string
	}{
		{"短于限制", "hello", 10, "hello"},
		{"UTF-8边界", "你好", 3, "你"},
		{"空字符串", "", 5, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := TruncateBytes(tt.input, tt.maxBytes); got != tt.want {
				t.Errorf("TruncateBytes() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTruncateError(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		maxLen int
		want   string
	}{
		{"nil错误", nil, 10, ""},
		{"短错误", errors.New("err"), 10, "err"},
		{"长错误", errors.New("very long error message"), 10, "very long "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := TruncateError(tt.err, tt.maxLen); got != tt.want {
				t.Errorf("TruncateError() = %v, want %v", got, tt.want)
			}
		})
	}
}
