package input_test

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/chadsmith12/berth/pkg/input"
)

func TestPromptWritesLabelToOut(t *testing.T) {
	var out bytes.Buffer
	term := input.New(strings.NewReader("  value  \n"), &out, true)
	got, err := term.Prompt("url?")
	if err != nil {
		t.Fatalf("err %v", err)
	}
	if got != "value" {
		t.Fatalf("got %q", got)
	}
	if out.String() != "url? " {
		t.Fatalf("out %q", out.String())
	}
}

func TestPromptNotInteractive(t *testing.T) {
	var out bytes.Buffer
	term := input.New(strings.NewReader("value\n"), &out, false)
	_, err := term.Prompt("url?")
	if !errors.Is(err, input.ErrNotInteractive) {
		t.Fatalf("expected ErrNotInteractive, got %v", err)
	}
	if out.Len() != 0 {
		t.Fatalf("prompt written while not interactive: %q", out.String())
	}
}

func TestPromptEOFWithoutNewline(t *testing.T) {
	var out bytes.Buffer
	term := input.New(strings.NewReader("value"), &out, true)
	got, err := term.Prompt("url?")
	if err != nil {
		t.Fatalf("err %v", err)
	}
	if got != "value" {
		t.Fatalf("got %q", got)
	}
}

type errReader struct{}

func (errReader) Read(p []byte) (int, error) {
	return 0, errors.New("read failed")
}

func TestPromptReadError(t *testing.T) {
	var out bytes.Buffer
	term := input.New(errReader{}, &out, true)
	if _, err := term.Prompt("url?"); err == nil {
		t.Fatal("expected read error")
	}
}

func TestPromptLinesSequentially(t *testing.T) {
	var out bytes.Buffer
	term := input.New(strings.NewReader("first\nsecond\n"), &out, true)
	first, err := term.Prompt("one?")
	if err != nil {
		t.Fatalf("err %v", err)
	}
	second, err := term.Prompt("two?")
	if err != nil {
		t.Fatalf("err %v", err)
	}
	if first != "first" || second != "second" {
		t.Fatalf("got %q %q", first, second)
	}
}

func TestPromptIntParses(t *testing.T) {
	var out bytes.Buffer
	term := input.New(strings.NewReader("3\n"), &out, true)
	n, err := term.PromptInt("count?", nil)
	if err != nil {
		t.Fatalf("err %v", err)
	}
	if n != 3 {
		t.Fatalf("got %d", n)
	}
}

func TestPromptIntEmptyErrors(t *testing.T) {
	var out bytes.Buffer
	term := input.New(strings.NewReader("\n"), &out, true)
	if _, err := term.PromptInt("count?", nil); err == nil {
		t.Fatal("expected error on empty input")
	}
}

func TestPromptIntInvalidErrors(t *testing.T) {
	var out bytes.Buffer
	term := input.New(strings.NewReader("many\n"), &out, true)
	if _, err := term.PromptInt("count?", nil); err == nil {
		t.Fatal("expected error on invalid input")
	}
}

func TestPromptIntValidate(t *testing.T) {
	var out bytes.Buffer
	term := input.New(strings.NewReader("-1\n"), &out, true)
	_, err := term.PromptInt("count?", func(n int) error {
		if n < 0 {
			return errors.New("must be 0 or greater")
		}
		return nil
	})
	if err == nil || err.Error() != "must be 0 or greater" {
		t.Fatalf("expected validator error, got %v", err)
	}
}

func TestPromptIntNotInteractive(t *testing.T) {
	var out bytes.Buffer
	term := input.New(strings.NewReader("3\n"), &out, false)
	_, err := term.PromptInt("count?", nil)
	if !errors.Is(err, input.ErrNotInteractive) {
		t.Fatalf("expected ErrNotInteractive, got %v", err)
	}
}

func TestPromptIntIOReaderCompatibility(t *testing.T) {
	var out bytes.Buffer
	term := input.New(io.Reader(strings.NewReader("7\n")), &out, true)
	n, err := term.PromptInt("count?", nil)
	if err != nil || n != 7 {
		t.Fatalf("n %d err %v", n, err)
	}
}

