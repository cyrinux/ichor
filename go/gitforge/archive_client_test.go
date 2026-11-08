package gitforge

import (
	"strings"
	"testing"
)

// otherForge is a Forge none of the three: what a fourth forge without its case would be.
type otherForge struct{ Forge }

// An unknown forge is an error from the archive download, not a panic of the process.
func TestGitForgeClientOfUnknownForge(t *testing.T) {
	for _, kind := range []string{KindGitHub, KindGitLab, KindGitea} {
		if _, err := gitForgeClientOf(New(Host{Kind: kind, BaseURL: "https://forge.example.invalid"})); err != nil {
			t.Errorf("%v: %v", kind, err)
		}
	}

	if _, err := gitForgeClientOf(otherForge{}); err == nil || !strings.Contains(err.Error(), "unknown forge gitforge.otherForge") {
		t.Fatalf("err %v", err)
	}
}
