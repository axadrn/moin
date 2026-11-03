package service

import (
	"errors"
	"testing"
	"time"

	"github.com/axadrn/ops/controlplane/internal/access"
	"github.com/axadrn/ops/controlplane/internal/model"
	"github.com/axadrn/ops/controlplane/internal/repository"
	"github.com/axadrn/ops/controlplane/internal/testutil"
)

func TestClosedSignupPutsNewAddressesOnTheWaitlist(t *testing.T) {
	environment := newAuthFlowEnvironment(t)
	mailer := testutil.NewMemoryMailer()
	email := NewEmailService(mailer, "http://localhost:8090", "Test App")
	beta := NewBetaService(false, true, []string{"admin@example.com"}, repository.NewWaitlistRepository(environment.database), environment.subscriptionRepository, email)
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
	adminWorkspace, err := environment.auth.CompleteOnboarding(admin.ID, "Ada Admin", "", true)
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
	workspace, err := environment.auth.CompleteOnboarding(user.ID, "Grace Hopper", "", true)
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
	beta := NewBetaService(true, true, nil, repository.NewWaitlistRepository(environment.database), environment.subscriptionRepository, NewEmailService(testutil.NewMemoryMailer(), "http://localhost:8090", "Test App"))
	environment.auth.SetBeta(beta)
	if err := environment.auth.SendMagicLink("anyone@example.com"); err != nil {
		t.Fatalf("open sign-up: err = %v", err)
	}
}

