package service

import "strings"

// bootstrapWorkspace names the workspace Bootstrap makes. Onboarding asks
// the person's name afterwards, and the workspace can be renamed.
const bootstrapWorkspace = "My Workspace"

// Bootstrap readies a hub that runs on the same server as its
// apps: the account of email with a workspace, and the server named
// serverName waiting for its daemon. It returns the install token, or ""
// when the daemon of that server has connected already. Run again, it makes
// nothing twice: the same account, workspace and server, with a new token.
func Bootstrap(auth *AuthService, workspaces *WorkspaceService, servers *ServerService, email, serverName string) (string, error) {
	user, err := auth.EnsureUser(email)
	if err != nil {
		return "", err
	}
	workspace, err := workspaces.EnsureFirst(user.ID, user.Name, bootstrapWorkspace)
	if err != nil {
		return "", err
	}
	actor := Actor{WorkspaceID: workspace.ID, UserID: user.ID, Source: Source{Via: ViaSystem}}
	serverName = ServerNameFrom(serverName)
	if serverName == "" {
		// A fixed name, so a second run finds the server again.
		serverName = "server"
	}
	list, err := servers.List(workspace.ID)
	if err != nil {
		return "", err
	}
	for _, server := range list {
		if server.Name != serverName {
			continue
		}
		if server.DaemonTokenHash != nil {
			return "", nil
		}
		_, token, err := servers.NewInstallToken(actor, server.ID)
		return token, err
	}
	_, token, err := servers.Create(actor, serverName)
	return token, err
}

// ServerNameFrom turns a hostname into a server name: lowercase letters,
// digits and dashes from its first label, like "web-1" from
// "Web_1.example.com". It is "" when nothing valid is left.
func ServerNameFrom(hostname string) string {
	label, _, _ := strings.Cut(strings.ToLower(strings.TrimSpace(hostname)), ".")
	var name strings.Builder
	dash := false
	for _, char := range label {
		if char >= 'a' && char <= 'z' || char >= '0' && char <= '9' {
			if dash && name.Len() > 0 {
				name.WriteByte('-')
			}
			dash = false
			name.WriteRune(char)
			continue
		}
		dash = true
	}
	result := name.String()
	if len(result) > 63 {
		result = strings.TrimRight(result[:63], "-")
	}
	if !validServerName(result) {
		return ""
	}
	return result
}
