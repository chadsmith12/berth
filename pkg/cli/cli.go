package cli

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

type Globals struct {
	Env    string
	JSON   bool
	Yes    bool
	Config string
}

type CmdContext struct {
	Globals     Globals
	Command     *Command
	Interactive bool
	Stdin       io.Reader
	Stdout      io.Writer
	Stderr      io.Writer
}

type Command struct {
	Name  string
	Short string
	Long  string
	Run   func(ctx *CmdContext, args []string) int
	Flags *flag.FlagSet
	cmds  map[string]*Command
	order []*Command
}

func NewCommand(name, short string) *Command {
	return &Command{
		Name:  name,
		Short: short,
		cmds:  make(map[string]*Command),
	}
}

func (c *Command) AddCommand(sub *Command) {
	if c.cmds == nil {
		c.cmds = make(map[string]*Command)
	}
	c.cmds[sub.Name] = sub
	c.order = append(c.order, sub)
}

func (c *Command) Commands() []*Command {
	out := make([]*Command, len(c.order))
	copy(out, c.order)
	return out
}

type App struct {
	Name     string
	Version  string
	commands map[string]*Command
	order    []*Command
}

func NewApp(name string) *App {
	return &App{
		Name:     name,
		commands: make(map[string]*Command),
	}
}

func (a *App) AddCommand(c *Command) {
	a.commands[c.Name] = c
	a.order = append(a.order, c)
}

func (a *App) Commands() []*Command {
	out := make([]*Command, len(a.order))
	copy(out, a.order)
	return out
}

func (a *App) Execute(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if stdout == nil {
		stdout = io.Discard
	}
	if stderr == nil {
		stderr = io.Discard
	}
	globals, remaining, err := parseGlobals(args)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	if len(remaining) == 1 && isHelp(remaining) {
		printRootHelp(a, stdout)
		return 0
	}
	if len(remaining) == 0 {
		if globals.JSON || globals.Yes || globals.Env != "" || globals.Config != "" {
			fmt.Fprintln(stderr, "no command specified")
			return 2
		}
		printRootHelp(a, stderr)
		return 2
	}
	name := remaining[0]
	if name == "help" {
		if len(remaining) > 1 {
			if cmd, ok := a.commands[remaining[1]]; ok {
				printCommandHelp(cmd, stdout)
				return 0
			}
			fmt.Fprintf(stderr, "unknown command %q\n", remaining[1])
			return 2
		}
		printRootHelp(a, stdout)
		return 0
	}
	cmd, ok := a.commands[name]
	if !ok {
		fmt.Fprintf(stderr, "unknown command %q\n", name)
		printRootHelp(a, stderr)
		return 2
	}
	ctx := &CmdContext{
		Globals:     globals,
		Command:     cmd,
		Interactive: isTerminal(stdin),
		Stdin:       stdin,
		Stdout:      stdout,
		Stderr:      stderr,
	}
	return cmd.dispatch(ctx, remaining[1:])
}

func isTerminal(r io.Reader) bool {
	f, ok := r.(*os.File)
	if !ok {
		return false
	}
	return term.IsTerminal(int(f.Fd()))
}

func (c *Command) dispatch(ctx *CmdContext, args []string) int {
	ctx.Command = c
	if len(c.cmds) > 0 {
		if len(args) > 0 && !isFlag(args[0]) {
			if sub, ok := c.cmds[args[0]]; ok {
				if isHelp(args[1:]) {
					printCommandHelp(sub, ctx.Stdout)
					return 0
				}
				return sub.dispatch(ctx, args[1:])
			}
			if args[0] == "help" {
				if len(args) > 1 {
					if sub, ok := c.cmds[args[1]]; ok {
						printCommandHelp(sub, ctx.Stdout)
						return 0
					}
					fmt.Fprintf(ctx.Stderr, "unknown command %q\n", args[1])
					return 2
				}
				printCommandHelp(c, ctx.Stdout)
				return 0
			}
			fmt.Fprintf(ctx.Stderr, "unknown command %q\n", args[0])
			printCommandHelp(c, ctx.Stderr)
			return 2
		}
		if isHelp(args) {
			printCommandHelp(c, ctx.Stdout)
			return 0
		}
		if len(args) == 0 {
			printCommandHelp(c, ctx.Stdout)
			return 0
		}
	}
	if isHelp(args) {
		printCommandHelp(c, ctx.Stdout)
		return 0
	}
	if c.Flags != nil {
		c.Flags.SetOutput(ctx.Stderr)
		if err := c.Flags.Parse(args); err != nil {
			return 2
		}
		args = c.Flags.Args()
	}
	if c.Run == nil {
		printCommandHelp(c, ctx.Stdout)
		return 0
	}
	return c.Run(ctx, args)
}

