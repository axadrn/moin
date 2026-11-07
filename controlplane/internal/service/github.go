package service

import (
	"bytes"
	"context"
	"crypto"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// GitHub is the moin GitHub App. Users install it on their account or
// organization; moin then reads the repos they chose, downloads a commit as
// a tarball for a deploy and writes the deploy's commit status.
type GitHub struct {
	appID         string
	slug          string
	key           *rsa.PrivateKey
	webhookSecret string
	// clientID and clientSecret let moin ask GitHub who installed the app.
	// Without them moin trusts the installation ID in the setup link.
	clientID     string
	clientSecret string
	apiURL       string
	webURL       string
	http         *http.Client

	mu     sync.Mutex
	tokens map[int64]installationToken
}

type installationToken struct {
	token   string
	expires time.Time
}

// GitHubRepo is a repo an installation may read.
type GitHubRepo struct {
	FullName      string `json:"full_name"`
	DefaultBranch string `json:"default_branch"`
	Private       bool   `json:"private"`
}

// GitHubCommit is a resolved ref.
type GitHubCommit struct {
	SHA     string
	Message string
}

var (
	ErrGitHubOff      = errors.New("the GitHub App is not set up on this control plane")
	ErrGitHubNotYours = errors.New("your GitHub account cannot manage this installation: install the app from an account you own")
)

// NewGitHub returns nil without an app ID, so the control plane runs
// without GitHub.
func NewGitHub(appID, slug, privateKey, webhookSecret, clientID, clientSecret, apiURL, webURL string) (*GitHub, error) {
	if appID == "" {
		return nil, nil
	}
	block, _ := pem.Decode([]byte(strings.ReplaceAll(privateKey, `\n`, "\n")))
	if block == nil {
		return nil, errors.New("GITHUB_APP_PRIVATE_KEY is not a PEM key")
	}
	key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		parsed, pkcs8Err := x509.ParsePKCS8PrivateKey(block.Bytes)
		rsaKey, ok := parsed.(*rsa.PrivateKey)
		if pkcs8Err != nil || !ok {
			return nil, fmt.Errorf("GITHUB_APP_PRIVATE_KEY: %w", err)
		}
		key = rsaKey
	}
	if apiURL == "" {
		apiURL = "https://api.github.com"
	}
	if webURL == "" {
		webURL = "https://github.com"
	}
	return &GitHub{
		appID:         appID,
		slug:          slug,
		key:           key,
		webhookSecret: webhookSecret,
		clientID:      clientID,
		clientSecret:  clientSecret,
		apiURL:        strings.TrimRight(apiURL, "/"),
		webURL:        strings.TrimRight(webURL, "/"),
		http:          &http.Client{Timeout: 2 * time.Minute},
		tokens:        map[int64]installationToken{},
	}, nil
}

// InstallURL is where a user installs the app; GitHub sends them back to
// the setup URL with the installation ID and state.
func (g *GitHub) InstallURL(state string) string {
	return g.webURL + "/apps/" + g.slug + "/installations/new?state=" + url.QueryEscape(state)
}

// AuthorizeURL is where a user signs in to GitHub with the app. GitHub
// sends them back to the callback URL, the setup URL, with a code and the
// state: no installation ID, so moin looks up their installations itself.
func (g *GitHub) AuthorizeURL(state string) string {
	return g.webURL + "/login/oauth/authorize?client_id=" + url.QueryEscape(g.clientID) + "&state=" + url.QueryEscape(state)
}

// ChecksInstaller says whether moin can ask GitHub who installed the app.
func (g *GitHub) ChecksInstaller() bool {
	return g.clientID != "" && g.clientSecret != ""
}

