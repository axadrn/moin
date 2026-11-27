package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"slices"
	"strings"
	"time"

	"github.com/axadrn/moin/internal/build"
	"github.com/axadrn/moin/internal/daemon"
	"github.com/axadrn/moin/internal/style"
)

// Closing a server. A fresh server answers SSH on public port 22, so the
// first deploy goes that way. Right after it moin offers to close it:
// the server joins your tailnet, moin tries a new SSH session over
// Tailscale, and only when that works does port 22 close to the rest of
// the internet. `moin secure` closes the rest too. `moin unlock`, typed in
// your provider's web console, opens port 22 again.

// offerClose is the question after the first deploy to a server.
func offerClose(ctx context.Context, server string, stdin io.Reader, stdout io.Writer) error {
	status, err := serverStatus(ctx, server)
	if err != nil || !status.PublicSSH {
		return nil
	}
	fmt.Fprintln(stdout)
	if !isTerminal(stdin) {
		hint(stdout, "Port 22 of "+server+" is open to the internet. Close it, SSH only over Tailscale:", build.Name+" secure "+server)
		return nil
	}
	yes, err := askYes(stdin, stdout, "Close public SSH, only Tailscale from now on?")
	if err != nil {
		return err
	}
	if !yes {
		hint(stdout, "Port 22 stays open to the internet. Close it later with", build.Name+" secure "+server)
		return nil
	}
	return lockDown(ctx, server, daemon.Secure{CloseSSH: true}, stdin, stdout)
}

// secure is `moin secure <server>`: public SSH closes, the web goes
// through a Cloudflare Tunnel when you paste its token, and a firewall
// closes every other port.
func secure(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	flags := newFlags("secure", "secure <server>")
	if err := flags.parse(args); err != nil {
		return err
	}
	server, err := flags.server()
	if err != nil {
		return err
	}
	token, err := askTunnelToken(stdin, stdout)
	if err != nil {
		return err
	}
	return lockDown(ctx, server, daemon.Secure{CloseSSH: true, Firewall: true, TunnelToken: token}, stdin, stdout)
}

// askTunnelToken asks for the token of a Cloudflare Tunnel on a terminal,
// and reads it from stdin without one. Empty keeps the web on ports 80
// and 443.
func askTunnelToken(stdin io.Reader, stdout io.Writer) (string, error) {
	if !isTerminal(stdin) {
		return readLine(stdin)
	}
	fmt.Fprintln(stdout, style.Muted.Render("A Cloudflare Tunnel brings the web to the server, so ports 80 and 443 can close too."))
	fmt.Fprintln(stdout, style.Muted.Render("Make one at https://one.dash.cloudflare.com: Networks, Tunnels, Create a tunnel, Cloudflared."))
	fmt.Fprint(stdout, style.Warning.Render("?")+" Its token, the long text after --token, or Enter to keep the web on 80 and 443: ")
	token, err := readSecret(stdin)
	fmt.Fprintln(stdout)
	return token, err
}

// lockDown closes what change asks for. When it closes SSH it first
// makes sure a new session over Tailscale works, so you never lock
// yourself out.
func lockDown(ctx context.Context, server string, change daemon.Secure, stdin io.Reader, stdout io.Writer) error {
	public, err := sshHostname(ctx, server)
	if err != nil {
		return err
	}
	tailnet, err := joinTailnet(ctx, server, stdin, stdout)
	if err != nil {
		return err
	}
	// Already reached through the tailnet: nothing to try, nothing to
	// point elsewhere.
	viaTailnet := public == tailnet.Name || public == tailnet.IP
	reach := public
	if !viaTailnet {
		step(stdout, "trying a new SSH session to %s over Tailscale", server)
		reach, err = tryTailnet(ctx, server, public, tailnet)
		if err != nil {
			return err
		}
	}
	step(stdout, "closing the ports")
	body, err := json.Marshal(change)
	if err != nil {
		return err
	}
	var now daemon.Security
	if err := sshJSON(ctx, server, []string{"secure"}, strings.NewReader(string(body)), &now); err != nil {
		return err
	}
	if !viaTailnet && portAnswers(public, "22") {
		fmt.Fprintln(stdout, style.Warning.Render("!")+" "+public+" still answers on port 22 from here. Check it with `"+build.Name+"`.")
	} else {
		done(stdout, "Public port 22 is closed. %s answers SSH only over Tailscale", server)
	}
	if now.Firewall {
		ports := slices.DeleteFunc(slices.Clone(now.OpenPorts), func(port string) bool { return port == "41641/udp" })
		open := "none"
		if len(ports) > 0 {
			open = strings.Join(ports, ", ")
		}
		done(stdout, "The firewall is on. Open to the internet: %s", open)
	}
	if now.Tunnel {
		fmt.Fprintln(stdout, "  "+style.Muted.Render("In the tunnel's Public Hostnames, send each domain to http://"+build.Name+"-traefik:8080."))
	}
	if !viaTailnet {
		pointAlias(stdout, server, reach)
	}
	return nil
}