func isFlag(s string) bool {
	return strings.HasPrefix(s, "-")
}

func isHelp(args []string) bool {
	for _, a := range args {
		if a == "--help" || a == "-h" || a == "-help" {
			return true
		}
	}
	return false
}

func parseGlobals(args []string) (Globals, []string, error) {
	var g Globals
	var remaining []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--json":
			g.JSON = true
		case strings.HasPrefix(a, "--json="):
			v := strings.TrimPrefix(a, "--json=")
			if v != "true" && v != "false" && v != "" {
				return g, nil, fmt.Errorf("invalid --json value %q", v)
			}
			g.JSON = v == "true" || v == ""
		case a == "--yes" || a == "-y":
			g.Yes = true
		case a == "--env":
			if i+1 >= len(args) {
				return g, nil, fmt.Errorf("flag --env requires a value")
			}
			i++
			g.Env = args[i]
		case strings.HasPrefix(a, "--env="):
			g.Env = strings.TrimPrefix(a, "--env=")
		case a == "-c":
			if i+1 >= len(args) {
				return g, nil, fmt.Errorf("flag -c requires a value")
			}
			i++
			g.Config = args[i]
		case strings.HasPrefix(a, "-c="):
			g.Config = strings.TrimPrefix(a, "-c=")
		default:
			if strings.HasPrefix(a, "--env") || a == "--json" || strings.HasPrefix(a, "--json=") {
				return g, nil, fmt.Errorf("invalid flag %q", a)
			}
			remaining = append(remaining, a)
		}
	}
	return g, remaining, nil
}

func printRootHelp(a *App, w io.Writer) {
	fmt.Fprintf(w, "%s <command>\n\n", a.Name)
	if len(a.order) > 0 {
		fmt.Fprintln(w, "Commands:")
		for _, c := range a.order {
			fmt.Fprintf(w, "  %-12s %s\n", c.Name, c.Short)
		}
		fmt.Fprintln(w, "")
	}
	fmt.Fprintln(w, "Global flags:")
	fmt.Fprintln(w, "  --env <name>     environment")
	fmt.Fprintln(w, "  --json           machine-readable output")
	fmt.Fprintln(w, "  --yes, -y        assume yes")
	fmt.Fprintln(w, "  -c <path>        config file")
	fmt.Fprintln(w, "  -h, --help       help")
}

func printCommandHelp(c *Command, w io.Writer) {
	fmt.Fprintf(w, "%s - %s\n", c.Name, c.Short)
	if c.Long != "" {
		fmt.Fprintf(w, "\n%s\n", c.Long)
	}
	if c.Flags != nil {
		fmt.Fprintln(w, "")
		fmt.Fprintln(w, "Flags:")
		c.Flags.SetOutput(w)
		c.Flags.PrintDefaults()
	}
	if len(c.order) > 0 {
		fmt.Fprintln(w, "")
		fmt.Fprintln(w, "Subcommands:")
		for _, sub := range c.order {
			fmt.Fprintf(w, "  %-12s %s\n", sub.Name, sub.Short)
		}
	}
	if c.Flags == nil && len(c.order) == 0 {
		fmt.Fprintln(w, "")
	}
}
