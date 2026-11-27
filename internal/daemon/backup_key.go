package daemon

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"filippo.io/age"

	"github.com/axadrn/moin/protocol"
)

// Every copy is encrypted on the server before it leaves, with age, for
// two keys:
//
//   - The server's own key, made here. Its private half is in
//     /etc/moin/backup.key, for root only: the daily check and a restore
//     open the copies with it.
//   - Your key, made by the CLI on your laptop and shown once. The server
//     only ever gets its public half, in the config (backup_key): it can
//     write copies for you and never read one with it.
//
// When the server is gone its key is gone with it, and yours opens the
// copies: `moin restore … --key`.

// serverKeyFile is where the private half of the server's key lives, next
// to the config.
func serverKeyFile() string {
	return filepath.Join(filepath.Dir(ConfigFile()), "backup.key")
}

// serverKey reads the server's key.
func serverKey() (*age.X25519Identity, error) {
	body, err := os.ReadFile(serverKeyFile())
	if err != nil {
		return nil, err
	}
	key, err := age.ParseX25519Identity(strings.TrimSpace(string(body)))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", serverKeyFile(), err)
	}
	return key, nil
}

// makeServerKey reads the server's key, and makes it when there is none
// yet: before the first copy. Two commands at once end with the same key.
func makeServerKey() (*age.X25519Identity, error) {
	key, err := serverKey()
	if !errors.Is(err, os.ErrNotExist) {
		return key, err
	}
	key, err = age.GenerateX25519Identity()
	if err != nil {
		return nil, err
	}
	// Written whole under another name first: a link only lands when no
	// other command made the key meanwhile, and then theirs counts.
	fresh := fmt.Sprintf("%s.%d", serverKeyFile(), os.Getpid())
	if err := writeWhole(fresh, []byte(key.String()+"\n")); err != nil {
		return nil, err
	}
	defer os.Remove(fresh)
	if err := os.Link(fresh, serverKeyFile()); err != nil && !errors.Is(err, os.ErrExist) {
		return nil, err
	}
	return serverKey()
}

// encrypt wraps w: what goes in comes out encrypted for the server's key
// and for yours. Closing it ends the copy; it does not close w.
func (b *backups) encrypt(w io.Writer) (io.WriteCloser, error) {
	server, err := makeServerKey()
	if err != nil {
		return nil, fmt.Errorf("the server's backup key: %w", err)
	}
	keys := []age.Recipient{server.Recipient()}
	if yours := b.config().BackupKey; yours != nil {
		key, err := age.ParseX25519Recipient(yours.Public)
		if err != nil {
			return nil, fmt.Errorf("your backup key in the config: %w", err)
		}
		keys = append(keys, key)
	}
	return age.Encrypt(w, keys...)
}

// errWrongKey is a copy that was encrypted for other keys.
var errWrongKey = errors.New("the key does not open this copy")

// decrypt opens a copy as it lies in the bucket: with the server's key, or
// with the key a restore brought. A plain copy, from before every copy was
// encrypted, is read as it is.
func (b *backups) decrypt(copy protocol.Copy, body io.Reader) (io.Reader, error) {
	if copy.Plain {
		return body, nil
	}
	key := b.key
	if key == nil {
		server, err := serverKey()
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("this server has no backup key, so %w", errWrongKey)
		}
		if err != nil {
			return nil, fmt.Errorf("the server's backup key: %w", err)
		}
		key = server
	}
	return DecryptCopy(body, key)
}

// DecryptCopy opens an encrypted copy with key. The CLI opens one on your
// laptop with it, to prove your key.
func DecryptCopy(body io.Reader, key age.Identity) (io.Reader, error) {
	plain, err := age.Decrypt(body, key)
	var wrong *age.NoIdentityMatchError
	if errors.As(err, &wrong) {
		return nil, errWrongKey
	}
	if err != nil {
		return nil, fmt.Errorf("the copy is not encrypted as it should be: %w", err)
	}
	return plain, nil
}