// joinTailnet puts the server in your tailnet when it is in none: with
// an auth key you paste, or with a link to log in that Tailscale shows.
func joinTailnet(ctx context.Context, server string, stdin io.Reader, stdout io.Writer) (daemon.Tailnet, error) {
	var tailnet daemon.Tailnet
	if err := sshJSON(ctx, server, []string{"tailnet"}, nil, &tailnet); err != nil {
		return tailnet, err
	}
	if tailnet.IP != "" {
		return tailnet, nil
	}
	key := ""
	if isTerminal(stdin) {
		fmt.Fprintln(stdout, style.Muted.Render(server+" joins your tailnet, your own private network. Keys: https://login.tailscale.com/admin/settings/keys"))
		fmt.Fprint(stdout, style.Warning.Render("?")+" A Tailscale auth key, or Enter to log in with a link: ")
		var err error
		if key, err = readSecret(stdin); err != nil {
			return tailnet, err
		}
		fmt.Fprintln(stdout)
	}
	step(stdout, "putting %s in your tailnet", server)
	err := sshJSON(ctx, server, []string{"tailnet", "up"}, strings.NewReader(key), &tailnet)
	return tailnet, err
}

// tryTailnet opens a new SSH session to the server over the tailnet: its
// name, else its address. The server must show the host key it shows on
// its public address, so it is the same machine. Then ssh keeps that key
// under the tailnet name too, as it does for any new host. It returns
// what reached the server.
func tryTailnet(ctx context.Context, server, public string, tailnet daemon.Tailnet) (string, error) {
	for _, reach := range []string{tailnet.Name, tailnet.IP} {
		if reach == "" {
			continue
		}
		same := sshCommand(ctx, server, asRoot("true"), false)
		same.Args = slices.Insert(same.Args, 1, "-o", "BatchMode=yes", "-o", "HostName="+reach, "-o", "HostKeyAlias="+public)
		if same.Run() != nil {
			continue
		}
		keep := sshCommand(ctx, server, "true", false)
		keep.Args = slices.Insert(keep.Args, 1, "-o", "BatchMode=yes", "-o", "HostName="+reach, "-o", "StrictHostKeyChecking=accept-new")
		if keep.Run() == nil {
			return reach, nil
		}
	}
	return "", fmt.Errorf("this computer does not reach %s over Tailscale. Turn Tailscale on here, in the same tailnet, then `%s secure %s`. Public SSH stays open", server, build.Name, server)
}

// pointAlias says how to reach the server from now on. moin does not
// edit your ssh config: the alias may live in an Include, a Match block
// or your dotfiles, and a wrong edit locks you out of more than moin.
func pointAlias(stdout io.Writer, server, reach string) {
	if user, _, ok := strings.Cut(server, "@"); ok {
		fmt.Fprintln(stdout, "  "+style.Muted.Render("From now on, reach it as ")+command(stdout, user+"@"+reach)+style.Muted.Render("."))
		return
	}
	fmt.Fprintln(stdout, "  "+style.Muted.Render("Point "+server+" at the tailnet. In ~/.ssh/config, set:"))
	fmt.Fprintln(stdout, "    Host "+server)
	fmt.Fprintln(stdout, "      HostName "+reach)
	fmt.Fprintln(stdout, "  "+style.Muted.Render("Until then, ssh "+server+" knocks on the closed port."))
}

// sshHostname is the address ssh dials for server, from `ssh -G`.
func sshHostname(ctx context.Context, server string) (string, error) {
	out, err := exec.CommandContext(ctx, "ssh", "-G", "--", server).Output()
	if err != nil {
		return "", fmt.Errorf("ssh -G %s: %w", server, err)
	}
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		if name, ok := strings.CutPrefix(scanner.Text(), "hostname "); ok {
			return name, nil
		}
	}
	return "", fmt.Errorf("ssh -G %s names no host", server)
}

// portAnswers says whether host takes a TCP connection on port.
func portAnswers(host, port string) bool {
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, port), 5*time.Second)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// askYes asks a question that Enter answers with yes.
func askYes(stdin io.Reader, stdout io.Writer, question string) (bool, error) {
	fmt.Fprint(stdout, style.Warning.Render("?")+" "+question+" "+style.Muted.Render("[Y/n]")+" ")
	answer, err := readLine(stdin)
	if err != nil {
		return false, err
	}
	switch strings.ToLower(answer) {
	case "", "y", "yes":
		return true, nil
	}
	return false, nil
}

// unlock is `moin unlock`, typed on the server in your provider's web
// console: public port 22 opens again.
func unlock(ctx context.Context, args []string, stdout io.Writer) error {
	if err := noArguments("unlock", args); err != nil {
		return err
	}
	if err := daemon.UnlockHere(ctx, daemon.StateDir()); err != nil {
		return err
	}
	done(stdout, "Port 22 is open to the internet again. SSH works as on day one")
	hint(stdout, "Close it again from your laptop with", build.Name+" secure <server>")
	return nil
}

// hereTailnet is `here tailnet`, where the server is in a tailnet, and
// `here tailnet up` with an auth key, or none, on stdin.
func hereTailnet(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	switch {
	case len(args) == 0:
		return printJSON(stdout, daemon.TailnetHere(ctx))
	case len(args) == 1 && args[0] == "up":
		key, err := readLine(stdin)
		if err != nil {
			return err
		}
		tailnet, err := daemon.JoinTailnetHere(ctx, key, os.Stderr)
		return respond(stdout, tailnet, err)
	}
	return errors.New("usage: here tailnet [up]")
}

// hereSecure is `here secure`, with what to close as JSON on stdin.
func hereSecure(ctx context.Context, stdin io.Reader, stdout io.Writer) error {
	var change daemon.Secure
	if err := json.NewDecoder(stdin).Decode(&change); err != nil {
		return fmt.Errorf("read what to close: %w", err)
	}
	now, err := daemon.SecureHere(ctx, daemon.StateDir(), change, os.Stderr)
	return respond(stdout, now, err)
}
