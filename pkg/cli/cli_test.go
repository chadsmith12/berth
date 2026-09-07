package cli_test

import (
	"bytes"
	"flag"
	"os"
	"strings"
	"testing"

	"github.com/chadsmith12/berth/pkg/cli"
)

func execute(app *cli.App, args ...string) (int, string, string) {
	var out, err bytes.Buffer
	code := app.Execute(args, nil, &out, &err)
	return code, out.String(), err.String()
}

func TestNoArgsShowsHelp(t *testing.T) {
	app := cli.NewApp("berth")
	code, _, stderr := execute(app)
	if code != 2 {
		t.Fatalf("expected 2 got %d", code)
	}
	if !strings.Contains(stderr, "berth <command>") {
		t.Fatalf("expected root help in stderr, got %q", stderr)
	}
}

func TestUnknownCommand(t *testing.T) {
	app := cli.NewApp("berth")
	app.AddCommand(cli.NewCommand("init", "scaffold"))
	code, _, stderr := execute(app, "unknown")
	if code != 2 {
		t.Fatalf("expected 2 got %d", code)
	}
	if !strings.Contains(stderr, "unknown command") {
		t.Fatalf("expected unknown error got %q", stderr)
	}
}

func TestHelpFlag(t *testing.T) {
	app := cli.NewApp("berth")
	app.AddCommand(cli.NewCommand("init", "scaffold"))
	code, stdout, _ := execute(app, "--help")
	if code != 0 {
		t.Fatalf("expected 0 got %d", code)
	}
	if !strings.Contains(stdout, "Commands:") {
		t.Fatalf("expected Commands in stdout got %q", stdout)
	}
}

func TestCommandHelp(t *testing.T) {
	app := cli.NewApp("berth")
	cmd := cli.NewCommand("init", "scaffold")
	cmd.Long = "long desc"
	app.AddCommand(cmd)
	code, stdout, _ := execute(app, "init", "--help")
	if code != 0 {
		t.Fatalf("expected 0 got %d", code)
	}
	if !strings.Contains(stdout, "init - scaffold") {
		t.Fatalf("got %q", stdout)
	}
	if !strings.Contains(stdout, "long desc") {
		t.Fatalf("got %q", stdout)
	}
}

func TestHelpSubcommand(t *testing.T) {
	app := cli.NewApp("berth")
	app.AddCommand(cli.NewCommand("init", "scaffold"))
	code, stdout, _ := execute(app, "help", "init")
	if code != 0 {
		t.Fatalf("expected 0 got %d", code)
	}
	if !strings.Contains(stdout, "init -") {
		t.Fatalf("got %q", stdout)
	}
}

func TestDispatch(t *testing.T) {
	app := cli.NewApp("berth")
	called := false
	cmd := cli.NewCommand("init", "scaffold")
	cmd.Run = func(ctx *cli.CmdContext, args []string) int {
		called = true
		return 7
	}
	app.AddCommand(cmd)
	code, _, _ := execute(app, "init")
	if !called {
		t.Fatal("not called")
	}
	if code != 7 {
		t.Fatalf("expected 7 got %d", code)
	}
}

func TestFlagParsing(t *testing.T) {
	app := cli.NewApp("berth")
	var path string
	cmd := cli.NewCommand("init", "scaffold")
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	fs.StringVar(&path, "path", ".", "path")
	cmd.Flags = fs
	cmd.Run = func(ctx *cli.CmdContext, args []string) int {
		if path != "/tmp" {
			t.Errorf("expected /tmp got %q", path)
		}
		if len(args) != 0 {
			t.Errorf("expected no positional got %v", args)
		}
		return 0
	}
	app.AddCommand(cmd)
	code, _, stderr := execute(app, "init", "--path", "/tmp")
	if code != 0 {
		t.Fatalf("code %d stderr %q", code, stderr)
	}
}

func TestFlagParsingError(t *testing.T) {
	app := cli.NewApp("berth")
	cmd := cli.NewCommand("init", "scaffold")
	cmd.Flags = flag.NewFlagSet("init", flag.ContinueOnError)
	cmd.Flags.Int("workers", -1, "")
	cmd.Run = func(ctx *cli.CmdContext, args []string) int { return 0 }
	app.AddCommand(cmd)
	code, _, _ := execute(app, "init", "--workers", "notanint")
	if code != 2 {
		t.Fatalf("expected 2 got %d", code)
	}
}

func TestPositionalPassedToRun(t *testing.T) {
	app := cli.NewApp("berth")
	cmd := cli.NewCommand("init", "scaffold")
	cmd.Run = func(ctx *cli.CmdContext, args []string) int {
		if len(args) != 1 || args[0] != "pos" {
			t.Fatalf("args %v", args)
		}
		return 0
	}
	app.AddCommand(cmd)
	code, _, _ := execute(app, "init", "pos")
	if code != 0 {
		t.Fatalf("code %d", code)
	}
}

func TestGlobalsBeforeCommand(t *testing.T) {
	app := cli.NewApp("berth")
	var got cli.Globals
	cmd := cli.NewCommand("init", "scaffold")
	cmd.Run = func(ctx *cli.CmdContext, args []string) int {
		got = ctx.Globals
		return 0
	}
	app.AddCommand(cmd)
	code, _, _ := execute(app, "--env", "production", "--json", "-c", "berth.json", "init")
	if code != 0 {
		t.Fatalf("code %d", code)
	}
	if got.Env != "production" || !got.JSON || got.Config != "berth.json" {
		t.Fatalf("globals %+v", got)
	}
}

