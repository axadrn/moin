package daemon

import (
	"bytes"
	"cmp"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/axadrn/moin/internal/build"
)

// A server pulls images from a registry with Docker's own logins: `moin
// registry set` is `docker login` as root, `moin registry rm` is `docker
// logout`. They live where Docker keeps them, in root's config.json, and
// nowhere else. A deploy, a build and a follow all read them there.

// dockerHub is how moin names Docker Hub, which Docker keeps under hubKey.
const (
	dockerHub = "docker.io"
	hubKey    = "https://index.docker.io/v1/"
)

// dockerConfigDir is the folder of root's Docker config.
func dockerConfigDir() string {
	return cmp.Or(os.Getenv("DOCKER_CONFIG"), "/root/.docker")
}

// Registry is a login of this server at a registry. Token only comes in,
// with `moin registry set`; it is never answered.
type Registry struct {
	Registry string `json:"registry"`
	User     string `json:"user"`
	Token    string `json:"token,omitempty"`
}

// registryName is a registry's host, with a port when it has one.
var registryName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.-]*(:[0-9]+)?$`)

// RegistriesHere lists the registries this server is logged in to.
func RegistriesHere() ([]Registry, error) {
	list := []Registry{}
	body, err := os.ReadFile(filepath.Join(dockerConfigDir(), "config.json"))
	if errors.Is(err, os.ErrNotExist) {
		return list, nil
	}
	if err != nil {
		return nil, err
	}
	var config struct {
		Auths map[string]struct {
			Auth string `json:"auth"`
		} `json:"auths"`
	}
	if err := json.Unmarshal(body, &config); err != nil {
		return nil, fmt.Errorf("read Docker's config.json: %w", err)
	}
	for key, login := range config.Auths {
		// auth is user:token in base64. Docker leaves it empty when a
		// credential helper keeps the login.
		decoded, _ := base64.StdEncoding.DecodeString(login.Auth)
		user, _, _ := strings.Cut(string(decoded), ":")
		list = append(list, Registry{Registry: registryOf(key), User: user})
	}
	slices.SortFunc(list, func(a, b Registry) int { return cmp.Compare(a.Registry, b.Registry) })
	return list, nil
}

// registryOf is the registry of a key in Docker's config: the host, and
// docker.io for Docker Hub.
func registryOf(key string) string {
	if key == hubKey {
		return dockerHub
	}
	key = strings.TrimPrefix(strings.TrimPrefix(key, "https://"), "http://")
	host, _, _ := strings.Cut(key, "/")
	return host
}

// SetRegistryHere logs this server in to a registry. Docker asks the
// registry, so a wrong token fails here and nothing is kept.
func SetRegistryHere(ctx context.Context, login Registry) error {
	if !registryName.MatchString(login.Registry) {
		return fmt.Errorf("%q is not a registry: its host, like ghcr.io", login.Registry)
	}
	if login.User == "" || login.Token == "" {
		return errors.New("a login needs a user and a token")
	}
	args := []string{"login", "--username=" + login.User, "--password-stdin"}
	if login.Registry != dockerHub {
		args = append(args, login.Registry)
	}
	_, err := registryDocker(ctx, strings.NewReader(login.Token), args...)
	if err != nil {
		return fmt.Errorf("the login was refused by %s: %w", login.Registry, err)
	}
	return nil
}

// RemoveRegistryHere logs this server out of a registry.
func RemoveRegistryHere(ctx context.Context, registry string) error {
	list, err := RegistriesHere()
	if err != nil {
		return err
	}
	if !slices.ContainsFunc(list, func(each Registry) bool { return each.Registry == registry }) {
		return fmt.Errorf("this server has no login for %s: see them with `%s registry <server>`", registry, build.Name)
	}
	args := []string{"logout"}
	if registry != dockerHub {
		args = append(args, registry)
	}
	_, err = registryDocker(ctx, nil, args...)
	return err
}

// Pulled is an image the server pulled, with its ID.
type Pulled struct {
	Image string `json:"image"`
	ID    string `json:"id"`
}

// PullHere pulls image from its registry, with the server's login for it.
func PullHere(ctx context.Context, image string) (Pulled, error) {
	id, err := pullImage(ctx, image)
	return Pulled{Image: image, ID: id}, err
}

// pullImage pulls image and returns its ID, like sha256:3f2a…. An image
// the server has already costs one question to the registry.
func pullImage(ctx context.Context, image string) (string, error) {
	if !validImage(image) {
		return "", fmt.Errorf("%q is not an image", image)
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	if _, err := registryDocker(ctx, nil, "pull", "--quiet", "--", image); err != nil {
		return "", err
	}
	return imageID(ctx, image)
}

// imageID is Docker's ID of an image on this server.
func imageID(ctx context.Context, image string) (string, error) {
	id, err := registryDocker(ctx, nil, "image", "inspect", "--format", "{{.Id}}", "--", image)
	return strings.TrimSpace(id), err
}

// validImage reports whether image can name an image, and is no option.
func validImage(image string) bool {
	return image != "" && !strings.HasPrefix(image, "-") && !strings.ContainsAny(image, " \t\n")
}

// registryDocker runs docker with root's Docker config and returns what it
// printed. A failure is what docker said, on one line.
func registryDocker(ctx context.Context, stdin io.Reader, args ...string) (string, error) {
	command := exec.CommandContext(ctx, "docker", args...)
	command.Env = append(os.Environ(), "DOCKER_CONFIG="+dockerConfigDir())
	command.Stdin = stdin
	var stderr bytes.Buffer
	command.Stderr = &stderr
	out, err := command.Output()
	if err != nil {
		return "", errors.New(strings.Join(strings.Fields(cmp.Or(stderr.String(), err.Error())), " "))
	}
	return string(out), nil
}

// shortID is the first 12 characters of an image ID, as docker shows it.
func shortID(id string) string {
	id = strings.TrimPrefix(id, "sha256:")
	return id[:min(len(id), 12)]
}
