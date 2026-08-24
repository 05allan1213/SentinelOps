package chat

import "testing"

func TestNormalizeDeepThinkingTimeoutSecRaisesTooSmallValues(t *testing.T) {
	if got := normalizeDeepThinkingTimeoutSec(300); got != 600 {
		t.Fatalf("深度思考超时下限错误，got=%d want=%d", got, 600)
	}
}

func TestNormalizeDeepThinkingTimeoutSecKeepsLargerValues(t *testing.T) {
	if got := normalizeDeepThinkingTimeoutSec(900); got != 900 {
		t.Fatalf("较大的深度思考超时不应被修改，got=%d want=%d", got, 900)
	}
}
