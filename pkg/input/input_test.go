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
