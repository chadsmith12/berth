package input

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"golang.org/x/term"
)

var ErrNotInteractive = errors.New("input required but stdin is not interactive")

type Terminal struct {
	in          io.Reader
	out         io.Writer
	interactive bool
}

func New(in io.Reader, out io.Writer, interactive bool) *Terminal {
	return &Terminal{
		in:          in,
		out:         out,
		interactive: interactive,
	}
}

// readLine reads one line without consuming past its newline. A buffered
// reader would read ahead and strand input meant for the next prompt — a
// pasted "url\ntoken\n" must leave the token for the second prompt, even when
// two Terminals share one stdin.
func (t *Terminal) readLine() (string, error) {
	var line strings.Builder
	buf := make([]byte, 1)
	for {
		n, err := t.in.Read(buf)
		if n > 0 {
			switch buf[0] {
			case '\n':
				return line.String(), nil
			case '\r':
			default:
				line.WriteByte(buf[0])
			}
		}
		if err != nil {
			return line.String(), err
		}
	}
}

func (t *Terminal) Prompt(label string) (string, error) {
	if !t.interactive {
		return "", ErrNotInteractive
	}
	fmt.Fprintf(t.out, "%s ", label)
	line, err := t.readLine()
	if err != nil && !errors.Is(err, io.EOF) && line == "" {
		return "", fmt.Errorf("read input: %w", err)
	}
	return strings.TrimSpace(line), nil
}

// PromptPassword reads a secret. On a terminal it disables echo; with a pipe
// it reads one line, so `printf 'tok\n' | berth auth login` works.
func (t *Terminal) PromptPassword(label string) (string, error) {
	fmt.Fprintf(t.out, "%s ", label)
	if f, ok := t.in.(*os.File); ok && t.interactive {
		b, err := term.ReadPassword(int(f.Fd()))
		fmt.Fprintln(t.out)
		if err != nil {
			return "", fmt.Errorf("read input: %w", err)
		}
		return strings.TrimSpace(string(b)), nil
	}
	line, err := t.readLine()
	if err != nil && !errors.Is(err, io.EOF) && line == "" {
		return "", fmt.Errorf("read input: %w", err)
	}
	if strings.TrimSpace(line) == "" {
		return "", errors.New("no value entered")
	}
	return strings.TrimSpace(line), nil
}

// PromptDefault asks for a value, offering def as the accepted default.
// Empty input takes the default; non-interactive stdin takes it directly.
func (t *Terminal) PromptDefault(label, def string) (string, error) {
	if !t.interactive {
		return def, nil
	}
	line, err := t.Prompt(fmt.Sprintf("%s [%s]", label, def))
	if err != nil {
		return "", err
	}
	if line == "" {
		return def, nil
	}
	return line, nil
}

// PromptYesNo asks a yes/no question, offering def when the answer is empty
// or stdin is not interactive.
func (t *Terminal) PromptYesNo(label string, def bool) (bool, error) {
	if !t.interactive {
		return def, nil
	}
	hint := "[y/N]"
	if def {
		hint = "[Y/n]"
	}
	line, err := t.Prompt(fmt.Sprintf("%s %s", label, hint))
	if err != nil {
		return false, err
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "":
		return def, nil
	case "y", "yes", "true":
		return true, nil
	case "n", "no", "false":
		return false, nil
	default:
		return false, fmt.Errorf("please answer y or n, got %q", line)
	}
}

// PromptDefaultInt asks for an integer, offering def when the answer is
// empty or stdin is not interactive.
func (t *Terminal) PromptDefaultInt(label string, def int, validate func(int) error) (int, error) {
	if !t.interactive {
		return def, nil
	}
	for range 3 {
		line, err := t.Prompt(fmt.Sprintf("%s [%d]", label, def))
		if err != nil {
			return 0, err
		}
		if strings.TrimSpace(line) == "" {
			return def, nil
		}
		n, err := strconv.Atoi(strings.TrimSpace(line))
		if err != nil {
			fmt.Fprintf(t.out, "  not a number: %s\n", line)
			continue
		}
		if validate != nil {
			if verr := validate(n); verr != nil {
				fmt.Fprintf(t.out, "  %s\n", verr)
				continue
			}
		}
		return n, nil
	}
	return 0, errors.New("no valid value entered")
}

// Select shows a numbered list and asks for a pick. It returns the index of
// the chosen option. Non-interactive stdin is an error — callers turn that
// into a usage error naming the flag.
func (t *Terminal) Select(label string, options []string) (int, error) {
	if !t.interactive {
		return 0, ErrNotInteractive
	}
	if len(options) == 0 {
		return 0, errors.New("no options to select from")
	}
	fmt.Fprintf(t.out, "%s\n", label)
	for i, o := range options {
		fmt.Fprintf(t.out, "  %d) %s\n", i+1, o)
	}
	for range 3 {
		line, err := t.Prompt("number?")
		if err != nil {
			return 0, err
		}
		n, err := strconv.Atoi(strings.TrimSpace(line))
		if err != nil || n < 1 || n > len(options) {
			fmt.Fprintf(t.out, "  pick a number between 1 and %d\n", len(options))
			continue
		}
		return n - 1, nil
	}
	return 0, errors.New("no valid selection")
}

func (t *Terminal) PromptInt(label string, validate func(int) error) (int, error) {
	line, err := t.Prompt(label)
	if err != nil {
		return 0, err
	}
	if line == "" {
		return 0, errors.New("no value entered")
	}
	n, err := strconv.Atoi(line)
	if err != nil {
		return 0, fmt.Errorf("invalid number %q", line)
	}
	if validate != nil {
		if err := validate(n); err != nil {
			return 0, err
		}
	}
	return n, nil
}