func TestGlobalsAfterCommand(t *testing.T) {
	app := cli.NewApp("berth")
	var got cli.Globals
	cmd := cli.NewCommand("init", "scaffold")
	cmd.Run = func(ctx *cli.CmdContext, args []string) int {
		got = ctx.Globals
		return 0
	}
	app.AddCommand(cmd)
	code, _, _ := execute(app, "init", "--json")
	if code != 0 {
		t.Fatalf("code %d", code)
	}
	if !got.JSON {
		t.Fatalf("expected json")
	}
}

func TestGlobalsEqualsForm(t *testing.T) {
	app := cli.NewApp("berth")
	var got cli.Globals
	cmd := cli.NewCommand("init", "scaffold")
	cmd.Run = func(ctx *cli.CmdContext, args []string) int { got = ctx.Globals; return 0 }
	app.AddCommand(cmd)
	code, _, _ := execute(app, "--env=staging", "init")
	if code != 0 {
		t.Fatalf("code %d", code)
	}
	if got.Env != "staging" {
		t.Fatalf("got %q", got.Env)
	}
}

func TestMissingGlobalValue(t *testing.T) {
	app := cli.NewApp("berth")
	app.AddCommand(cli.NewCommand("init", "scaffold"))
	code, _, _ := execute(app, "--env")
	if code != 2 {
		t.Fatalf("expected 2 got %d", code)
	}
}

func TestNoCommandWithGlobals(t *testing.T) {
	app := cli.NewApp("berth")
	app.AddCommand(cli.NewCommand("init", "scaffold"))
	code, _, _ := execute(app, "--json")
	if code != 2 {
		t.Fatalf("expected 2 got %d", code)
	}
}

func TestSubcommands(t *testing.T) {
	app := cli.NewApp("berth")
	parent := cli.NewCommand("auth", "auth")
	var called string
	login := cli.NewCommand("login", "login")
	login.Run = func(ctx *cli.CmdContext, args []string) int { called = "login"; return 0 }
	logout := cli.NewCommand("logout", "logout")
	logout.Run = func(ctx *cli.CmdContext, args []string) int { called = "logout"; return 0 }
	parent.AddCommand(login)
	parent.AddCommand(logout)
	app.AddCommand(parent)

	code, _, _ := execute(app, "auth", "login")
	if code != 0 || called != "login" {
		t.Fatalf("code %d called %q", code, called)
	}
	called = ""
	code, _, _ = execute(app, "auth", "logout")
	if code != 0 || called != "logout" {
		t.Fatalf("code %d called %q", code, called)
	}
}

func TestUnknownSubcommand(t *testing.T) {
	app := cli.NewApp("berth")
	parent := cli.NewCommand("auth", "auth")
	parent.AddCommand(cli.NewCommand("login", "login"))
	app.AddCommand(parent)
	code, _, stderr := execute(app, "auth", "nope")
	if code != 2 {
		t.Fatalf("expected 2 got %d", code)
	}
	if !strings.Contains(stderr, "unknown command") {
		t.Fatalf("got %q", stderr)
	}
}

func TestSubcommandFlags(t *testing.T) {
	app := cli.NewApp("berth")
	parent := cli.NewCommand("auth", "auth")
	login := cli.NewCommand("login", "login")
	var token string
	fs := flag.NewFlagSet("login", flag.ContinueOnError)
	fs.StringVar(&token, "token", "", "")
	login.Flags = fs
	login.Run = func(ctx *cli.CmdContext, args []string) int {
		if token != "abc" {
			t.Fatalf("token %q", token)
		}
		return 0
	}
	parent.AddCommand(login)
	app.AddCommand(parent)
	code, _, _ := execute(app, "auth", "login", "--token", "abc")
	if code != 0 {
		t.Fatalf("code %d", code)
	}
}

func TestIOPropagation(t *testing.T) {
	app := cli.NewApp("berth")
	cmd := cli.NewCommand("init", "scaffold")
	cmd.Run = func(ctx *cli.CmdContext, args []string) int {
		ctx.Stdout.Write([]byte("out"))
		ctx.Stderr.Write([]byte("err"))
		return 0
	}
	app.AddCommand(cmd)
	var out, err bytes.Buffer
	code := app.Execute([]string{"init"}, strings.NewReader("in"), &out, &err)
	if code != 0 {
		t.Fatalf("code %d", code)
	}
	if out.String() != "out" || err.String() != "err" {
		t.Fatalf("out %q err %q", out.String(), err.String())
	}
}

func TestConfigShortFlag(t *testing.T) {
	app := cli.NewApp("berth")
	var got string
	cmd := cli.NewCommand("deploy", "deploy")
	cmd.Run = func(ctx *cli.CmdContext, args []string) int { got = ctx.Globals.Config; return 0 }
	app.AddCommand(cmd)
	execute(app, "-c", "custom.json", "deploy")
	if got != "custom.json" {
		t.Fatalf("got %q", got)
	}
}

func TestInteractiveFalseWithBufferStdin(t *testing.T) {
	app := cli.NewApp("berth")
	var got bool
	cmd := cli.NewCommand("init", "scaffold")
	cmd.Run = func(ctx *cli.CmdContext, args []string) int { got = ctx.Interactive; return 0 }
	app.AddCommand(cmd)
	execute(app, "init")
	if got {
		t.Fatal("expected non-interactive with buffer stdin")
	}
}

func TestInteractiveFalseWithPipeStdin(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	app := cli.NewApp("berth")
	var got bool
	cmd := cli.NewCommand("init", "scaffold")
	cmd.Run = func(ctx *cli.CmdContext, args []string) int { got = ctx.Interactive; return 0 }
	app.AddCommand(cmd)
	var out, errB bytes.Buffer
	app.Execute([]string{"init"}, r, &out, &errB)
	if got {
		t.Fatal("expected non-interactive with pipe stdin")
	}
}