func TestSelfHostedClosedSignupIsPrivate(t *testing.T) {
	environment := newAuthFlowEnvironment(t)
	waitlist := repository.NewWaitlistRepository(environment.database)
	beta := NewBetaService(false, false, []string{"admin@example.com"}, waitlist, environment.subscriptionRepository, NewEmailService(testutil.NewMemoryMailer(), "http://localhost:8090", "Test App"))
	environment.auth.SetBeta(beta)

	if err := environment.auth.SendMagicLink("stranger@example.com"); !errors.Is(err, ErrPrivate) {
		t.Fatalf("a stranger: err = %v", err)
	}
	if _, err := environment.auth.AuthenticateOAuth("stranger@example.com", "github"); !errors.Is(err, ErrPrivate) {
		t.Fatalf("a stranger through GitHub: err = %v", err)
	}
	if _, err := environment.userRepository.ByEmail("stranger@example.com"); err == nil {
		t.Fatal("a stranger got an account")
	}
	if list, err := beta.Waitlist(); err != nil || len(list) != 0 {
		t.Fatalf("waitlist = %+v, %v", list, err)
	}

	if err := environment.auth.SendMagicLink("Admin@Example.com"); err != nil {
		t.Fatalf("an admin: err = %v", err)
	}
	admin, err := environment.userRepository.ByEmail("admin@example.com")
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := environment.auth.CompleteOnboarding(admin.ID, "Ada Admin", "", true)
	if err != nil {
		t.Fatal(err)
	}
	_, member, err := environment.workspaceService.BySlugForUser(workspace.Slug, admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := environment.workspaceService.Invite(member, workspace, "mate@example.com", model.WorkspaceRoleMember, Source{Via: "web"}); err != nil {
		t.Fatal(err)
	}
	if err := environment.auth.SendMagicLink("mate@example.com"); err != nil {
		t.Fatalf("an invited teammate: err = %v", err)
	}
	if _, err := environment.userRepository.ByEmail("mate@example.com"); err != nil {
		t.Fatalf("the invited teammate has no account: %v", err)
	}
}

// signUp lets an address through closed sign-up and makes its first
// workspace, the way the onboarding does.
func signUp(t *testing.T, environment authFlowEnvironment, address, name string) *model.Workspace {
	t.Helper()
	if err := environment.auth.SendMagicLink(address); err != nil {
		t.Fatalf("%s: err = %v", address, err)
	}
	user, err := environment.userRepository.ByEmail(address)
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := environment.auth.CompleteOnboarding(user.ID, name, "", true)
	if err != nil {
		t.Fatal(err)
	}
	return workspace
}

func planOf(t *testing.T, environment authFlowEnvironment, workspaceID string) *model.Subscription {
	t.Helper()
	subscription, err := environment.subscriptionRepository.ByWorkspaceID(workspaceID)
	if err != nil {
		t.Fatal(err)
	}
	return subscription
}

func entryOf(t *testing.T, beta *BetaService, address string) model.WaitlistEntry {
	t.Helper()
	list, err := beta.Waitlist()
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range list {
		if entry.Email == address {
			return entry
		}
	}
	t.Fatalf("%s is not on the waitlist", address)
	return model.WaitlistEntry{}
}

func TestLetInWithoutForeverFreeGetsTheTrial(t *testing.T) {
	environment := newAuthFlowEnvironment(t)
	beta := NewBetaService(false, true, nil, repository.NewWaitlistRepository(environment.database), environment.subscriptionRepository, NewEmailService(testutil.NewMemoryMailer(), "http://localhost:8090", "Test App"))
	environment.auth.SetBeta(beta)

	if err := beta.LetIn("trial@example.com", false); err != nil {
		t.Fatal(err)
	}
	if entry := entryOf(t, beta, "trial@example.com"); entry.Plan != model.WaitlistNoAccount {
		t.Fatalf("before sign-up: plan = %q", entry.Plan)
	}
	workspace := signUp(t, environment, "trial@example.com", "Trial Person")
	subscription := planOf(t, environment, workspace.ID)
	if subscription.Status != access.StatusTrialing || subscription.CurrentPeriodEnd == nil {
		t.Fatalf("let in without free forever: %+v", subscription)
	}
	entry := entryOf(t, beta, "trial@example.com")
	if entry.Plan != model.WaitlistTrial || entry.TrialEndsAt == nil {
		t.Fatalf("admin list: %+v", entry)
	}
}

func TestForeverFreeChangesAnAccountRightAway(t *testing.T) {
	environment := newAuthFlowEnvironment(t)
	mailer := testutil.NewMemoryMailer()
	beta := NewBetaService(false, true, nil, repository.NewWaitlistRepository(environment.database), environment.subscriptionRepository, NewEmailService(mailer, "http://localhost:8090", "Test App"))
	environment.auth.SetBeta(beta)

	if err := beta.LetIn("ada@example.com", false); err != nil {
		t.Fatal(err)
	}
	workspace := signUp(t, environment, "ada@example.com", "Ada Lovelace")
	trialEnd := *planOf(t, environment, workspace.ID).CurrentPeriodEnd
	sent := len(mailer.Messages())

	if err := beta.LetIn("ada@example.com", true); err != nil {
		t.Fatal(err)
	}
	subscription := planOf(t, environment, workspace.ID)
	if !isForeverFree(subscription) {
		t.Fatalf("ticked free forever: %+v", subscription)
	}
	if entry := entryOf(t, beta, "ada@example.com"); entry.Plan != model.WaitlistFreeForever || !entry.ForeverFree {
		t.Fatalf("admin list: %+v", entry)
	}
	if len(mailer.Messages()) != sent {
		t.Fatal("an account that is already in got the sign-in email again")
	}

	// Unticked during the trial: the rest of the trial.
	if err := beta.LetIn("ada@example.com", false); err != nil {
		t.Fatal(err)
	}
	subscription = planOf(t, environment, workspace.ID)
	if subscription.Status != access.StatusTrialing || subscription.CurrentPeriodEnd == nil || !subscription.CurrentPeriodEnd.Equal(trialEnd) {
		t.Fatalf("unticked during the trial: %+v, trial ends %s", subscription, trialEnd)
	}

	// Unticked after the trial: the free plan, read-only.
	if err := beta.LetIn("ada@example.com", true); err != nil {
		t.Fatal(err)
	}
	beta.now = func() time.Time { return trialEnd.Add(24 * time.Hour) }
	if err := beta.LetIn("ada@example.com", false); err != nil {
		t.Fatal(err)
	}
	subscription = planOf(t, environment, workspace.ID)
	if subscription.PlanID != access.PlanFree || subscription.Status != access.StatusActive {
		t.Fatalf("unticked after the trial: %+v", subscription)
	}
	if entry := entryOf(t, beta, "ada@example.com"); entry.Plan != model.WaitlistTrialEnded {
		t.Fatalf("admin list after the trial: %+v", entry)
	}
}

func TestForeverFreeLeavesAPayingWorkspaceAlone(t *testing.T) {
	environment := newAuthFlowEnvironment(t)
	beta := NewBetaService(false, true, nil, repository.NewWaitlistRepository(environment.database), environment.subscriptionRepository, NewEmailService(testutil.NewMemoryMailer(), "http://localhost:8090", "Test App"))
	environment.auth.SetBeta(beta)

	if err := beta.LetIn("payer@example.com", false); err != nil {
		t.Fatal(err)
	}
	workspace := signUp(t, environment, "payer@example.com", "Pay Er")
	subscription := planOf(t, environment, workspace.ID)
	providerID := "sub_123"
	end := time.Now().Add(30 * 24 * time.Hour)
	subscription.PlanID, subscription.Status, subscription.CurrentPeriodEnd, subscription.ProviderSubscriptionID = access.PlanPro, access.StatusActive, &end, &providerID
	if err := environment.subscriptionRepository.Update(subscription); err != nil {
		t.Fatal(err)
	}
	if entry := entryOf(t, beta, "payer@example.com"); entry.Plan != model.WaitlistPaying {
		t.Fatalf("admin list: %+v", entry)
	}
	if err := beta.LetIn("payer@example.com", true); !errors.Is(err, ErrPaying) {
		t.Fatalf("free forever for a paying workspace: err = %v", err)
	}
	if entry := entryOf(t, beta, "payer@example.com"); entry.ForeverFree {
		t.Fatal("the refused change was stored")
	}
	if err := beta.LetIn("payer@example.com", false); err != nil {
		t.Fatal(err)
	}
	if after := planOf(t, environment, workspace.ID); after.CurrentPeriodEnd == nil || !after.HasProviderSubscription() {
		t.Fatalf("unticking touched the paying workspace: %+v", after)
	}
}
