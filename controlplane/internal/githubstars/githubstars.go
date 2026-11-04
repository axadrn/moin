// Package githubstars knows how many stars the moin repo has on GitHub. It
// asks GitHub at most once an hour, without a token: GitHub allows 60
// such calls an hour per address. Pages never wait for it.
package githubstars

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// Repo is the repo whose stars the navbar shows.
const Repo = "axadrn/ops"

var (
	// apiURL is where the stars come from; tests point it at a fake.
	apiURL = "https://api.github.com"
	client = &http.Client{Timeout: 5 * time.Second}

	mu      sync.Mutex
	count   int
	checked time.Time
	asking  bool
)

// Count returns the stars GitHub last reported, 0 before the first answer or
// while the repo is private. Once the answer is an hour old it asks again in
// the background.
func Count() int {
	mu.Lock()
	defer mu.Unlock()
	if !asking && time.Since(checked) > time.Hour {
		asking = true
		go refresh()
	}
	return count
}

func refresh() {
	stars, err := fetch()
	mu.Lock()
	defer mu.Unlock()
	asking = false
	checked = time.Now()
	if err == nil {
		count = stars
	}
}

func fetch() (int, error) {
	request, err := http.NewRequest(http.MethodGet, apiURL+"/repos/"+Repo, nil)
	if err != nil {
		return 0, err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	response, err := client.Do(request)
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("github: %s", response.Status)
	}
	var repo struct {
		Stars int `json:"stargazers_count"`
	}
	err = json.NewDecoder(response.Body).Decode(&repo)
	return repo.Stars, err
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
