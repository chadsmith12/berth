package output_test

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"
	"syscall"
	"testing"

	"github.com/chadsmith12/berth/pkg/cli"
	"github.com/chadsmith12/berth/pkg/output"
)

func newCtx(stdout, stderr io.Writer, jsonMode bool) *cli.CmdContext {
	return &cli.CmdContext{
		Globals: cli.Globals{JSON: jsonMode},
		Command: &cli.Command{Name: "init"},
		Stdout:  stdout,
		Stderr:  stderr,
	}
}

type fakeView struct {
	called bool
}

func (f *fakeView) WriteText(w io.Writer) {
	f.called = true
	fmt.Fprint(w, "text-data")
}

func TestEmitJSONSuccess(t *testing.T) {
	var out, errB bytes.Buffer
	code := output.Emit(newCtx(&out, &errB, true), map[string]int{"workers": 2}, nil)
	if code != 0 {
		t.Fatalf("code %d", code)
	}
	got := out.String()
	for _, want := range []string{`"success":true`, `"command":"init"`, `"workers":2`} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %s in %q", want, got)
		}
	}
	if strings.Contains(got, `"code"`) {
		t.Fatalf("success envelope should omit code: %q", got)
	}
	if errB.Len() != 0 {
		t.Fatalf("stderr %q", errB.String())
	}
}

func TestEmitJSONErrorWithCode(t *testing.T) {
	var out, errB bytes.Buffer
	data := &fakeView{}
	code := output.Emit(newCtx(&out, &errB, true), data, output.Preflight(errors.New("checks failed")))
	if code != 4 {
		t.Fatalf("code %d", code)
	}
	got := out.String()
	for _, want := range []string{`"success":false`, `"error":"checks failed"`, `"code":4`} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %s in %q", want, got)
		}
	}
	if data.called {
		t.Fatal("WriteText must not run in JSON mode")
	}
}

func TestEmitJSONPlainErrorDefaultsToFailure(t *testing.T) {
	var out, errB bytes.Buffer
	code := output.Emit(newCtx(&out, &errB, true), nil, errors.New("boom"))
	if code != 1 {
		t.Fatalf("code %d", code)
	}
	if !strings.Contains(out.String(), `"code":1`) {
		t.Fatalf("got %q", out.String())
	}
}

func TestEmitTextWritesDataThenError(t *testing.T) {
	var out, errB bytes.Buffer
	data := &fakeView{}
	code := output.Emit(newCtx(&out, &errB, false), data, output.Usage(errors.New("missing --workers")))
	if code != 2 {
		t.Fatalf("code %d", code)
	}
	if !data.called {
		t.Fatal("WriteText not called")
	}
	if out.String() != "text-data" {
		t.Fatalf("stdout %q", out.String())
	}
	if errB.String() != "missing --workers\n" {
		t.Fatalf("stderr %q", errB.String())
	}
}

func TestEmitTextNoData(t *testing.T) {
	var out, errB bytes.Buffer
	code := output.Emit(newCtx(&out, &errB, false), nil, nil)
	if code != 0 {
		t.Fatalf("code %d", code)
	}
}

func TestWriteJSONEPIPEIsSilent(t *testing.T) {
	var errB bytes.Buffer
	code := output.Emit(newCtx(epipeWriter{}, &errB, true), nil, nil)
	if code != 0 {
		t.Fatalf("code %d", code)
	}
	if errB.Len() != 0 {
		t.Fatalf("stderr %q", errB.String())
	}
}

type epipeWriter struct{}

func (epipeWriter) Write(p []byte) (int, error) {
	return 0, syscall.EPIPE
}

func TestCodeOf(t *testing.T) {
	if got := output.CodeOf(errors.New("plain")); got != 1 {
		t.Fatalf("plain %d", got)
	}
	if got := output.CodeOf(output.Auth(errors.New("nope"))); got != 3 {
		t.Fatalf("auth %d", got)
	}
	if got := output.CodeOf(fmt.Errorf("wrapped: %w", output.Usage(errors.New("flag")))); got != 2 {
		t.Fatalf("wrapped %d", got)
	}
}
