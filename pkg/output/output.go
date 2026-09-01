package output

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"syscall"

	"github.com/chadsmith12/berth/pkg/cli"
)

const (
	ExitSuccess   = 0
	ExitFailure   = 1
	ExitUsage     = 2
	ExitAuth      = 3
	ExitPreflight = 4
)

type Error struct {
	Code int
	Err  error
}

func (e *Error) Error() string {
	return e.Err.Error()
}

func (e *Error) Unwrap() error {
	return e.Err
}

func Usage(err error) error {
	return &Error{Code: ExitUsage, Err: err}
}

func Auth(err error) error {
	return &Error{Code: ExitAuth, Err: err}
}

func Preflight(err error) error {
	return &Error{Code: ExitPreflight, Err: err}
}

func CodeOf(err error) int {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ExitFailure
}

type Envelope struct {
	Success bool   `json:"success"`
	Command string `json:"command"`
	Data    any    `json:"data,omitempty"`
	Error   string `json:"error,omitempty"`
	Code    int    `json:"code,omitempty"`
}

type TextView interface {
	WriteText(w io.Writer)
}

func WriteJSON(w io.Writer, env Envelope) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(env); err != nil {
		return err
	}
	if _, err := w.Write(buf.Bytes()); err != nil {
		if errors.Is(err, syscall.EPIPE) {
			return nil
		}
		return err
	}
	return nil
}

func Emit(ctx *cli.CmdContext, data any, err error) int {
	if ctx.Globals.JSON {
		env := Envelope{
			Success: err == nil,
			Command: ctx.Command.Name,
			Data:    data,
		}
		if err != nil {
			env.Error = err.Error()
			env.Code = CodeOf(err)
		}
		if werr := WriteJSON(ctx.Stdout, env); werr != nil {
			fmt.Fprintln(ctx.Stderr, werr)
		}
		if err != nil {
			return CodeOf(err)
		}
		return ExitSuccess
	}
	if data != nil {
		if v, ok := data.(TextView); ok {
			v.WriteText(ctx.Stdout)
		}
	}
	if err != nil {
		fmt.Fprintln(ctx.Stderr, err)
		return CodeOf(err)
	}
	return ExitSuccess
}
