package daemon

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/axadrn/moin/internal/build"
	"github.com/axadrn/moin/protocol"
)

// DefaultRulesFile is where the owner of a server writes what the daemon
// may do there. MOIN_RULES overrides it. The daemon never writes this file.
const DefaultRulesFile = "/etc/" + build.Name + "/rules"

// RulesFile returns MOIN_RULES, else DefaultRulesFile.
func RulesFile() string {
	if file := build.Getenv("RULES"); file != "" {
		return file
	}
	return DefaultRulesFile
}

// LoadRules reads the rules file. Without one, moin may do everything. A file
// with a mistake is an error, so a typo never allows what it meant to stop.
func LoadRules(file string) (*protocol.Rules, error) {
	body, err := os.ReadFile(file)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return parseRules(string(body), file)
}

func parseRules(text, file string) (*protocol.Rules, error) {
	rules := &protocol.Rules{}
	for number, line := range strings.Split(text, "\n") {
		line, _, _ = strings.Cut(line, "#")
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		wrong := func(want string) error {
			return fmt.Errorf("%s line %d: %q: %s", file, number+1, line, want)
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			return nil, wrong("write it as key: value")
		}
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		switch key {
		case "data", "logs", "exec":
			if value != "on" && value != "off" {
				return nil, wrong(key + " is on or off")
			}
			switch key {
			case "data":
				rules.DataOff = value == "off"
			case "logs":
				rules.LogsOff = value == "off"
			default:
				rules.ExecOff = value == "off"
			}
		case "server-access":
			if value != "on" && value != "off" {
				return nil, wrong("server-access is on or off")
			}
			rules.ServerAccess = value == "on"
		case "network":
			if value != "open" && value != "locked" {
				return nil, wrong("network is open or locked")
			}
			rules.NetworkLocked = value == "locked"
		case "deploy-from":
			for _, source := range strings.Split(value, ",") {
				source = strings.TrimSpace(source)
				if _, err := path.Match(source, ""); err != nil || source == "" {
					return nil, wrong("deploy-from lists sources like github.com/acme/*, ghcr.io/acme/* or catalog")
				}
				rules.DeployFrom = append(rules.DeployFrom, source)
			}
		case "mounts":
			for _, folder := range strings.Split(value, ",") {
				folder = strings.TrimSpace(folder)
				if !filepath.IsAbs(folder) || filepath.Clean(folder) != folder {
					return nil, wrong("mounts lists folders of the server like /mnt/media, /srv/photos")
				}
				rules.Mounts = append(rules.Mounts, folder)
			}
		case "backups-to":
			host, bucket, ok := strings.Cut(value, "/")
			if !ok || host == "" || bucket == "" || strings.Contains(bucket, "/") {
				return nil, wrong("backups-to is <endpoint host>/<bucket>, such as s3.eu-central-1.amazonaws.com/acme-backups")
			}
			rules.BackupsTo = value
		default:
			return nil, wrong("unknown rule; the rules are data, logs, exec, deploy-from, backups-to, network, mounts and server-access")
		}
	}
	return rules, nil
}

// refusedByRules starts the answer to a request the rules do not allow.
const refusedByRules = "refused by the rules of this server"

// refused is the answer to a request the rules do not allow.
func refused(file, what string) error {
	return fmt.Errorf("%s (%s): %s", refusedByRules, file, what)
}

// checkDeploy allows a deploy whose source the rules allow. A redeploy or a
// rollback brings no new code and is always allowed.
func checkDeploy(rules *protocol.Rules, file string, start protocol.DeployStart) error {
	if rules == nil || len(rules.DeployFrom) == 0 {
		return nil
	}
	var sources []string
	if start.Image != "" {
		sources = append(sources, imageName(start.Image))
	}
	if start.Catalog != "" {
		sources = append(sources, "catalog")
	}
	if start.SourceURL != "" {
		source, err := sourceName(start.SourceURL)
		if err != nil {
			return refused(file, err.Error())
		}
		sources = append(sources, source)
	}
	for _, source := range sources {
		if !allowed(rules.DeployFrom, source) {
			return refused(file, fmt.Sprintf("deploy-from does not list %s", source))
		}
	}
	return nil
}

func allowed(patterns []string, source string) bool {
	for _, pattern := range patterns {
		if ok, _ := path.Match(pattern, source); ok {
			return true
		}
	}
	return false
}

// imageName is an image reference without its tag or digest.
func imageName(image string) string {
	image, _, _ = strings.Cut(image, "@")
	slash := strings.LastIndex(image, "/")
	if colon := strings.LastIndex(image, ":"); colon > slash {
		image = image[:colon]
	}
	return image
}

// githubAPI is the one place a source comes from; tests replace it.
var githubAPI = "https://api.github.com"

// sourceName names the GitHub repo a source download comes from, as
// github.com/<owner>/<repo>. A source is only ever the tarball of a commit,
// over https from api.github.com: anything else is refused.
func sourceName(raw string) (string, error) {
	link, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("the source address %q is not a URL", raw)
	}
	from := link.Scheme + "://" + link.Host
	parts := strings.Split(strings.Trim(link.Path, "/"), "/")
	if from == githubAPI && len(parts) == 5 && parts[0] == "repos" && parts[3] == "tarball" {
		return "github.com/" + parts[1] + "/" + parts[2], nil
	}
	return "", fmt.Errorf("a source comes only from a GitHub repo, over https from api.github.com, not from %s", from)
}

// checkBucket allows a bucket the rules allow.
func checkBucket(rules *protocol.Rules, file string, bucket *protocol.Bucket) error {
	if rules == nil || rules.BackupsTo == "" || bucket == nil {
		return nil
	}
	if bucketPlace(bucket) != rules.BackupsTo {
		return refused(file, fmt.Sprintf("backups-to allows only %s", rules.BackupsTo))
	}
	return nil
}

// bucketPlace is where a bucket is, as backups-to names it: <endpoint
// host>/<bucket>, "" when its endpoint is no URL.
func bucketPlace(bucket *protocol.Bucket) string {
	host := "s3." + bucket.Region + ".amazonaws.com"
	if bucket.Endpoint != "" {
		endpoint, err := url.Parse(bucket.Endpoint)
		if err != nil {
			return ""
		}
		host = endpoint.Host
	}
	return host + "/" + bucket.Bucket
}
