package generate

import (
	"strings"
	"testing"
)

func TestUnifiedDiffRemovedAndAdded(t *testing.T) {
	before := "line1\nline2\nline3\n"
	after := "line1\nchanged\nline3\n"
	d := unifiedDiff("f.txt", before, after)
	for _, want := range []string{
		"--- a/f.txt",
		"+++ b/f.txt",
		" line1",
		"-line2",
		"+changed",
		" line3",
	} {
		if !strings.Contains(d, want) {
			t.Fatalf("missing %q in:\n%s", want, d)
		}
	}
}

func TestUnifiedDiffIdenticalIsEmpty(t *testing.T) {
	if d := unifiedDiff("f", "same\n", "same\n"); d != "" {
		t.Fatalf("expected empty diff, got %q", d)
	}
}

func TestUnifiedDiffPureInsertion(t *testing.T) {
	d := unifiedDiff("f", "a\nc\n", "a\nb\nc\n")
	for _, want := range []string{"@@ -1,2 +1,3 @@", " a", "+b", " c"} {
		if !strings.Contains(d, want) {
			t.Fatalf("missing %q in:\n%s", want, d)
		}
	}
}

func TestUnifiedDiffMultipleHunks(t *testing.T) {
	var before strings.Builder
	for i := range 20 {
		before.WriteString("line ")
		before.WriteString(strings.Repeat("x", i+1))
		before.WriteString("\n")
	}
	after := strings.Replace(before.String(), "line xxxxxxxxx\n", "CHANGED ONE\n", 1)
	after = strings.Replace(after, "line xxxxxxxxxxxxxxxxxxxx\n", "CHANGED TWO\n", 1)

	d := unifiedDiff("f", before.String(), after)
	if got := strings.Count(d, "\n@@"); got != 2 {
		t.Fatalf("expected 2 hunks, got %d:\n%s", got, d)
	}
}

func TestUnifiedDiffEmptyBefore(t *testing.T) {
	d := unifiedDiff("f", "", "new\n")
	if !strings.Contains(d, "@@ -0,0 +1,1 @@") || !strings.Contains(d, "+new") {
		t.Fatalf("got:\n%s", d)
	}
}