// UserInstallations lists the installations of the app that a GitHub user
// may manage. GitHub sends a code along after the install or the sign-in;
// moin trades it for a token of that user and asks GitHub. Without this
// check anyone could claim someone else's installation by its ID, which is
// only a counter. A code that is wrong, used or expired finds none.
func (g *GitHub) UserInstallations(ctx context.Context, code string) ([]int64, error) {
	if code == "" {
		return nil, nil
	}
	var grant struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
	}
	form := map[string]string{"client_id": g.clientID, "client_secret": g.clientSecret, "code": code}
	err := g.send(ctx, http.MethodPost, g.webURL+"/login/oauth/access_token", "", form, &grant)
	if err != nil || grant.AccessToken == "" {
		return nil, err
	}
	ids := []int64{}
	for page := 1; page <= 10; page++ {
		var answer struct {
			Installations []struct {
				ID int64 `json:"id"`
			} `json:"installations"`
		}
		err = g.call(ctx, http.MethodGet, "/user/installations?per_page=100&page="+strconv.Itoa(page), "Bearer "+grant.AccessToken, nil, &answer)
		if err != nil {
			return nil, err
		}
		for _, installation := range answer.Installations {
			ids = append(ids, installation.ID)
		}
		if len(answer.Installations) < 100 {
			break
		}
	}
	return ids, nil
}

// jwt signs the app's own token, valid for nine minutes.
func (g *GitHub) jwt() (string, error) {
	now := time.Now()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))
	claims, _ := json.Marshal(map[string]any{"iat": now.Add(-time.Minute).Unix(), "exp": now.Add(9 * time.Minute).Unix(), "iss": g.appID})
	unsigned := header + "." + base64.RawURLEncoding.EncodeToString(claims)
	digest := sha256.Sum256([]byte(unsigned))
	signature, err := rsa.SignPKCS1v15(rand.Reader, g.key, crypto.SHA256, digest[:])
	if err != nil {
		return "", err
	}
	return unsigned + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

