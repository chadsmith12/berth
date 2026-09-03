package input

import (
	"bufio"
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
	In          io.Reader
	Out         io.Writer
	Interactive bool

	reader *bufio.Reader
}

func New(in io.Reader, out io.Writer, interactive bool) *Terminal {
	return &Terminal{
		In:          in,
		Out:         out,
		Interactive: interactive,
		reader:      bufio.NewReader(in),
	}
}

func (t *Terminal) Prompt(label string) (string, error) {
	if !t.Interactive {
		return "", ErrNotInteractive
	}
	fmt.Fprintf(t.Out, "%s ", label)
	line, err := t.reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) && line == "" {
		return "", fmt.Errorf("read input: %w", err)
	}
	return strings.TrimSpace(line), nil
}

// PromptPassword reads a secret. On a terminal it disables echo; with a pipe
// it reads one line, so `printf 'tok\n' | berth auth login` works.
func (t *Terminal) PromptPassword(label string) (string, error) {
	fmt.Fprintf(t.Out, "%s ", label)
	if f, ok := t.In.(*os.File); ok && t.Interactive {
		b, err := term.ReadPassword(int(f.Fd()))
		fmt.Fprintln(t.Out)
		if err != nil {
			return "", fmt.Errorf("read input: %w", err)
		}
		return strings.TrimSpace(string(b)), nil
	}
	line, err := t.reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) && line == "" {
		return "", fmt.Errorf("read input: %w", err)
	}
	if strings.TrimSpace(line) == "" {
		return "", errors.New("no value entered")
	}
	return strings.TrimSpace(line), nil
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
