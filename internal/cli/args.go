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
	"github.com/axadrn/moin/internal/client"
	"github.com/axadrn/moin/internal/style"
	"golang.org/x/term"
)

// flagSet is a command's flags. It never prints Go's flag usage: -h shows
// the command's own usage line and its flags, a mistake names the usage
// line.
type flagSet struct {
	*flag.FlagSet
	usage   string
	appFlag *string
	yesFlag *bool
}

// newFlags makes the flags of a command. usage is the command line without
// the binary name, like "logs [--tail 200] [app]".
func newFlags(name, usage string) *flagSet {
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	return &flagSet{FlagSet: flags, usage: build.Name + " " + usage}
}

// withApp adds --app.
func (f *flagSet) withApp() *flagSet {
	f.appFlag = f.String("app", "", "app name; default x-moin.app in compose.yml, then the directory name")
	return f
}

// withYes adds --yes, which skips the question before a destructive step.
func (f *flagSet) withYes() *flagSet {
	f.yesFlag = f.Bool("yes", false, "do it without asking")
	return f
}

// parse reads flags before, between and after the arguments, so
// `rollback v3 --app shop` works like `rollback --app shop v3`. After
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

// app picks the app the command works on: --app, then the argument, then
// x-moin.app in compose.yml, then the directory name. A not found error
// names it and where it came from.
func (f *flagSet) app(argument string) (string, error) {
	return f.appIn(argument, ".")
}

// appIn is app for the repo in dir.
func (f *flagSet) appIn(argument, dir string) (string, error) {
	flagValue := ""
	if f.appFlag != nil {
		flagValue = *f.appFlag
	}
	if flagValue != "" && argument != "" && flagValue != argument {
		return "", fmt.Errorf("two apps: --app %s and %s. Name it once", flagValue, argument)
	}
	from := "--app"
	if flagValue == "" {
		flagValue, from = argument, "the argument"
	}
	name, source := client.PickApp(flagValue, dir)
	if flagValue == "" {
		from = source
	}
	if name == "" {
		return "", errors.New("cannot tell the app: pass it with --app, or set x-moin.app in compose.yml")
	}
	picked = pickedApp{name: name, from: from}
	return name, nil
}

// pickedApp is the app the running command works on, for error messages.
type pickedApp struct{ name, from string }

var picked pickedApp

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
// `db rows orders --limit 5` works like `db rows --limit 5 orders`. After
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
// confirmName asks before something big that cannot be undone: the answer
// must be name, typed out. --yes skips it for scripts.
func confirmName(stdin io.Reader, stdout io.Writer, yes bool, question, name string) error {
	if yes {
		return nil
	}
	if !isTerminal(stdin) {
		return fmt.Errorf("%s Answer it on a terminal, or add --yes", question)
	}
	fmt.Fprintln(stdout, style.Warning.Render("?")+" "+question)
	fmt.Fprint(stdout, "  Type "+style.Bold.Render(name)+" to confirm: ")
	answer, err := readLine(stdin)
	if err != nil {
		return err
	}
	if answer == name {
		return nil
	}
	fmt.Fprintln(stdout, style.Muted.Render("That is not "+name+". Nothing changed."))
	return errReported
}

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
