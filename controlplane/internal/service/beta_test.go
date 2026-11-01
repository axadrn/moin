package service

import (
	"errors"
	"testing"

	"github.com/axadrn/ops/controlplane/internal/access"
	"github.com/axadrn/ops/controlplane/internal/repository"
	"github.com/axadrn/ops/controlplane/internal/testutil"
)

func TestClosedSignupPutsNewAddressesOnTheWaitlist(t *testing.T) {
	environment := newAuthFlowEnvironment(t)
	mailer := testutil.NewMemoryMailer()
	email := NewEmailService(mailer, "http://localhost:8090", "Test App")
	beta := NewBetaService(false, []string{"admin@example.com"}, repository.NewWaitlistRepository(environment.database), environment.subscriptionRepository, email)
	environment.auth.SetBeta(beta)

	err := environment.auth.SendMagicLink("Stranger@Example.com")
	if !errors.Is(err, ErrWaitlisted) {
		t.Fatalf("a stranger: err = %v", err)
	}
	if _, err := environment.userRepository.ByEmail("stranger@example.com"); err == nil {
		t.Fatal("a stranger got an account")
	}
	if _, err := environment.auth.AuthenticateOAuth("stranger@example.com", "github"); !errors.Is(err, ErrWaitlisted) {
		t.Fatalf("a stranger through GitHub: err = %v", err)
	}
	list, err := beta.Waitlist()
	if err != nil || len(list) != 1 || list[0].Email != "stranger@example.com" || list[0].LetInAt != nil {
		t.Fatalf("waitlist = %+v, %v", list, err)
	}

	if err := environment.auth.SendMagicLink("admin@example.com"); err != nil {
		t.Fatalf("an admin: err = %v", err)
	}
	admin, _ := environment.userRepository.ByEmail("admin@example.com")
	adminWorkspace, err := environment.auth.CompleteOnboarding(admin.ID, "Ada Admin", true)
	if err != nil {
		t.Fatal(err)
	}
	if subscription, _ := environment.subscriptionRepository.ByWorkspaceID(adminWorkspace.ID); subscription.PlanID != access.PlanPro {
		t.Fatalf("the admin's workspace is on %s", subscription.PlanID)
	}

	if err := beta.LetIn("stranger@example.com", true); err != nil {
		t.Fatal(err)
	}
	if len(mailer.Messages()) == 0 {
		t.Fatal("letting in sent no email")
	}
	if err := environment.auth.SendMagicLink("stranger@example.com"); err != nil {
		t.Fatalf("someone let in: err = %v", err)
	}
	user, err := environment.userRepository.ByEmail("stranger@example.com")
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := environment.auth.CompleteOnboarding(user.ID, "Grace Hopper", true)
	if err != nil {
		t.Fatal(err)
	}
	subscription, err := environment.subscriptionRepository.ByWorkspaceID(workspace.ID)
	if err != nil || subscription.PlanID != access.PlanPro || subscription.Status != access.StatusActive {
		t.Fatalf("beta workspace = %+v, %v", subscription, err)
	}
	list, _ = beta.Waitlist()
	if list[0].LetInAt == nil || !list[0].ForeverFree {
		t.Fatalf("after letting in: %+v", list[0])
	}
}

func TestOpenSignupNeedsNoWaitlist(t *testing.T) {
	environment := newAuthFlowEnvironment(t)
	beta := NewBetaService(true, nil, repository.NewWaitlistRepository(environment.database), environment.subscriptionRepository, NewEmailService(testutil.NewMemoryMailer(), "http://localhost:8090", "Test App"))
	environment.auth.SetBeta(beta)
	if err := environment.auth.SendMagicLink("anyone@example.com"); err != nil {
		t.Fatalf("open sign-up: err = %v", err)
	}
}
