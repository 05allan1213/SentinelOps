package main

import "testing"

func TestHasUnknownFixtureIntentRequiresExplicitExternalEffect(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{name: "english fixture", content: "请执行一次 unknown 外部 effect", want: true},
		{name: "chinese fixture", content: "请执行一次未知外部 effect", want: true},
		{name: "nested status description", content: "执行封禁动作，unknown 状态必须 fail closed", want: false},
		{name: "ordinary block", content: "请执行一次需要审批的封禁动作", want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := hasUnknownFixtureIntent(test.content); got != test.want {
				t.Fatalf("hasUnknownFixtureIntent(%q) = %t, want %t", test.content, got, test.want)
			}
		})
	}
}
