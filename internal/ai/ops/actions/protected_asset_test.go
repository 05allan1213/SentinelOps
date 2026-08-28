package actions

import (
	"os"
	"strings"
	"testing"
)

func TestProtectedAssetCheckRemainsInTheSingleBlockIPImplementation(t *testing.T) {
	source, err := os.ReadFile("system.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	if strings.Count(text, `IsProtectedAsset(ctx, "whitelist_ip", ip)`) != 1 {
		t.Fatalf("protected asset check count changed in BlockIP implementation")
	}
	if strings.Count(text, "func (a *BlockIPAction) Execute") != 1 {
		t.Fatalf("BlockIP Execute implementation count changed")
	}
}
