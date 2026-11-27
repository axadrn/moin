package cli

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/axadrn/moin/internal/build"
	"github.com/axadrn/moin/internal/daemon"
	"github.com/axadrn/moin/internal/style"
)

// keys is `moin keys <server>`: the SSH keys that may log in as root,
// which is who may run moin there. `keys add` and `keys rm` change them.
func keys(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	if len(args) > 0 && args[0] == "add" {
		return addKey(ctx, args[1:], stdout)
	}
	if len(args) > 0 && args[0] == "rm" {
		return removeKey(ctx, args[1:], stdin, stdout)
	}
	flags := newFlags("keys", "keys <server> [--json], or keys add <server> <file.pub>, or keys rm <server> <name>").withJSON()
	if err := flags.parse(args); err != nil {
		return err
	}
	server, err := flags.server()
	if err != nil {
		return err
	}
	var list []daemon.Key
	if err := sshJSON(ctx, server, []string{"keys"}, nil, &list); err != nil {
		return err
	}
	if flags.json() {
		return printJSON(stdout, list)
	}
	rows := [][]string{}
	for _, key := range list {
		rows = append(rows, []string{dash(key.Comment), style.Muted.Render(key.Fingerprint)})
	}
	table(stdout, []string{"NAME", "FINGERPRINT"}, rows)
	fmt.Fprintln(stdout)
	hint(stdout, "Each key may log in as root and run moin. Add one with", build.Name+" keys add "+server+" anna.pub")
	return nil
}

// addKey adds the public key in a .pub file.
func addKey(ctx context.Context, args []string, stdout io.Writer) error {
	flags := newFlags("keys add", "keys add <server> <file.pub>")
	if err := flags.parse(args); err != nil {
		return err
	}
	if flags.NArg() != 2 {
		return flags.wrong()
	}
	server, file := flags.Arg(0), flags.Arg(1)
	body, err := os.ReadFile(file)
	if err != nil || !isPublicKey(body) {
		return fmt.Errorf("%s is not a public SSH key: pass the .pub file, like ~/.ssh/id_ed25519.pub, never the private key", file)
	}
	var key daemon.Key
	if err := sshJSON(ctx, server, []string{"keys", "add"}, bytes.NewReader(body), &key); err != nil {
		return err
	}
	done(stdout, "%s may log in to %s as root now", dash(key.Comment), server)
	return nil
}

// isPublicKey reports whether body looks like a .pub file: one line that
// starts with a key type. A private key never leaves this computer.
func isPublicKey(body []byte) bool {
	line := strings.TrimSpace(string(body))
	if strings.Contains(line, "\n") {
		return false
	}
	return strings.HasPrefix(line, "ssh-") || strings.HasPrefix(line, "ecdsa-") || strings.HasPrefix(line, "sk-")
}

// removeKey removes a key by its name, the comment at its end, or its
// fingerprint. The server refuses the key this session logged in with.
func removeKey(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	flags := newFlags("keys rm", "keys rm <server> <name or fingerprint> [-y]").withYes()
	if err := flags.parse(args); err != nil {
		return err
	}
	if flags.NArg() != 2 {
		return flags.wrong()
	}
	server, name := flags.Arg(0), flags.Arg(1)
	if err := confirm(stdin, stdout, flags.yes(), fmt.Sprintf("Remove the key %s from %s? It can no longer log in.", name, server)); err != nil {
		return err
	}
	// The session's own address comes from the server's shell: sudo would
	// drop $SSH_CONNECTION.
	line := hereLine("keys", "rm", name) + ` --session "$SSH_CONNECTION"`
	out, err := sshOutput(ctx, server, line, nil)
	if err != nil {
		return err
	}
	var key daemon.Key
	if err := readAnswer(server, out, &key); err != nil {
		return err
	}
	done(stdout, "%s can no longer log in to %s", dash(key.Comment), server)
	return nil
}

// dash is value, or - when it is empty.
func dash(value string) string {
	if value == "" {
		return "-"
	}
	return value
}
