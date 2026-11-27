// Package githubrepo knows what GitHub says about the moin repo: its
// stars, and how often the files of its releases were downloaded. It asks
// GitHub at most once an hour, without a token: GitHub allows 60 such
// calls an hour per address. Pages never wait for it.
package githubrepo

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

// Repo is the moin repo on GitHub.
const Repo = "axadrn/moin"

// Numbers are what GitHub said about the repo. Zero is not known yet.
type Numbers struct {
	Stars     int
	Downloads int64
}

var (
	// apiURL is where the numbers come from; tests point it at a fake.
	apiURL = "https://api.github.com"
	client = &http.Client{Timeout: 5 * time.Second}

	mu      sync.Mutex
	numbers Numbers
	checked time.Time
	asking  bool
	// failing says the last answer failed, so it is logged only once.
	failing bool
)

// Now returns what GitHub last said: zero before the first answer.
// Once the answer is an hour old it asks again in the background.
func Now() Numbers {
	mu.Lock()
	defer mu.Unlock()
	if !asking && time.Since(checked) > time.Hour {
		asking = true
		go refresh()
	}
	return numbers
}

// refresh asks GitHub. A failed answer keeps the numbers it had and is
// logged once, until an answer comes again: a dev server that restarts
// often uses up the 60 calls of an hour.
func refresh() {
	answer, err := fetch()
	mu.Lock()
	defer mu.Unlock()
	asking = false
	checked = time.Now()
	switch {
	case err == nil:
		numbers, failing = answer, false
	case !failing:
		failing = true
		slog.Warn("github numbers", "error", err)
	}
}

// fetch asks for the repo's stars, then for its releases, a hundred a
// page, and adds up the downloads of every file of every release.
func fetch() (Numbers, error) {
	var repo struct {
		Stars int `json:"stargazers_count"`
	}
	if err := get("/repos/"+Repo, &repo); err != nil {
		return Numbers{}, err
	}
	answer := Numbers{Stars: repo.Stars}
	for page := 1; ; page++ {
		var releases []struct {
			Assets []struct {
				Downloads int64 `json:"download_count"`
			} `json:"assets"`
		}
		if err := get(fmt.Sprintf("/repos/%s/releases?per_page=100&page=%d", Repo, page), &releases); err != nil {
			return Numbers{}, err
		}
		for _, release := range releases {
			for _, asset := range release.Assets {
				answer.Downloads += asset.Downloads
			}
		}
		if len(releases) < 100 {
			return answer, nil
		}
	}
}

// get reads one answer of the GitHub API into into.
func get(path string, into any) error {
	request, err := http.NewRequest(http.MethodGet, apiURL+path, nil)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("github: %s", response.Status)
	}
	return json.NewDecoder(response.Body).Decode(into)
}

// Stars are the stars as the site shows them, everywhere: the count the
// way GitHub writes it, "" before GitHub answered or when it failed.
func Stars() string {
	if stars := Now().Stars; stars > 0 {
		return Short(stars)
	}
	return ""
}

// Short writes a count the way GitHub does: 950, 1.2k, 12k.
func Short(stars int) string {
	switch {
	case stars < 1000:
		return fmt.Sprint(stars)
	case stars < 10000:
		return fmt.Sprintf("%.1fk", float64(stars)/1000)
	default:
		return fmt.Sprintf("%dk", stars/1000)
	}
}
