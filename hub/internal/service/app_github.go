package service

import (
	"context"
	"database/sql"
	"errors"
	"github.com/axadrn/moin/protocol"
	"log/slog"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/axadrn/moin/hub/internal/model"
	"github.com/axadrn/moin/hub/internal/repository"
)

var (
	ErrGitHubState    = errors.New("the GitHub install link expired or belongs to another account: connect again")
	ErrRepo           = errors.New("moin cannot read this repo: install the moin GitHub App on its account and give it the repo")
	ErrRepoName       = errors.New("name the repo as owner/name")
	ErrNoRepo         = errors.New("the app follows no repo: link one first")
	ErrRef            = errors.New("GitHub knows no branch, tag or commit by this name")
	ErrRefName        = errors.New("this is not a valid branch, tag or commit name")
	ErrBranch         = errors.New("the repo on GitHub has no branch by this name")
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

// GitHubInstallURL is the link that installs the app for the workspace,
// on any account or organization. Its state names the workspace and the
// member, because GitHub sends them back to one setup URL for every
// workspace.
func (s *AppService) GitHubInstallURL(actor Actor) (string, error) {
	if s.github == nil {
		return "", ErrGitHubOff
	}
	return s.github.InstallURL(s.gitHubState(actor)), nil
}

// GitHubConnectURL is the link behind "Connect GitHub". The install link
// alone fails for an app that is already installed: GitHub then shows the
// installation's settings and never sends the member back. So with the
// app's OAuth set up, the member signs in to GitHub first, and
// ConnectGitHub connects the installations they already have or sends them
// on to install the app.
func (s *AppService) GitHubConnectURL(actor Actor) (string, error) {
	if s.github == nil {
		return "", ErrGitHubOff
	}
	if !s.github.ChecksInstaller() {
		return s.github.InstallURL(s.gitHubState(actor)), nil
	}
	return s.github.AuthorizeURL(s.gitHubState(actor)), nil
}

func (s *AppService) gitHubState(actor Actor) string {
	return s.box.Seal(actor.WorkspaceID+"|"+actor.UserID+"|"+strconv.FormatInt(s.now().Add(githubStateExpiry).Unix(), 10), "github-state")
}

// ConnectGitHub takes the member back from GitHub and connects
// installations to the workspace the state names. actor has no workspace
// yet. It returns a link on github.com when the member has to go on there:
//
//   - With an installation ID and a code (after an install), it connects
//     that installation if the member's GitHub account may manage it.
//   - Without a code (after a change of an installed app's repos, or an
//     install without sign-in), it sends the member to sign in to GitHub,
//     which comes back with a code.
//   - With a code alone (after the sign-in), it connects every installation
//     the member may manage that no other workspace holds, and sends the
//     member to install the app if there is none.
//
// Without the app's OAuth, moin trusts the installation ID GitHub sent.
func (s *AppService) ConnectGitHub(ctx context.Context, actor Actor, installationID int64, state, code string) (string, error) {
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
	if !s.github.ChecksInstaller() {
		if installationID == 0 {
			return s.github.InstallURL(state), nil
		}
		return "", s.addGitHubInstallation(ctx, actor, installationID)
	}
	if code == "" {
		return s.github.AuthorizeURL(state), nil
	}
	ids, err := s.github.UserInstallations(ctx, code)
	if err != nil {
		return "", err
	}
	if installationID != 0 {
		if !slices.Contains(ids, installationID) {
			return "", ErrGitHubNotYours
		}
		return "", s.addGitHubInstallation(ctx, actor, installationID)
	}
	connected, taken := false, false
	for _, id := range ids {
		err = s.addGitHubInstallation(ctx, actor, id)
		switch {
		case err == nil:
			connected = true
		case errors.Is(err, repository.ErrGitHubInstallationTaken):
			taken = true
		default:
			return "", err
		}
	}
	if !connected && taken {
		// Every installation the member has is another workspace's.
		return "", repository.ErrGitHubInstallationTaken
	}
	if !connected {
		return s.github.InstallURL(state), nil
	}
	return "", nil
}

// addGitHubInstallation stores an installation in the actor's workspace.
func (s *AppService) addGitHubInstallation(ctx context.Context, actor Actor, installationID int64) error {
	account, settingsURL, err := s.github.Installation(ctx, installationID)
	if err != nil {
		return err
	}
	err = s.apps.AddGitHubInstallation(model.GitHubInstallation{InstallationID: installationID, WorkspaceID: actor.WorkspaceID, Account: account, CreatedAt: s.now().UTC(), SettingsURL: settingsURL})
	if err != nil {
		return err
	}
	s.servers.record(actor, "github.connected", "github", account, nil)
	s.notify(actor.WorkspaceID)
	return nil
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

// GitHubBranches lists the branches of a repo the workspace's installations
// may read, with its default branch first.
func (s *AppService) GitHubBranches(ctx context.Context, workspaceID, repo string) ([]string, error) {
	found, err := s.gitHubRepo(ctx, workspaceID, repo)
	if err != nil {
		return nil, err
	}
	branches, err := s.github.Branches(ctx, found.InstallationID, found.FullName)
	if err != nil {
		return nil, err
	}
	sorted := []string{found.DefaultBranch}
	for _, branch := range branches {
		if branch != found.DefaultBranch {
			sorted = append(sorted, branch)
		}
	}
	return sorted, nil
}

// gitHubRepo finds a repo the workspace's installations may read.
func (s *AppService) gitHubRepo(ctx context.Context, workspaceID, repo string) (GitHubRepoOf, error) {
	repo = strings.Trim(strings.TrimPrefix(strings.TrimSpace(repo), "https://github.com/"), "/")
	repo = strings.TrimSuffix(repo, ".git")
	if !repoPattern.MatchString(repo) {
		return GitHubRepoOf{}, ErrRepoName
	}
	repos, err := s.GitHubRepos(ctx, workspaceID)
	if err != nil {
		return GitHubRepoOf{}, err
	}
	for _, found := range repos {
		if strings.EqualFold(found.FullName, repo) {
			return found, nil
		}
	}
	return GitHubRepoOf{}, ErrRepo
}

// LinkRepo makes an app follow a repo and branch. An empty branch is the
// repo's default branch.
func (s *AppService) LinkRepo(ctx context.Context, actor Actor, idOrName, repo, branch string, autoDeploy bool) (*model.App, error) {
	// The branch goes into GitHub URLs and the push matching; git's rules
	// keep it a plain name. GitHub's default branch needs no check.
	branch = strings.TrimSpace(branch)
	if branch != "" && !protocol.ValidRefName(branch) {
		return nil, ErrRefName
	}
	app, err := s.apps.ByIDOrNameForWorkspace(idOrName, actor.WorkspaceID)
	if err != nil {
		return nil, err
	}
	// The repo and branch the app follows already: only deploy on push
	// changes, and GitHub need not answer for that.
	if app.Repo != "" && strings.EqualFold(repo, app.Repo) && branch == app.Branch {
		return s.SetAutoDeploy(actor, app.ID, autoDeploy)
	}
	found, err := s.gitHubRepo(ctx, actor.WorkspaceID, repo)
	if err != nil {
		return nil, err
	}
	if branch == "" {
		branch = found.DefaultBranch
	}
	// A branch GitHub does not have would never deploy.
	if branch != found.DefaultBranch {
		branches, err := s.github.Branches(ctx, found.InstallationID, found.FullName)
		if err != nil {
			return nil, err
		}
		if !slices.Contains(branches, branch) {
			return nil, ErrBranch
		}
	}
	err = s.apps.SetRepo(app.ID, found.FullName, branch, autoDeploy, found.InstallationID)
	if err != nil {
		return nil, err
	}
	s.servers.record(actor, "repo.linked", "app", app.Name, map[string]string{"repo": found.FullName, "branch": branch, "auto_deploy": strconv.FormatBool(autoDeploy)})
	s.notify(actor.WorkspaceID)
	return s.apps.ByID(app.ID)
}

// SetAutoDeploy turns deploy on push on or off for an app that follows a
// repo. It changes nothing else and asks GitHub nothing, so a switch on the
// page saves even while GitHub is slow or down.
func (s *AppService) SetAutoDeploy(actor Actor, idOrName string, on bool) (*model.App, error) {
	app, err := s.apps.ByIDOrNameForWorkspace(idOrName, actor.WorkspaceID)
	if err != nil {
		return nil, err
	}
	if app.Repo == "" {
		return nil, ErrNoRepo
	}
	if app.AutoDeploy == on {
		return app, nil
	}
	err = s.apps.SetAutoDeploy(app.ID, on)
	if err != nil {
		return nil, err
	}
	s.servers.record(actor, "repo.auto_deploy", "app", app.Name, map[string]string{"repo": app.Repo, "branch": app.Branch, "on": strconv.FormatBool(on)})
	s.notify(actor.WorkspaceID)
	return s.apps.ByID(app.ID)
}

// UnlinkRepo stops an app from following its repo. An app that follows
// none stays as it is, and the activity shows no change.
func (s *AppService) UnlinkRepo(actor Actor, idOrName string) error {
	app, err := s.apps.ByIDOrNameForWorkspace(idOrName, actor.WorkspaceID)
	if err != nil {
		return err
	}
	if app.Repo == "" {
		return nil
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
	// The ref goes into a GitHub URL; the linked branch was checked before.
	ref = strings.TrimSpace(ref)
	if ref != "" && !protocol.ValidRefName(ref) {
		return nil, ErrRefName
	}
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
	return s.deployCommit(ctx, actor, app, ref, ref)
}

// deployCommit deploys the commit that at points to and records it under
// ref: a push names the branch and pins its exact commit, so a newer push
// in between can't change what this deploy builds.
func (s *AppService) deployCommit(ctx context.Context, actor Actor, app *model.App, ref, at string) (*model.Deploy, error) {
	commit, err := s.github.Commit(ctx, app.InstallationID, app.Repo, at)
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
		actor := Actor{WorkspaceID: app.WorkspaceID, Source: Source{Via: ViaGitHub}}
		ready, err := s.ready(app.WorkspaceID, app.ID)
		if err != nil {
			slog.Warn("auto deploy failed", "app_id", app.ID, "repo", repo, "sha", sha, "error", err)
			continue
		}
		deploy, err := s.deployCommit(ctx, actor, ready, branch, sha)
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
	target := s.servers.appURL + model.Workspace{ID: app.WorkspaceID, Slug: app.WorkspaceSlug}.Path() + "/apps/" + app.Link()
	err := s.github.SetStatus(ctx, app.InstallationID, app.Repo, deploy.CommitSHA, state, target, description)
	if err != nil {
		slog.Warn("github commit status failed", "app_id", app.ID, "sha", deploy.CommitSHA, "error", err)
	}
}