// token returns an installation token, cached until shortly before it
// expires.
func (g *GitHub) token(ctx context.Context, installationID int64) (string, error) {
	g.mu.Lock()
	cached, ok := g.tokens[installationID]
	g.mu.Unlock()
	if ok && time.Until(cached.expires) > 5*time.Minute {
		return cached.token, nil
	}
	jwt, err := g.jwt()
	if err != nil {
		return "", err
	}
	var answer struct {
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	err = g.call(ctx, http.MethodPost, "/app/installations/"+strconv.FormatInt(installationID, 10)+"/access_tokens", "Bearer "+jwt, nil, &answer)
	if err != nil {
		return "", err
	}
	g.mu.Lock()
	g.tokens[installationID] = installationToken{token: answer.Token, expires: answer.ExpiresAt}
	g.mu.Unlock()
	return answer.Token, nil
}

// Installation returns the account an installation belongs to and the page
// on github.com where its owner chooses the repos.
func (g *GitHub) Installation(ctx context.Context, installationID int64) (account, settingsURL string, err error) {
	jwt, err := g.jwt()
	if err != nil {
		return "", "", err
	}
	var answer struct {
		Account struct {
			Login string `json:"login"`
		} `json:"account"`
		HTMLURL string `json:"html_url"`
	}
	err = g.call(ctx, http.MethodGet, "/app/installations/"+strconv.FormatInt(installationID, 10), "Bearer "+jwt, nil, &answer)
	return answer.Account.Login, answer.HTMLURL, err
}

// Branches lists the branch names of a repo.
func (g *GitHub) Branches(ctx context.Context, installationID int64, repo string) ([]string, error) {
	token, err := g.token(ctx, installationID)
	if err != nil {
		return nil, err
	}
	branches := []string{}
	for page := 1; page <= 10; page++ {
		var answer []struct {
			Name string `json:"name"`
		}
		err = g.call(ctx, http.MethodGet, "/repos/"+repo+"/branches?per_page=100&page="+strconv.Itoa(page), "token "+token, nil, &answer)
		if err != nil {
			return nil, err
		}
		for _, branch := range answer {
			branches = append(branches, branch.Name)
		}
		if len(answer) < 100 {
			break
		}
	}
	return branches, nil
}

// Repos lists the repos an installation may read.
func (g *GitHub) Repos(ctx context.Context, installationID int64) ([]GitHubRepo, error) {
	token, err := g.token(ctx, installationID)
	if err != nil {
		return nil, err
	}
	repos := []GitHubRepo{}
	for page := 1; page <= 10; page++ {
		var answer struct {
			Repositories []GitHubRepo `json:"repositories"`
		}
		err = g.call(ctx, http.MethodGet, "/installation/repositories?per_page=100&page="+strconv.Itoa(page), "token "+token, nil, &answer)
		if err != nil {
			return nil, err
		}
		repos = append(repos, answer.Repositories...)
		if len(answer.Repositories) < 100 {
			break
		}
	}
	return repos, nil
}

// Commit resolves a branch, tag or SHA to a commit.
func (g *GitHub) Commit(ctx context.Context, installationID int64, repo, ref string) (GitHubCommit, error) {
	token, err := g.token(ctx, installationID)
	if err != nil {
		return GitHubCommit{}, err
	}
	var answer struct {
		SHA    string `json:"sha"`
		Commit struct {
			Message string `json:"message"`
		} `json:"commit"`
	}
	err = g.call(ctx, http.MethodGet, "/repos/"+repo+"/commits/"+url.PathEscape(ref), "token "+token, nil, &answer)
	if err != nil {
		return GitHubCommit{}, err
	}
	message, _, _ := strings.Cut(answer.Commit.Message, "\n")
	return GitHubCommit{SHA: answer.SHA, Message: message}, nil
}

// Tarball is where the daemon downloads a commit itself, with the
// authorization to send: the code goes from GitHub to the server and never
// through moin. The token only reads this one repo and expires within the
// hour, so a server never holds access to the other repos of the
// installation.
func (g *GitHub) Tarball(ctx context.Context, installationID int64, repo, sha string) (url, authorization string, err error) {
	jwt, err := g.jwt()
	if err != nil {
		return "", "", err
	}
	_, name, _ := strings.Cut(repo, "/")
	scope := map[string]any{"repositories": []string{name}, "permissions": map[string]string{"contents": "read"}}
	var answer struct {
		Token string `json:"token"`
	}
	err = g.call(ctx, http.MethodPost, "/app/installations/"+strconv.FormatInt(installationID, 10)+"/access_tokens", "Bearer "+jwt, scope, &answer)
	if err != nil {
		return "", "", err
	}
	return g.apiURL + "/repos/" + repo + "/tarball/" + sha, "token " + answer.Token, nil
}

// SetStatus writes the moin commit status of a deploy.
func (g *GitHub) SetStatus(ctx context.Context, installationID int64, repo, sha, state, targetURL, description string) error {
	token, err := g.token(ctx, installationID)
	if err != nil {
		return err
	}
	body := map[string]string{"state": state, "target_url": targetURL, "description": clipText(description, 140), "context": "moin/deploy"}
	return g.call(ctx, http.MethodPost, "/repos/"+repo+"/statuses/"+sha, "token "+token, body, nil)
}

// VerifyWebhook checks the X-Hub-Signature-256 of a webhook body.
func (g *GitHub) VerifyWebhook(body []byte, signature string) bool {
	given, ok := strings.CutPrefix(signature, "sha256=")
	if !ok || g.webhookSecret == "" {
		return false
	}
	mac := hmac.New(sha256.New, []byte(g.webhookSecret))
	mac.Write(body)
	want := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(given), []byte(want))
}

// call calls the GitHub API.
func (g *GitHub) call(ctx context.Context, method, path, authorization string, body, target any) error {
	return g.send(ctx, method, g.apiURL+path, authorization, body, target)
}

// send sends body as JSON to a GitHub URL and decodes the answer into
// target.
func (g *GitHub) send(ctx context.Context, method, url, authorization string, body, target any) error {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, url, reader)
	if err != nil {
		return err
	}
	if authorization != "" {
		request.Header.Set("Authorization", authorization)
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := g.http.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode >= 300 {
		message, _ := io.ReadAll(io.LimitReader(response.Body, 512))
		return &GitHubError{Status: response.StatusCode, Message: strings.TrimSpace(string(message))}
	}
	if target == nil {
		return nil
	}
	return json.NewDecoder(response.Body).Decode(target)
}

// GitHubError is an answer of the GitHub API that is not a success.
type GitHubError struct {
	Status  int
	Message string
}

func (e *GitHubError) Error() string {
	return fmt.Sprintf("github: %d %s", e.Status, e.Message)
}
