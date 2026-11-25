package service

import (
	"fmt"
	"time"
)

func welcomeEmailTemplate(name, appURL, appName string) emailContent {
	return emailContent{
		Subject:     fmt.Sprintf("Welcome to %s", appName),
		Preheader:   "Your account is ready.",
		Heading:     fmt.Sprintf("Welcome to %s", appName),
		Greeting:    fmt.Sprintf("Hi %s,", name),
		Intro:       []string{"Your account is ready. Next: add your first server."},
		ActionLabel: "Get started",
		ActionURL:   appURL,
		AppName:     appName,
	}
}

func workspaceInvitationEmailTemplate(invitationURL, workspaceName, inviterName, appName string) emailContent {
	return emailContent{
		Subject:     fmt.Sprintf("Join %s on %s", workspaceName, appName),
		Preheader:   fmt.Sprintf("%s invited you to join %s.", inviterName, workspaceName),
		Heading:     fmt.Sprintf("Join %s", workspaceName),
		Intro:       []string{fmt.Sprintf("%s invited you to join %s on %s.", inviterName, workspaceName, appName)},
		ActionLabel: "Accept the invitation",
		ActionURL:   invitationURL,
		Outro: []string{
			"This invitation expires in 7 days and can only be accepted by this email address.",
			"Not expecting it? Ignore this mail.",
		},
		AppName: appName,
	}
}

func emailChangeVerificationTemplate(name, verifyURL, appName string) emailContent {
	return emailContent{
		Subject:     fmt.Sprintf("Verify your new email for %s", appName),
		Preheader:   "Verify your new email address.",
		Heading:     "Verify your new email",
		Greeting:    fmt.Sprintf("Hi %s,", name),
		Intro:       []string{"You asked to change your email. Verify the new address."},
		ActionLabel: "Verify email",
		ActionURL:   verifyURL,
		Outro: []string{
			"This link expires in 24 hours.",
			"Not you? Ignore this mail.",
		},
		AppName: appName,
	}
}

func emailChangeNotificationTemplate(name, newEmail, appName string) emailContent {
	return emailContent{
		Subject:   fmt.Sprintf("Email change requested for %s", appName),
		Preheader: "Someone asked to change your email.",
		Heading:   "Email change requested",
		Greeting:  fmt.Sprintf("Hi %s,", name),
		Intro: []string{
			fmt.Sprintf("Someone asked to change your email to %s.", newEmail),
			"Was it you? Click the link we sent there.",
		},
		Outro:   []string{"Not you? Someone may be in your account. Sign in and use Sign out everywhere under Settings, Account, Security."},
		AppName: appName,
	}
}

// passkeyAddedTemplate tells a user a passkey was added to their account,
// so one nobody expected does not go unseen.
func passkeyAddedTemplate(name string, at time.Time, appName string) emailContent {
	greeting := "Hi,"
	if name != "" {
		greeting = fmt.Sprintf("Hi %s,", name)
	}
	return emailContent{
		Subject:   "A passkey was added to your account",
		Preheader: fmt.Sprintf("A passkey now signs in to your %s account.", appName),
		Heading:   "A passkey was added",
		Greeting:  greeting,
		Intro: []string{
			fmt.Sprintf("A passkey was added to your %s account on %s.", appName, at.UTC().Format("January 2, 2006 at 15:04 UTC")),
			"It signs in to your account from now on.",
		},
		Outro:   []string{"If this was not you, sign in, remove the passkey under Settings, Account, Security, and use Sign out everywhere."},
		AppName: appName,
	}
}

func accountDeletedEmailTemplate(name, appName string) emailContent {
	return emailContent{
		Subject:   fmt.Sprintf("Your %s account is deleted", appName),
		Preheader: "Your account is deleted.",
		Heading:   "Your account is deleted",
		Greeting:  fmt.Sprintf("Hi %s,", name),
		Intro: []string{
			fmt.Sprintf("Your %s account is deleted. It cannot come back.", appName),
			"Your profile, picture, passkeys, sessions, connections and CI tokens are gone. Workspaces you shared with others stay, and your email stays on your entries in their activity.",
		},
		AppName: appName,
	}
}