func TestPromptYesNo(t *testing.T) {
	cases := []struct {
		in   string
		def  bool
		want bool
	}{
		{"y\n", false, true},
		{"yes\n", false, true},
		{"n\n", true, false},
		{"no\n", true, false},
		{"\n", true, true},
		{"\n", false, false},
		{"YES\n", false, true},
	}
	for _, c := range cases {
		var out bytes.Buffer
		term := input.New(strings.NewReader(c.in), &out, true)
		got, err := term.PromptYesNo("run?", c.def)
		if err != nil {
			t.Fatalf("in %q: err %v", c.in, err)
		}
		if got != c.want {
			t.Fatalf("in %q def %v: got %v", c.in, c.def, got)
		}
	}
}

func TestPromptYesNoInvalid(t *testing.T) {
	var out bytes.Buffer
	term := input.New(strings.NewReader("maybe\n"), &out, true)
	if _, err := term.PromptYesNo("run?", true); err == nil {
		t.Fatal("expected error on invalid answer")
	}
}

func TestPromptYesNoNotInteractiveTakesDefault(t *testing.T) {
	var out bytes.Buffer
	term := input.New(strings.NewReader("y\n"), &out, false)
	got, err := term.PromptYesNo("run?", true)
	if err != nil || !got {
		t.Fatalf("got %v err %v", got, err)
	}
	if out.Len() != 0 {
		t.Fatalf("prompt written while not interactive: %q", out.String())
	}
}

func TestPromptDefaultTakesDefaultOnEmpty(t *testing.T) {
	var out bytes.Buffer
	term := input.New(strings.NewReader("\n"), &out, true)
	got, err := term.PromptDefault("profile name?", "root-team")
	if err != nil || got != "root-team" {
		t.Fatalf("got %q err %v", got, err)
	}
}

func TestPromptDefaultInt(t *testing.T) {
	var out bytes.Buffer
	term := input.New(strings.NewReader("\n"), &out, true)
	n, err := term.PromptDefaultInt("port?", 8080, nil)
	if err != nil || n != 8080 {
		t.Fatalf("got %d err %v", n, err)
	}

	term = input.New(strings.NewReader("3000\n"), &out, true)
	n, err = term.PromptDefaultInt("port?", 8080, nil)
	if err != nil || n != 3000 {
		t.Fatalf("got %d err %v", n, err)
	}
}

func TestPromptDefaultIntRetriesInvalid(t *testing.T) {
	var out bytes.Buffer
	term := input.New(strings.NewReader("abc\n70000\n9999\n"), &out, true)
	n, err := term.PromptDefaultInt("port?", 8080, func(n int) error {
		if n > 65535 {
			return errors.New("must be between 1 and 65535")
		}
		return nil
	})
	if err != nil || n != 9999 {
		t.Fatalf("got %d err %v", n, err)
	}
}

func TestPromptDefaultIntNotInteractiveTakesDefault(t *testing.T) {
	var out bytes.Buffer
	term := input.New(strings.NewReader("3\n"), &out, false)
	n, err := term.PromptDefaultInt("port?", 8080, nil)
	if err != nil || n != 8080 {
		t.Fatalf("got %d err %v", n, err)
	}
	if out.Len() != 0 {
		t.Fatalf("prompt written while not interactive: %q", out.String())
	}
}

func TestSelect(t *testing.T) {
	var out bytes.Buffer
	term := input.New(strings.NewReader("2\n"), &out, true)
	idx, err := term.Select("pick?", []string{"a", "b", "c"})
	if err != nil || idx != 1 {
		t.Fatalf("idx %d err %v", idx, err)
	}
	if !strings.Contains(out.String(), "  2) b") {
		t.Fatalf("options not listed: %q", out.String())
	}
}

func TestSelectRetriesOutOfRange(t *testing.T) {
	var out bytes.Buffer
	term := input.New(strings.NewReader("9\n0\n3\n"), &out, true)
	idx, err := term.Select("pick?", []string{"a", "b", "c"})
	if err != nil || idx != 2 {
		t.Fatalf("idx %d err %v", idx, err)
	}
}

func TestSelectNotInteractive(t *testing.T) {
	var out bytes.Buffer
	term := input.New(strings.NewReader("1\n"), &out, false)
	if _, err := term.Select("pick?", []string{"a"}); !errors.Is(err, input.ErrNotInteractive) {
		t.Fatalf("got %v", err)
	}
}
