package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"github.com/axadrn/moin/internal/build"
	"github.com/axadrn/moin/internal/style"
	"github.com/axadrn/moin/protocol"
	"golang.org/x/term"
)

// flagSet is a command's flags. It never prints Go's flag usage: -h shows
// the command's own usage line and its flags, a mistake names the usage
// line.
type flagSet struct {
	*flag.FlagSet
	usage      string
	appFlag    *string
	serverFlag *string
	yesFlag    *bool
	jsonFlag   *bool
}

// newFlags makes the flags of a command. usage is the command line without
// the binary name, like "logs <server> [--tail 200]".
func newFlags(name, usage string) *flagSet {
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	return &flagSet{FlagSet: flags, usage: build.Name + " " + usage}
}

// withApp adds --app.
func (f *flagSet) withApp() *flagSet {
	f.appFlag = f.String("app", "", "app name; default the directory name")
	return f
}

// withYes adds --yes and -y, which skip the question before a destructive
// step.
func (f *flagSet) withYes() *flagSet {
	f.yesFlag = new(bool)
	f.BoolVar(f.yesFlag, "y", false, "do it without asking")
	f.BoolVar(f.yesFlag, "yes", false, "do it without asking")
	return f
}

// withJSON adds --json, for scripts and AI tools.
func (f *flagSet) withJSON() *flagSet {
	f.jsonFlag = f.Bool("json", false, "print JSON")
	return f
}

// parse reads flags before, between and after the arguments, so
// `logs prod -f` works like `logs -f prod`. After
// "--" everything is an argument.
func (f *flagSet) parse(args []string) error {
	return f.check(parseAnywhere(f.FlagSet, args), args)
}

// parseStrict reads flags only before the arguments, for commands like
// `run -- <command>` whose arguments carry their own flags.
func (f *flagSet) parseStrict(args []string) error {
	return f.check(f.Parse(args), args)
}

// check turns a parse error into the command's help or a message with its
// usage line. An unknown flag shows as typed: --yes, not Go's -yes.
func (f *flagSet) check(err error, args []string) error {
	if errors.Is(err, flag.ErrHelp) {
		return helpError{f}
	}
	if err != nil {
		message := err.Error()
		if name, ok := strings.CutPrefix(message, "flag provided but not defined: "); ok {
			if slices.ContainsFunc(args, func(arg string) bool { return arg == "-"+name || strings.HasPrefix(arg, "-"+name+"=") }) {
				name = "-" + name
			}
			message = "unknown flag " + name
		}
		return fmt.Errorf("%s. Usage: `%s`", message, f.usage)
	}
	return nil
}

// wrong is the error of arguments that do not fit the usage line.
func (f *flagSet) wrong() error {
	return fmt.Errorf("usage: `%s`", f.usage)
}

// yes reports whether --yes was given.
func (f *flagSet) yes() bool { return f.yesFlag != nil && *f.yesFlag }

// json reports whether --json was given.
func (f *flagSet) json() bool { return f.jsonFlag != nil && *f.jsonFlag }

// server is the one argument, the server, when the command takes nothing
// else.
func (f *flagSet) server() (string, error) {
	if f.NArg() != 1 {
		return "", f.wrong()
	}
	return f.Arg(0), nil
}

// app is the app of deploy: --app, or the name of this folder.
func (f *flagSet) app() (string, error) {
	if f.appFlag == nil || *f.appFlag == "" {
		return folderApp()
	}
	if !protocol.ValidAppName(*f.appFlag) {
		return "", fmt.Errorf("%q is not an app name: lowercase letters, digits and dashes", *f.appFlag)
	}
	return *f.appFlag, nil
}

// helpError carries the help of a command: Run prints it and exits with 0.
type helpError struct{ flags *flagSet }

func (h helpError) Error() string { return "help" }

// print writes the usage line and the flags.
func (h helpError) print(w io.Writer) {
	fmt.Fprintln(w, style.Bold.Render("Usage")+"  "+h.flags.usage)
	rows := [][]string{}
	h.flags.VisitAll(func(each *flag.Flag) {
		text := each.Usage
		if each.DefValue != "" && each.DefValue != "false" && each.DefValue != "0" {
			text += " (default " + each.DefValue + ")"
		}
		rows = append(rows, []string{"  --" + each.Name, style.Muted.Render(text)})
	})
	if len(rows) == 0 {
		return
	}
	fmt.Fprintln(w)
	widest := 0
	for _, row := range rows {
		widest = max(widest, len(row[0]))
	}
	for _, row := range rows {
		fmt.Fprintln(w, row[0]+strings.Repeat(" ", widest-len(row[0])+2)+row[1])
	}
}

// parseAnywhere parses flags before and after the arguments, so
// `logs prod --tail 5` works like `logs --tail 5 prod`. After
// "--" everything is an argument.
func parseAnywhere(flags *flag.FlagSet, args []string) error {
	positional := []string{}
	for len(args) > 0 {
		if err := flags.Parse(args); err != nil {
			return err
		}
		rest := flags.Args()
		used := len(args) - len(rest)
		if used > 0 && args[used-1] == "--" {
			positional = append(positional, rest...)
			break
		}
		if len(rest) == 0 {
			break
		}
		positional = append(positional, rest[0])
		args = rest[1:]
	}
	return flags.Parse(append([]string{"--"}, positional...))
}

// isTerminal reports whether r is a terminal people type into.
func isTerminal(r io.Reader) bool {
	file, ok := r.(*os.File)
	return ok && term.IsTerminal(int(file.Fd()))
}

// confirm asks before a destructive step. --yes skips the question; without
// a terminal and without --yes it refuses, so a script never removes
// something by accident.
func confirm(stdin io.Reader, stdout io.Writer, yes bool, question string) error {
	if yes {
		return nil
	}
	if !isTerminal(stdin) {
		return fmt.Errorf("%s Answer it on a terminal, or add --yes", question)
	}
	fmt.Fprint(stdout, style.Warning.Render("?")+" "+question+" "+style.Muted.Render("[y/N]")+" ")
	answer, err := readLine(stdin)
	if err != nil {
		return err
	}
	switch strings.ToLower(answer) {
	case "y", "yes":
		return nil
	}
	fmt.Fprintln(stdout, style.Muted.Render("Nothing changed."))
	return errReported
}

// readSecret reads one line without echo from a terminal, or plainly from
// a pipe.
func readSecret(stdin io.Reader) (string, error) {
	if file, ok := stdin.(*os.File); ok && term.IsTerminal(int(file.Fd())) {
		secret, err := term.ReadPassword(int(file.Fd()))
		return strings.TrimSpace(string(secret)), err
	}
	return readLine(stdin)
}
