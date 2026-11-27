package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
)

// Who may run moin on a server is whoever can SSH to it as root: a team
// is the keys in root's authorized_keys. `moin keys` edits that file.

// authorizedKeys is root's file of SSH keys.
var authorizedKeys = "/root/.ssh/authorized_keys"

// Key is one SSH key that may log in as root.
type Key struct {
	Type        string `json:"type"`
	Comment     string `json:"comment"`
	Fingerprint string `json:"fingerprint"`
	line        string
}

// KeysHere lists the keys in root's authorized_keys.
func KeysHere() ([]Key, error) {
	body, err := os.ReadFile(authorizedKeys)
	if errors.Is(err, os.ErrNotExist) {
		return []Key{}, nil
	}
	if err != nil {
		return nil, err
	}
	keys := []Key{}
	for line := range strings.Lines(string(body)) {
		if key, ok := parseKey(line); ok {
			keys = append(keys, key)
		}
	}
	return keys, nil
}

// AddKeyHere adds one public key, a line of a .pub file.
func AddKeyHere(line string) (Key, error) {
	key, ok := parseKey(line)
	if !ok {
		return Key{}, errors.New("that is not a public SSH key: pass the .pub file, like ~/.ssh/id_ed25519.pub")
	}
	keys, err := KeysHere()
	if err != nil {
		return Key{}, err
	}
	if slices.ContainsFunc(keys, func(k Key) bool { return k.Fingerprint == key.Fingerprint }) {
		return Key{}, fmt.Errorf("the key %s is there already", key.Fingerprint)
	}
	body, _ := os.ReadFile(authorizedKeys)
	text := string(body)
	if text != "" && !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	return key, writeWhole(authorizedKeys, []byte(text+key.line+"\n"))
}

// RemoveKeyHere removes the key whose comment or fingerprint is match. It
// refuses the key this SSH session logged in with, so you never lock
// yourself out. session is $SSH_CONNECTION of the session.
func RemoveKeyHere(ctx context.Context, match, session string) (Key, error) {
	keys, err := KeysHere()
	if err != nil {
		return Key{}, err
	}
	var matches []Key
	for _, key := range keys {
		if key.Fingerprint == match || key.Comment == match {
			matches = append(matches, key)
		}
	}
	if len(matches) == 0 {
		return Key{}, fmt.Errorf("no key %q: see them with `moin keys <server>`", match)
	}
	if len(matches) > 1 {
		return Key{}, fmt.Errorf("%d keys are called %q: name the one to remove by its fingerprint", len(matches), match)
	}
	key := matches[0]
	own := sessionKey(ctx, session)
	if own == "" {
		return Key{}, errors.New("cannot tell which key this SSH session uses, so moin removes none. Remove the line from " + authorizedKeys + " by hand")
	}
	if own == key.Fingerprint {
		return Key{}, errors.New("this SSH session logged in with that key: remove it from another key's session")
	}
	body, err := os.ReadFile(authorizedKeys)
	if err != nil {
		return Key{}, err
	}
	var kept strings.Builder
	for line := range strings.Lines(string(body)) {
		if parsed, ok := parseKey(line); ok && parsed.Fingerprint == key.Fingerprint {
			continue
		}
		kept.WriteString(line)
	}
	return key, writeWhole(authorizedKeys, []byte(kept.String()))
}

// parseKey reads one line of authorized_keys: options may come first,
// then the type, the key and a comment. Blank lines and # comments are no
// key.
func parseKey(line string) (Key, bool) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return Key{}, false
	}
	fields := strings.Fields(line)
	for i := 0; i+1 < len(fields); i++ {
		if !isKeyType(fields[i]) {
			continue
		}
		blob, err := base64.StdEncoding.DecodeString(fields[i+1])
		if err != nil {
			return Key{}, false
		}
		sum := sha256.Sum256(blob)
		return Key{
			Type:        fields[i],
			Comment:     strings.Join(fields[i+2:], " "),
			Fingerprint: "SHA256:" + base64.RawStdEncoding.EncodeToString(sum[:]),
			line:        line,
		}, true
	}
	return Key{}, false
}

// isKeyType reports whether word is an SSH key type, like ssh-ed25519.
func isKeyType(word string) bool {
	return strings.HasPrefix(word, "ssh-") || strings.HasPrefix(word, "ecdsa-sha2-") || strings.HasPrefix(word, "sk-")
}

// sessionKey is the fingerprint of the key the SSH session logged in with,
// from sshd's line in the journal, like "Accepted publickey for root from
// 203.0.113.7 port 51234 ssh2: ED25519 SHA256:…". "" when it is not there.
func sessionKey(ctx context.Context, session string) string {
	fields := strings.Fields(session)
	if len(fields) < 2 {
		return ""
	}
	from := " from " + fields[0] + " port " + fields[1] + " "
	output, err := exec.CommandContext(ctx, "journalctl", "--no-pager", "-o", "cat", "-n", "5000", "-t", "sshd", "-t", "sshd-session").Output()
	if err != nil {
		return ""
	}
	own := ""
	for line := range strings.Lines(string(output)) {
		if strings.HasPrefix(line, "Accepted publickey ") && strings.Contains(line, from) {
			words := strings.Fields(line)
			own = words[len(words)-1]
		}
	}
	return own
}
