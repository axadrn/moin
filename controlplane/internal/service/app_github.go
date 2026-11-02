package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/axadrn/ops/protocol"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/axadrn/ops/controlplane/internal/model"
)

var (
	ErrGitHubState    = errors.New("the GitHub install link expired or belongs to another account: connect again")
	ErrRepo           = errors.New("ops cannot read this repo: install the ops GitHub App on its account and give it the repo")
	ErrRepoName       = errors.New("name the repo as owner/name")
	ErrNoRepo         = errors.New("the app follows no repo: link one first")
	ErrRef            = errors.New("GitHub knows no branch, tag or commit by this name")
	repoPattern       = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,100}/[A-Za-z0-9_.-]{1,100}$`)
	githubStateExpiry = time.Hour
)

// SetGitHub turns on the GitHub App. Without it the GitHub calls answer
// ErrGitHubOff.
func (s *AppService) SetGitHub(github *GitHub) { s.github = github }

// GitHubRepoOf is a repo with the installation that may read it.
type GitHubRepoOf struct {
	GitHubRepo
	InstallationID int64
	Account        string
}

// GitHubInstallURL is the link that installs the app for the workspace. Its
// state names the workspace and the member, because GitHub sends them back
// to one setup URL for every workspace.
func (s *AppService) GitHubInstallURL(actor Actor) (string, error) {
	if s.github == nil {
		return "", ErrGitHubOff
	}
	state := s.box.Seal(actor.WorkspaceID+"|"+actor.UserID+"|"+strconv.FormatInt(s.now().Add(githubStateExpiry).Unix(), 10), "github-state")
	return s.github.InstallURL(state), nil
}

// ConnectGitHub stores the installation GitHub sent the member back with,
// in the workspace the state names. actor has no workspace yet.
func (s *AppService) ConnectGitHub(ctx context.Context, actor Actor, installationID int64, state string) (string, error) {
	if s.github == nil {
		return "", ErrGitHubOff
	}
	opened, err := s.box.Open(state, "github-state")
	parts := strings.Split(opened, "|")
	if err != nil || len(parts) != 3 || parts[1] != actor.UserID {
		return "", ErrGitHubState
	}
	deadline, _ := strconv.ParseInt(parts[2], 10, 64)
	if s.now().Unix() > deadline {
		return "", ErrGitHubState
	}
	actor.WorkspaceID = parts[0]
	account, err := s.github.Installation(ctx, installationID)
	if err != nil {
		return "", err
	}
	err = s.apps.AddGitHubInstallation(model.GitHubInstallation{InstallationID: installationID, WorkspaceID: actor.WorkspaceID, Account: account, CreatedAt: s.now().UTC()})
	if err != nil {
		return "", err
	}
	s.servers.record(actor, "github.connected", "github", account, nil)
	s.notify(actor.WorkspaceID)
	return account, nil
}

// GitHubInstallations lists where the user installed the app.
func (s *AppService) GitHubInstallations(workspaceID string) ([]model.GitHubInstallation, error) {
	return s.apps.GitHubInstallations(workspaceID)
}

// GitHubRepos lists every repo the user's installations may read.
func (s *AppService) GitHubRepos(ctx context.Context, workspaceID string) ([]GitHubRepoOf, error) {
	if s.github == nil {
		return nil, ErrGitHubOff
	}
	installations, err := s.apps.GitHubInstallations(workspaceID)
	if err != nil {
		return nil, err
	}
	repos := []GitHubRepoOf{}
	for _, installation := range installations {
		found, err := s.github.Repos(ctx, installation.InstallationID)
		if err != nil {
			slog.Warn("list github repos failed", "installation_id", installation.InstallationID, "error", err)
			continue
		}
		for _, repo := range found {
			repos = append(repos, GitHubRepoOf{GitHubRepo: repo, InstallationID: installation.InstallationID, Account: installation.Account})
		}
	}
	return repos, nil
}

// LinkRepo makes an app follow a repo and branch. An empty branch is the
// repo's default branch.
func (s *AppService) LinkRepo(ctx context.Context, actor Actor, idOrName, repo, branch string, autoDeploy bool) (*model.App, error) {
	app, err := s.apps.ByIDOrNameForWorkspace(idOrName, actor.WorkspaceID)
	if err != nil {
		return nil, err
	}
	repo = strings.Trim(strings.TrimPrefix(strings.TrimSpace(repo), "https://github.com/"), "/")
	repo = strings.TrimSuffix(repo, ".git")
	if !repoPattern.MatchString(repo) {
		return nil, ErrRepoName
	}
	repos, err := s.GitHubRepos(ctx, actor.WorkspaceID)
	if err != nil {
		return nil, err
	}
	for _, found := range repos {
		if !strings.EqualFold(found.FullName, repo) {
			continue
		}
		branch = strings.TrimSpace(branch)
		if branch == "" {
			branch = found.DefaultBranch
		}
		err = s.apps.SetRepo(app.ID, found.FullName, branch, autoDeploy, found.InstallationID)
		if err != nil {
			return nil, err
		}
		s.servers.record(actor, "repo.linked", "app", app.Name, map[string]string{"repo": found.FullName, "branch": branch, "auto_deploy": strconv.FormatBool(autoDeploy)})
		s.notify(actor.WorkspaceID)
		return s.apps.ByID(app.ID)
	}
	return nil, ErrRepo
}

// UnlinkRepo stops an app from following its repo.
func (s *AppService) UnlinkRepo(actor Actor, idOrName string) error {
	app, err := s.apps.ByIDOrNameForWorkspace(idOrName, actor.WorkspaceID)
	if err != nil {
		return err
	}
	err = s.apps.SetRepo(app.ID, "", "", false, 0)
	if err != nil {
		return err
	}
	s.servers.record(actor, "repo.unlinked", "app", app.Name, map[string]string{"repo": app.Repo})
	s.notify(actor.WorkspaceID)
	return nil
}

// DeployRef deploys a branch, tag or commit of the app's repo, downloaded
// from GitHub. An empty ref is the linked branch.
func (s *AppService) DeployRef(ctx context.Context, actor Actor, idOrName, ref string) (*model.Deploy, error) {
	if s.github == nil {
		return nil, ErrGitHubOff
	}
	app, err := s.ready(actor.WorkspaceID, idOrName)
	if err != nil {
		return nil, err
	}
	if app.Repo == "" {
		return nil, ErrNoRepo
	}
	ref = strings.TrimSpace(ref)
	if ref == "" {
		ref = app.Branch
	}
	commit, err := s.github.Commit(ctx, app.InstallationID, app.Repo, ref)
	var githubErr *GitHubError
	if errors.As(err, &githubErr) && (githubErr.Status == 404 || githubErr.Status == 422) {
		return nil, ErrRef
	}
	if err != nil {
		return nil, err
	}
	url, authorization, err := s.github.Tarball(ctx, app.InstallationID, app.Repo, commit.SHA)
	if err != nil {
		return nil, err
	}
	deploy := s.newDeploy(app)
	deploy.SourceKey, deploy.Ref, deploy.CommitSHA, deploy.CommitMessage = "server", ref, commit.SHA, clipText(commit.Message, 200)
	deploy, err = s.start(actor, "app.deployed", app, deploy, protocol.DeployStart{SourceURL: url, SourceAuthorization: authorization, SourceStripTop: true})
	if err != nil {
		return nil, err
	}
	go s.reportStatus(app, deploy, "pending", "Deploying v"+strconv.Itoa(deploy.Number))
	return deploy, nil
}

// GitHubPush deploys a push to every app that follows the branch with auto
// deploy on.
func (s *AppService) GitHubPush(ctx context.Context, installationID int64, repo, branch, sha string) {
	apps, err := s.apps.AppsForPush(installationID, repo, branch)
	if err != nil {
		slog.Error("find apps for push failed", "repo", repo, "error", err)
		return
	}
	for _, app := range apps {
		if err := s.servers.RequireChanges(app.WorkspaceID); err != nil {
			slog.Info("auto deploy skipped", "app_id", app.ID, "repo", repo, "error", err)
			continue
		}
		deploy, err := s.DeployRef(ctx, Actor{WorkspaceID: app.WorkspaceID, Source: Source{Via: ViaGitHub}}, app.ID, sha)
		if err != nil {
			slog.Warn("auto deploy failed", "app_id", app.ID, "repo", repo, "sha", sha, "error", err)
			continue
		}
		slog.Info("auto deploy", "app_id", app.ID, "repo", repo, "branch", branch, "number", deploy.Number)
	}
}

// GitHubUninstalled forgets an installation and unlinks its apps.
func (s *AppService) GitHubUninstalled(installationID int64) error {
	installation, apps, err := s.apps.GitHubInstallation(installationID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	err = s.apps.DeleteGitHubInstallation(installationID)
	if err != nil || installation == nil {
		return err
	}
	actor := Actor{WorkspaceID: installation.WorkspaceID, Source: Source{Via: ViaGitHub}}
	s.servers.record(actor, "github.disconnected", "github", installation.Account, nil)
	for _, app := range apps {
		s.servers.record(actor, "repo.unlinked", "app", app.Name, map[string]string{"repo": app.Repo, "branch": app.Branch})
	}
	s.notify(installation.WorkspaceID)
	return nil
}

// VerifyGitHubWebhook checks the signature of a webhook body.
func (s *AppService) VerifyGitHubWebhook(body []byte, signature string) bool {
	return s.github != nil && s.github.VerifyWebhook(body, signature)
}

// reportStatus writes the commit status of a deploy that came from GitHub.
func (s *AppService) reportStatus(app *model.App, deploy *model.Deploy, state, description string) {
	if s.github == nil || deploy.CommitSHA == "" || app.InstallationID == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	target := fmt.Sprintf("%s/app/%s/apps/%s", s.servers.appURL, app.WorkspaceSlug, app.Name)
	err := s.github.SetStatus(ctx, app.InstallationID, app.Repo, deploy.CommitSHA, state, target, description)
	if err != nil {
		slog.Warn("github commit status failed", "app_id", app.ID, "sha", deploy.CommitSHA, "error", err)
	}
}
