package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"slices"
	"strconv"
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
// the internet. `moin secure` closes the rest too.
//
// Before the server closes anything, it sets a guard: in two minutes it
// opens port 22 again by itself. moin then opens one more new SSH session
// over Tailscale, and only that session stops the guard. So a change that
// would lock you out undoes itself. `moin unlock`, typed in your
// provider's web console, opens port 22 again too.

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
// closes every other port. With --tunnel off the web leaves the tunnel
// and comes on ports 80 and 443 again.
func secure(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	flags := newFlags("secure", "secure <server> [--tunnel off]")
	tunnel := flags.String("tunnel", "", "off: the web on ports 80 and 443 again, not through the tunnel")
	if err := flags.parse(args); err != nil {
		return err
	}
	server, err := flags.server()
	if err != nil {
		return err
	}
	switch *tunnel {
	case "":
	case "off":
		return lockDown(ctx, server, daemon.Secure{TunnelOff: true}, stdin, stdout)
	default:
		return fmt.Errorf("--tunnel takes only off: %s", flags.usage)
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

// lockDown makes change on the server. It first makes sure a new SSH
// session over Tailscale works. After the change, one more new session
// over Tailscale stops the guard, so a change that locks you out undoes
// itself within two minutes.
func lockDown(ctx context.Context, server string, change daemon.Secure, stdin io.Reader, stdout io.Writer) error {
	public, err := sshHostname(ctx, server)
	if err != nil {
		return err
	}
	tailnet, err := joinTailnet(ctx, server, stdin, stdout)
	if err != nil {
		return err
	}
	// Already reached through the tailnet: nothing to point elsewhere.
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
	// The server may fail after it closed something: what closed still
	// has to prove it lets you in.
	var ignored daemon.Security
	failed := sshJSON(ctx, server, []string{"secure"}, strings.NewReader(string(body)), &ignored)
	step(stdout, "trying one more new SSH session over Tailscale, with the ports closed")
	now, err := confirmTailnet(ctx, server, reach)
	if err != nil {
		fmt.Fprintln(stdout, style.Warning.Render("!")+" No new SSH session over Tailscale came through. Within 2 minutes "+server+" opens port 22 again by itself.")
		return errors.Join(failed, err)
	}
	// The name checked above, not the one of a second answer.
	now.Tailnet.Name = tailnet.Name
	printSecurity(stdout, server, public, viaTailnet, change, now)
	if !viaTailnet && now.SSHOnTailscale {
		pointAlias(stdout, server, reach)
	}
	return failed
}

// printSecurity says what is closed on the server now.
func printSecurity(stdout io.Writer, server, public string, viaTailnet bool, change daemon.Secure, now daemon.Security) {
	switch {
	case now.PublicSSH && change.CloseSSH:
		fmt.Fprintln(stdout, style.Warning.Render("!")+" Port 22 of "+server+" is still open to the internet.")
	case now.PublicSSH:
	case !viaTailnet && portAnswers(public, "22"):
		fmt.Fprintln(stdout, style.Warning.Render("!")+" "+public+" still answers on port 22 from here. Check it with `"+build.Name+"`.")
	case now.SSHOnTailscale:
		done(stdout, "Public port 22 is closed. %s answers SSH only over Tailscale", server)
	}
	apps := slices.DeleteFunc(slices.Clone(now.Published), func(port string) bool { return port == "80/tcp" || port == "443/tcp" })
	if now.Firewall {
		ports := slices.DeleteFunc(slices.Clone(now.OpenPorts), func(port string) bool { return port == "41641/udp" })
		open := "none"
		if len(ports) > 0 {
			open = strings.Join(ports, ", ")
		}
		done(stdout, "The firewall is on. Open to the internet: %s", open)
		if len(apps) > 0 {
			fmt.Fprintln(stdout, "  "+style.Muted.Render("Apps publish "+strings.Join(apps, ", ")+". Only your tailnet reaches them."))
		}
	} else if len(apps) > 0 {
		fmt.Fprintln(stdout, style.Warning.Render("!")+" Apps publish "+strings.Join(apps, ", ")+" to the internet. Close them with `"+build.Name+" secure "+server+"`.")
	}
	if now.Tunnel {
		fmt.Fprintln(stdout, "  "+style.Muted.Render("In the tunnel's Public Hostnames, send each domain to http://"+build.Name+"-traefik:8080."))
	}
	if !now.SSHOnTailscale {
		return
	}
	if !now.Tailnet.KeyExpiry.IsZero() {
		fmt.Fprintln(stdout, "  "+style.Muted.Render("Its Tailscale key expires on "+now.Tailnet.KeyExpiry.Local().Format("January 2")+", then SSH stops. "+daemon.KeyExpiryHint(now.Tailnet.Name)))
	}
	hint(stdout, "Locked out one day? In your provider's web console:", remoteBinary+" unlock")
}

// joinTailnet puts the server in your tailnet when it is in none: with
// an auth key you paste, or with a link to log in that Tailscale shows.
func joinTailnet(ctx context.Context, server string, stdin io.Reader, stdout io.Writer) (daemon.Tailnet, error) {
	var tailnet daemon.Tailnet
	if err := sshJSON(ctx, server, []string{"tailnet"}, nil, &tailnet); err != nil {
		return tailnet, err
	}
	if tailnet.IP != "" {
		return checkTailnet(server, tailnet)
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
	if err := sshJSON(ctx, server, []string{"tailnet", "up"}, strings.NewReader(key), &tailnet); err != nil {
		return tailnet, err
	}
	return checkTailnet(server, tailnet)
}

// Tailscale gives its machines addresses from these ranges.
var (
	tailnetIPv4 = netip.MustParsePrefix("100.64.0.0/10")
	tailnetIPv6 = netip.MustParsePrefix("fd7a:115c:a1e0::/48")
)

// checkTailnet checks what the server answered before ssh dials it and
// the terminal shows it: the address must be one of Tailscale's, the name
// only letters, digits, dots and dashes. A name that is not one is
// dropped, so ssh dials the address.
func checkTailnet(server string, tailnet daemon.Tailnet) (daemon.Tailnet, error) {
	ip, err := netip.ParseAddr(tailnet.IP)
	if err != nil || !(tailnetIPv4.Contains(ip) || tailnetIPv6.Contains(ip)) {
		return daemon.Tailnet{}, fmt.Errorf("%s answered a tailnet address that is not one. Public SSH stays open", server)
	}
	if !plainHostname(tailnet.Name) {
		tailnet.Name = ""
	}
	return tailnet, nil
}

// plainHostname reports whether name is only letters, digits, dots and
// dashes, and does not start with a dash.
func plainHostname(name string) bool {
	if name == "" || name[0] == '-' {
		return false
	}
	for _, r := range name {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '-') {
			return false
		}
	}
	return true
}

// tailnetWait is how long moin tries to reach a server over Tailscale.
// A server that just joined takes a few seconds to show up.
const tailnetWait = 60 * time.Second

// tailnetSSH is ssh to server at reach, a tailnet name or address, in a
// new session of its own: never through a connection ssh keeps open to
// the public address, and with nothing to ask.
func tailnetSSH(ctx context.Context, server, reach, line string, options ...string) *exec.Cmd {
	ssh := sshCommand(ctx, server, line, false)
	options = append([]string{"-o", "BatchMode=yes", "-o", "ControlPath=none", "-o", "HostName=" + reach}, options...)
	ssh.Args = slices.Insert(ssh.Args, 1, options...)
	return ssh
}

// untilTailnetWait runs try every few seconds until it works or
// tailnetWait is over.
func untilTailnetWait(ctx context.Context, try func() bool) bool {
	deadline := time.Now().Add(tailnetWait)
	for {
		if try() {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(3 * time.Second):
		}
	}
}

// tryTailnet opens a new SSH session to the server over the tailnet: its
// name, else its address. The server must show the host key it shows on
// its public address, so it is the same machine. Then ssh keeps that key
// under the tailnet name too, as it does for any new host. It returns
// what reached the server.
func tryTailnet(ctx context.Context, server, public string, tailnet daemon.Tailnet) (string, error) {
	reached := ""
	untilTailnetWait(ctx, func() bool {
		for _, reach := range []string{tailnet.Name, tailnet.IP} {
			if reach == "" {
				continue
			}
			if tailnetSSH(ctx, server, reach, asRoot("true"), "-o", "HostKeyAlias="+public).Run() != nil {
				continue
			}
			if tailnetSSH(ctx, server, reach, "true", "-o", "StrictHostKeyChecking=accept-new").Run() == nil {
				reached = reach
				return true
			}
		}
		return false
	})
	if reached != "" {
		return reached, nil
	}
	return "", tailnetBlocked(ctx, server, tailnet)
}

// confirmTailnet stops the guard on the server over a new session through
// the tailnet, and says what is closed now.
func confirmTailnet(ctx context.Context, server, reach string) (daemon.Security, error) {
	var now daemon.Security
	var err error
	untilTailnetWait(ctx, func() bool {
		var out []byte
		out, err = tailnetSSH(ctx, server, reach, asRoot(hereLine("secure", "confirm"))).Output()
		if err == nil {
			err = readAnswer(server, out, &now)
		}
		return err == nil
	})
	return now, err
}

// thisComputer is this computer in Tailscale: whether it runs, its tags
// and the login it runs as.
type thisComputer struct {
	BackendState string
	Self         struct {
		Tags   []string
		UserID int64
	}
	User map[string]struct{ LoginName string }
}

// tailnetBlocked says why this computer does not reach the server over
// Tailscale, and how to fix it. When both are in the tailnet, the
// tailnet's rules keep them apart.
func tailnetBlocked(ctx context.Context, server string, tailnet daemon.Tailnet) error {
	var self thisComputer
	out, err := exec.CommandContext(ctx, "tailscale", "status", "--self", "--peers=false", "--json").Output()
	if err != nil || json.Unmarshal(out, &self) != nil || self.BackendState != "Running" {
		return fmt.Errorf("this computer does not reach %s over Tailscale. Turn Tailscale on here, in the same tailnet, then `%s secure %s`. Public SSH stays open", server, build.Name, server)
	}
	return errors.New(tailnetRulesFix(server, tailnet, self))
}

// tailnetRulesFix is what to add to the tailnet's rules, so this computer
// reaches the server on port 22: a tag for servers, and a grant from
// this computer to it.
func tailnetRulesFix(server string, tailnet daemon.Tailnet, self thisComputer) string {
	source := "autogroup:member"
	if len(self.Self.Tags) > 0 {
		source = strings.Join(self.Self.Tags, `", "`)
	} else if user, ok := self.User[strconv.FormatInt(self.Self.UserID, 10)]; ok && user.LoginName != "" {
		source = user.LoginName
	}
	machine, _, _ := strings.Cut(tailnet.Name, ".")
	if machine == "" {
		machine = server
	}
	return fmt.Sprintf(`this computer is in your tailnet, but the tailnet's rules do not let it reach %s on port 22. Public SSH stays open.
  Fix it in the Tailscale admin console, Access controls. Add a tag for servers and let this computer reach it:
    "tagOwners": {"tag:server": ["autogroup:admin"]},
    "grants": [{"src": ["%s"], "dst": ["tag:server"], "ip": ["tcp:22"]}]
  Then give the server the tag: Machines, %s, the ⋯ menu, Edit ACL tags, tag:server.
  Then run %s secure %s again`, server, source, machine, build.Name, server)
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

// hereSecure is `here secure`, with what to close as JSON on stdin, and
// `here secure confirm`, which stops the guard.
func hereSecure(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	switch {
	case len(args) == 1 && args[0] == "confirm":
		now, err := daemon.ConfirmHere(ctx, daemon.StateDir())
		return respond(stdout, now, err)
	case len(args) > 0:
		return errors.New("usage: here secure [confirm]")
	}
	var change daemon.Secure
	if err := json.NewDecoder(stdin).Decode(&change); err != nil {
		return fmt.Errorf("read what to close: %w", err)
	}
	now, err := daemon.SecureHere(ctx, daemon.StateDir(), change, os.Stderr)
	return respond(stdout, now, err)
}
