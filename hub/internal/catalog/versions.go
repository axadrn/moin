package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/axadrn/moin/internal/build"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Version is a release of an app: its image tag, and when it was pushed if
// the registry says.
type Version struct {
	Tag  string    `json:"tag"`
	Date time.Time `json:"date,omitzero"`
	// Digest is the image the tag names, where the registry says; a
	// channel tag like stable shares it with its version.
	Digest string `json:"-"`
}

// UpToChannel drops the releases newer than the one the channel tag, such
// as stable, names: a project that tags its betas like releases keeps them
// out. Without the channel among tags, releases stay as they are.
func UpToChannel(releases, tags []Version, channel string) []Version {
	digest := ""
	for _, tag := range tags {
		if tag.Tag == channel {
			digest = tag.Digest
		}
	}
	if channel == "" || digest == "" {
		return releases
	}
	newest := ""
	for _, release := range releases {
		for _, tag := range tags {
			if tag.Tag == release.Tag && tag.Digest == digest && (newest == "" || Compare(release.Tag, newest) > 0) {
				newest = release.Tag
			}
		}
	}
	if newest == "" {
		return releases
	}
	var kept []Version
	for _, release := range releases {
		if Compare(release.Tag, newest) <= 0 {
			kept = append(kept, release)
		}
	}
	return kept
}

// Releases keeps the tags filter takes and sorts them newest first, by
// their numbers: 1.10.0 comes before 1.9.2.
func Releases(tags []Version, filter *regexp.Regexp) []Version {
	if filter == nil {
		filter = defaultFilter
	}
	var releases []Version
	for _, tag := range tags {
		if filter.MatchString(tag.Tag) && !slices.ContainsFunc(releases, func(v Version) bool { return v.Tag == tag.Tag }) {
			releases = append(releases, tag)
		}
	}
	slices.SortStableFunc(releases, func(a, b Version) int { return Compare(b.Tag, a.Tag) })
	return releases
}

// Compare orders two tags by their numbers, then as text: negative when a
// is older.
func Compare(a, b string) int {
	numbers := func(tag string) []int {
		var parts []int
		for _, field := range strings.FieldsFunc(strings.TrimPrefix(tag, "v"), func(r rune) bool { return r < '0' || r > '9' }) {
			number, err := strconv.Atoi(field)
			if err != nil {
				break
			}
			parts = append(parts, number)
		}
		return parts
	}
	if order := slices.Compare(numbers(a), numbers(b)); order != 0 {
		return order
	}
	return strings.Compare(a, b)
}

// Registry asks Docker Hub and the GitHub Container Registry for the tags
// of an image, anonymously. The URLs are there for tests.
type Registry struct {
	Client  *http.Client
	HubURL  string
	GHCRURL string
}

// NewRegistry asks the real registries.
func NewRegistry() Registry {
	return Registry{Client: &http.Client{Timeout: 20 * time.Second}, HubURL: "https://hub.docker.com", GHCRURL: "https://ghcr.io"}
}

// maxPages bounds how far back Docker Hub is read, 100 tags a page.
const maxPages = 10

// Tags lists the tags of image: ghcr.io/owner/name from GHCR, anything
// else from Docker Hub.
func (r Registry) Tags(ctx context.Context, image string) ([]Version, error) {
	if rest, ok := strings.CutPrefix(image, "ghcr.io/"); ok {
		return r.ghcr(ctx, rest)
	}
	repo := strings.TrimPrefix(image, "docker.io/")
	if host, _, ok := strings.Cut(repo, "/"); ok && strings.ContainsAny(host, ".:") {
		return nil, fmt.Errorf("%s: only Docker Hub and ghcr.io are read", image)
	}
	if !strings.Contains(repo, "/") {
		repo = "library/" + repo
	}
	return r.hub(ctx, repo)
}

func (r Registry) hub(ctx context.Context, repo string) ([]Version, error) {
	next := r.HubURL + "/v2/repositories/" + repo + "/tags?page_size=100&ordering=last_updated"
	var tags []Version
	for page := 0; next != "" && page < maxPages; page++ {
		var body struct {
			Next    string `json:"next"`
			Results []struct {
				Name          string    `json:"name"`
				Digest        string    `json:"digest"`
				LastUpdated   time.Time `json:"last_updated"`
				TagLastPushed time.Time `json:"tag_last_pushed"`
			} `json:"results"`
		}
		err := r.getJSON(ctx, next, "", &body)
		if err != nil {
			return nil, err
		}
		for _, result := range body.Results {
			date := result.TagLastPushed
			if date.IsZero() {
				date = result.LastUpdated
			}
			tags = append(tags, Version{Tag: result.Name, Date: date, Digest: result.Digest})
		}
		next = body.Next
		// Only follow pages on Docker Hub itself.
		if next != "" && !strings.HasPrefix(next, r.HubURL+"/") {
			next = ""
		}
	}
	return tags, nil
}

func (r Registry) ghcr(ctx context.Context, repo string) ([]Version, error) {
	var token struct {
		Token string `json:"token"`
	}
	err := r.getJSON(ctx, r.GHCRURL+"/token?scope="+url.QueryEscape("repository:"+repo+":pull"), "", &token)
	if err != nil {
		return nil, err
	}
	// GHCR answers at most 1000 tags a page and links the next one; an
	// image with many build tags has its releases on a later page.
	var tags []Version
	address := r.GHCRURL + "/v2/" + repo + "/tags/list?n=1000"
	for page := 0; address != "" && page < ghcrPages; page++ {
		var list struct {
			Tags []string `json:"tags"`
		}
		next, err := r.getJSONPage(ctx, address, token.Token, &list)
		if err != nil {
			return nil, err
		}
		for _, tag := range list.Tags {
			tags = append(tags, Version{Tag: tag})
		}
		address = ""
		if next != "" {
			address = r.GHCRURL + next
		}
	}
	return tags, nil
}

// ghcrPages bounds the pages of tags read from GHCR: 50,000 tags.
const ghcrPages = 50

func (r Registry) getJSON(ctx context.Context, address, bearer string, into any) error {
	_, err := r.getJSONPage(ctx, address, bearer, into)
	return err
}

// linkNext is the path of the next page in a Link header, as registries
// send it: </v2/x/tags/list?last=y&n=1000>; rel="next".
var linkNext = regexp.MustCompile(`<(/[^>]+)>;\s*rel="next"`)

// getJSONPage is getJSON that also reports the path of the next page, or
// "" on the last.
func (r Registry) getJSONPage(ctx context.Context, address, bearer string, into any) (string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return "", err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", build.Name+"-catalog")
	if bearer != "" {
		request.Header.Set("Authorization", "Bearer "+bearer)
	}
	response, err := r.Client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%s: %s", request.URL.Host, response.Status)
	}
	err = json.NewDecoder(io.LimitReader(response.Body, 8<<20)).Decode(into)
	if err != nil {
		return "", errors.New(request.URL.Host + ": unreadable answer")
	}
	next := ""
	if match := linkNext.FindStringSubmatch(response.Header.Get("Link")); match != nil {
		next = match[1]
	}
	return next, nil
}
