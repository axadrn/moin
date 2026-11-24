package service

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/axadrn/moin/hub/internal/model"
	"github.com/axadrn/moin/hub/internal/repository"
	"github.com/axadrn/moin/hub/internal/testutil"
)

func TestDeleteAccountRequiresAnotherWorkspaceOwner(t *testing.T) {
	database, workspaces, _ := workspaceTestService(t)
	fixture := testutil.SeedUser(t, database, testutil.UserOptions{ID: "delete-owner", Email: "delete@example.com", Name: "Delete Owner"})
	users := repository.NewUsers(database)
	files := NewFileService(repository.NewFiles(database), testutil.NewMemoryStorage())
	email := NewEmailService(testutil.NewMemoryMailer(), "https://app.example.com", "Test App")
	userService := NewUserService(users, files, email, workspaces)

	if err := userService.DeleteAccount(fixture.User.ID); !errors.Is(err, ErrSoleWorkspaceOwner) {
		t.Fatalf("DeleteAccount() sole owner error = %v, want ErrSoleWorkspaceOwner", err)
	}

	secondOwner := createWorkspaceTestUser(t, database, "delete-second-owner", "second-owner@example.com")
	if _, err := database.Exec(`
		INSERT INTO workspace_members (id, workspace_id, user_id, role, created_at)
		VALUES ($1, $2, $3, $4, $5)
	`, "member-delete-second-owner", fixture.Workspace.ID, secondOwner.ID, model.WorkspaceRoleOwner, time.Now()); err != nil {
		t.Fatalf("add second owner: %v", err)
	}

	if err := userService.DeleteAccount(fixture.User.ID); err != nil {
		t.Fatalf("DeleteAccount() with second owner error = %v", err)
	}
	if _, err := users.ByID(fixture.User.ID); !errors.Is(err, repository.ErrUserNotFound) {
		t.Fatalf("deleted user lookup error = %v, want ErrUserNotFound", err)
	}
	workspace, err := repository.NewWorkspaces(database).ByID(fixture.Workspace.ID)
	if err != nil {
		t.Fatalf("workspace was deleted with departing owner: %v", err)
	}
	if workspace.CreatedBy != nil {
		t.Fatalf("workspace created_by = %v, want nil after creator deletion", workspace.CreatedBy)
	}
}

func TestConcurrentAccountDeletionKeepsAnWorkspaceOwner(t *testing.T) {
	database, workspaces, _ := workspaceTestService(t)
	first := testutil.SeedUser(t, database, testutil.UserOptions{ID: "concurrent-delete-first", Email: "first@example.com", Name: "First Owner"})
	second := createWorkspaceTestUser(t, database, "concurrent-delete-second", "second@example.com")
	if _, err := database.Exec(`
		INSERT INTO workspace_members (id, workspace_id, user_id, role, created_at)
		VALUES ($1, $2, $3, $4, $5)
	`, "member-concurrent-delete-second", first.Workspace.ID, second.ID, model.WorkspaceRoleOwner, time.Now()); err != nil {
		t.Fatalf("add second owner: %v", err)
	}

	users := repository.NewUsers(database)
	userService := NewUserService(
		users,
		NewFileService(repository.NewFiles(database), testutil.NewMemoryStorage()),
		NewEmailService(testutil.NewMemoryMailer(), "https://app.example.com", "Test App"),
		workspaces,
	)
	start := make(chan struct{})
	errorsByUser := make(chan error, 2)
	var wait sync.WaitGroup
	for _, userID := range []string{first.User.ID, second.ID} {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			errorsByUser <- userService.DeleteAccount(userID)
		}()
	}
	close(start)
	wait.Wait()
	close(errorsByUser)

	var deleted, blocked int
	for err := range errorsByUser {
		switch {
		case err == nil:
			deleted++
		case errors.Is(err, ErrSoleWorkspaceOwner):
			blocked++
		default:
			t.Fatalf("concurrent DeleteAccount() error = %v", err)
		}
	}
	if deleted != 1 || blocked != 1 {
		t.Fatalf("deleted = %d, blocked = %d, want one each", deleted, blocked)
	}

	var owners int
	if err := database.Get(&owners, `SELECT COUNT(*) FROM workspace_members WHERE workspace_id = $1 AND role = 'owner'`, first.Workspace.ID); err != nil {
		t.Fatalf("count remaining owners: %v", err)
	}
	if owners != 1 {
		t.Fatalf("remaining owners = %d, want 1", owners)
	}
}

func TestRefusedAccountDeletionKeepsFiles(t *testing.T) {
	database, workspaces, _ := workspaceTestService(t)
	fixture := testutil.SeedUser(t, database, testutil.UserOptions{ID: "delete-files-owner", Email: "files@example.com", Name: "Files Owner"})
	storage := testutil.NewMemoryStorage()
	err := storage.Save("avatars/delete-files-owner.png", strings.NewReader("avatar"))
	if err != nil {
		t.Fatalf("save avatar: %v", err)
	}
	err = repository.NewFiles(database).Create(&model.File{
		ID:          "file-delete-files-owner",
		UserID:      fixture.User.ID,
		OwnerType:   "user",
		OwnerID:     fixture.User.ID,
		Type:        model.FileTypeAvatar,
		Filename:    "delete-files-owner.png",
		MimeType:    "image/png",
		Size:        6,
		StoragePath: "avatars/delete-files-owner.png",
		Public:      true,
		CreatedAt:   time.Now(),
	})
	if err != nil {
		t.Fatalf("create file row: %v", err)
	}
	files := NewFileService(repository.NewFiles(database), storage)
	email := NewEmailService(testutil.NewMemoryMailer(), "https://app.example.com", "Test App")
	userService := NewUserService(repository.NewUsers(database), files, email, workspaces)

	err = userService.DeleteAccount(fixture.User.ID)
	if !errors.Is(err, ErrSoleWorkspaceOwner) {
		t.Fatalf("DeleteAccount() error = %v, want ErrSoleWorkspaceOwner", err)
	}
	if !storage.Has("avatars/delete-files-owner.png") {
		t.Fatal("refused deletion removed the avatar from storage")
	}
	remaining, err := files.AllUserFiles(fixture.User.ID)
	if err != nil || len(remaining) != 1 {
		t.Fatalf("file rows after refused deletion = %d, error = %v", len(remaining), err)
	}

	err = workspaces.Delete(fixture.WorkspaceMember, fixture.Workspace, fixture.Workspace.Name)
	if err != nil {
		t.Fatalf("delete workspace: %v", err)
	}
	err = userService.DeleteAccount(fixture.User.ID)
	if err != nil {
		t.Fatalf("DeleteAccount() after workspace deletion error = %v", err)
	}
	if storage.Has("avatars/delete-files-owner.png") {
		t.Fatal("deleted account kept its avatar in storage")
	}
}
