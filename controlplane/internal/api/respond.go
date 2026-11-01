// Package api serves the JSON API under /api/v1. Handlers call the same
// services as the HTML handlers and only translate between JSON and models.
package api

import (
	"encoding/json"
	"errors"
	"github.com/axadrn/ops/controlplane/internal/access"
	"github.com/axadrn/ops/controlplane/internal/repository"
	"github.com/axadrn/ops/controlplane/internal/service"
	"log/slog"
	"net/http"
)

// ErrorBody is the envelope of every non-2xx JSON response.
type ErrorBody struct {
	Error ErrorDetail `json:"error"`
}

// ErrorDetail carries a stable lowercase snake case code, a message for
// humans, and per-field messages for validation errors.
type ErrorDetail struct {
	Code    string            `json:"code"`
	Message string            `json:"message"`
	Fields  map[string]string `json:"fields,omitempty"`
}

// JSON writes value as a JSON response with the given status.
func JSON(w http.ResponseWriter, status int, value any) {
	body, err := json.Marshal(value)
	if err != nil {
		slog.Error("encode json response failed", "error", err)
		writeBody(w, http.StatusInternalServerError, []byte(`{"error":{"code":"internal","message":"Something went wrong. Please try again."}}`))
		return
	}
	writeBody(w, status, body)
}

// NoContent writes an empty 204 response.
func NoContent(w http.ResponseWriter) {
	w.WriteHeader(http.StatusNoContent)
}

// Error writes the error envelope with a code and a message.
func Error(w http.ResponseWriter, status int, code, message string) {
	JSON(w, status, ErrorBody{Error: ErrorDetail{Code: code, Message: message}})
}

// ValidationMessage is the message of every validation_failed error.
const ValidationMessage = "Check the highlighted fields and try again."

// ValidationError writes a 422 validation_failed envelope with one message
// per invalid field.
func ValidationError(w http.ResponseWriter, fields map[string]string) {
	JSON(w, http.StatusUnprocessableEntity, ErrorBody{Error: ErrorDetail{
		Code:    "validation_failed",
		Message: ValidationMessage,
		Fields:  fields,
	}})
}

// ServiceError maps a service or repository error to its status and code.
// Unknown errors are logged and answered with 500 internal.
func ServiceError(w http.ResponseWriter, err error) {
	status, detail, known := ErrorFor(err)
	if !known {
		slog.Error("api request failed", "error", err)
	}
	JSON(w, status, ErrorBody{Error: detail})
}

// ErrorFor returns the status and error detail the API answers for a service
// or repository error. Other transports, like the MCP server, use it to report
// the same codes. known is false for errors without a mapping; they get 500
// internal and the caller should log them.
func ErrorFor(err error) (int, ErrorDetail, bool) {
	var agentErr *service.AgentError
	if errors.As(err, &agentErr) {
		return http.StatusBadGateway, ErrorDetail{Code: "agent_error", Message: agentErr.Message}, true
	}
	for _, known := range serviceErrors {
		if !errors.Is(err, known.err) {
			continue
		}
		if known.field != "" {
			return http.StatusUnprocessableEntity, ErrorDetail{
				Code:    "validation_failed",
				Message: ValidationMessage,
				Fields:  map[string]string{known.field: known.message},
			}, true
		}
		return known.status, ErrorDetail{Code: known.code, Message: known.message}, true
	}
	return http.StatusInternalServerError, ErrorDetail{Code: "internal", Message: "Something went wrong. Please try again."}, false
}

// CodeForStatus returns the default code for a status, used by middleware
// errors that have no more specific code.
func CodeForStatus(status int) string {
	switch status {
	case http.StatusBadRequest:
		return "bad_request"
	case http.StatusUnauthorized:
		return "unauthenticated"
	case http.StatusForbidden:
		return "forbidden"
	case http.StatusNotFound:
		return "not_found"
	case http.StatusMethodNotAllowed:
		return "method_not_allowed"
	case http.StatusConflict:
		return "conflict"
	case http.StatusRequestEntityTooLarge:
		return "body_too_large"
	case http.StatusUnsupportedMediaType:
		return "unsupported_media_type"
	case http.StatusUnprocessableEntity:
		return "validation_failed"
	case http.StatusTooManyRequests:
		return "rate_limited"
	}
	if status >= http.StatusInternalServerError {
		return "internal"
	}
	return "error"
}

func writeBody(w http.ResponseWriter, status int, body []byte) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, err := w.Write(append(body, '\n'))
	if err != nil {
		slog.Debug("write json response failed", "error", err)
	}
}

// serviceError maps one sentinel error. A field turns the answer into a 422
// validation error for that field.
type serviceError struct {
	err     error
	status  int
	code    string
	message string
	field   string
}

var serviceErrors = []serviceError{
	{err: service.ErrInvalidCredentials, status: http.StatusUnauthorized, code: "invalid_credentials", message: "Invalid email or password."},
	{err: service.ErrEmailNotVerified, status: http.StatusForbidden, code: "email_not_verified", message: "Verify your email address before signing in with a password."},
	{err: service.ErrEmailAlreadyExists, status: http.StatusConflict, code: "email_taken", message: "An account with this email address already exists."},
	{err: repository.ErrDuplicateEmail, status: http.StatusConflict, code: "email_taken", message: "An account with this email address already exists."},
	{err: service.ErrInvalidEmail, field: "email", message: "Enter a valid email address."},
	{err: service.ErrWeakPassword, field: "password", message: "Password must be at least 12 characters."},
	{err: service.ErrCommonPassword, field: "password", message: "Password is too common. Choose a stronger one."},
	{err: service.ErrNameRequired, field: "name", message: "Enter your name."},
	{err: service.ErrInvalidCurrentPassword, field: "current_password", message: "Current password is incorrect."},
	{err: repository.ErrTokenNotFound, status: http.StatusBadRequest, code: "invalid_token", message: "This link is invalid or has expired."},
	{err: repository.ErrTokenExpired, status: http.StatusBadRequest, code: "invalid_token", message: "This link is invalid or has expired."},
	{err: repository.ErrTokenUsed, status: http.StatusBadRequest, code: "invalid_token", message: "This link is invalid or has expired."},
	{err: repository.ErrUserNotFound, status: http.StatusNotFound, code: "not_found", message: "The user was not found."},
	{err: repository.ErrSessionNotFound, status: http.StatusNotFound, code: "not_found", message: "The session was not found."},
	{err: repository.ErrAPITokenNotFound, status: http.StatusNotFound, code: "not_found", message: "The API token was not found."},
	{err: service.ErrAPITokenName, field: "name", message: "Name must contain between 1 and 64 characters."},
	{err: service.ErrAPITokenScope, field: "scopes", message: "Choose read, or read and write."},
	{err: service.ErrAPITokenExpiry, field: "expires_in_days", message: "Choose between 1 and 365 days, or no expiry."},
	{err: service.ErrAPITokenLimit, status: http.StatusConflict, code: "token_limit", message: "You have reached the limit of 50 API tokens. Revoke one first."},
	{err: service.ErrAPITokenWorkspace, field: "workspace_id", message: "Choose one of your workspaces."},
	{err: service.ErrAdminPermission, status: http.StatusForbidden, code: "forbidden", message: "Admin access is required."},
	{err: service.ErrAdminSelfAction, status: http.StatusConflict, code: "admin_self_action", message: "This action is not available for your own account."},
	{err: service.ErrAdminTargetIsOperator, status: http.StatusForbidden, code: "forbidden", message: "This action is not available for operator accounts."},
	{err: service.ErrActiveSubscription, status: http.StatusConflict, code: "active_subscription", message: "End paid access before deleting."},
	{err: repository.ErrSubscriptionNotFound, status: http.StatusNotFound, code: "not_found", message: "The subscription was not found."},
	{err: access.ErrProductAccess, status: http.StatusForbidden, code: "feature_unavailable", message: "Your plan does not include this feature."},
	{err: access.ErrFeatureUnavailable, status: http.StatusForbidden, code: "feature_unavailable", message: "Your plan does not include this feature."},
	{err: access.ErrLimitReached, status: http.StatusForbidden, code: "limit_reached", message: "Your plan limit is reached."},
	{err: access.ErrQuotaExceeded, status: http.StatusForbidden, code: "quota_exceeded", message: "Your plan quota is used up."},
	{err: service.ErrWorkspacePermission, status: http.StatusForbidden, code: "forbidden", message: "Your workspace role does not allow this action."},
	{err: repository.ErrWorkspaceMemberPermission, status: http.StatusForbidden, code: "forbidden", message: "Your workspace role does not allow this action."},
	{err: service.ErrInvalidWorkspace, field: "name", message: "Workspace name must contain between 2 and 80 characters."},
	{err: service.ErrInvalidRole, field: "role", message: "Choose a valid workspace role."},
	{err: service.ErrInvalidInviteEmail, field: "email", message: "Enter a valid email address."},
	{err: service.ErrSoleWorkspaceOwner, status: http.StatusConflict, code: "sole_workspace_owner", message: "Transfer or delete the workspaces you own before deleting your account."},
	{err: repository.ErrWorkspaceNotFound, status: http.StatusNotFound, code: "not_found", message: "The workspace was not found."},
	{err: repository.ErrWorkspaceMemberNotFound, status: http.StatusNotFound, code: "not_found", message: "The workspace member was not found."},
	{err: repository.ErrWorkspaceBillingActive, status: http.StatusConflict, code: "active_subscription", message: "End paid access before deleting the workspace."},
	{err: repository.ErrLastOwner, status: http.StatusConflict, code: "last_owner", message: "A workspace must keep at least one owner."},
	{err: repository.ErrWorkspaceInvitationNotFound, status: http.StatusNotFound, code: "not_found", message: "The invitation was not found."},
	{err: repository.ErrWorkspaceInvitationExpired, status: http.StatusGone, code: "invitation_unavailable", message: "This invitation is no longer available."},
	{err: repository.ErrWorkspaceInvitationRevoked, status: http.StatusGone, code: "invitation_unavailable", message: "This invitation is no longer available."},
	{err: repository.ErrWorkspaceInvitationUsed, status: http.StatusGone, code: "invitation_unavailable", message: "This invitation is no longer available."},
	{err: repository.ErrWorkspaceInvitationEmail, status: http.StatusForbidden, code: "invitation_email_mismatch", message: "This invitation belongs to another email address."},
	{err: repository.ErrAlreadyMember, status: http.StatusConflict, code: "already_member", message: "This person is already a member of the workspace."},
	{err: repository.ErrSeatLimit, status: http.StatusConflict, code: "seat_limit", message: "The workspace has no free seats."},
	{err: repository.ErrCheckoutPending, status: http.StatusConflict, code: "checkout_pending", message: "A checkout is still being created. Try again in a moment."},
	{err: repository.ErrCheckoutSelection, status: http.StatusConflict, code: "checkout_conflict", message: "A different checkout is already open."},
	{err: repository.ErrSubscriptionAlreadyPaid, status: http.StatusConflict, code: "already_paid", message: "This subscription is already paid."},
	{err: ErrBillingPermission, status: http.StatusForbidden, code: "forbidden", message: "Only workspace owners can manage billing."},
	{err: ErrActivityPermission, status: http.StatusForbidden, code: "forbidden", message: "You do not have permission to view workspace activity."},
	{err: ErrInvalidCursor, status: http.StatusBadRequest, code: "invalid_cursor", message: "The cursor is invalid. Start again without a cursor."},
	{err: service.ErrFilePermission, status: http.StatusForbidden, code: "forbidden", message: "This file belongs to another account."},
	{err: repository.ErrServerNotFound, status: http.StatusNotFound, code: "not_found", message: "The server was not found."},
	{err: repository.ErrDuplicateServer, field: "name", message: "A server with this name already exists."},
	{err: service.ErrServerName, field: "name", message: "Name must contain between 1 and 64 characters."},
	{err: repository.ErrServerHasApps, status: http.StatusConflict, code: "server_has_apps", message: "Remove the apps on this server first."},
	{err: service.ErrSubscriptionRequired, status: http.StatusPaymentRequired, code: "subscription_required", message: "Subscribe to ops under Billing to add servers."},
	{err: service.ErrServerOffline, status: http.StatusConflict, code: "server_offline", message: "The server is offline. Start its agent and try again."},
	{err: repository.ErrAppNotFound, status: http.StatusNotFound, code: "not_found", message: "The app was not found."},
	{err: repository.ErrDuplicateApp, field: "name", message: "An app with this name already exists."},
	{err: service.ErrAppName, field: "name", message: "Use 1 to 63 lowercase letters, digits or dashes, starting with a letter or digit."},
	{err: repository.ErrDeployNotFound, status: http.StatusNotFound, code: "not_found", message: "The deploy was not found."},
	{err: service.ErrDeployRunning, status: http.StatusConflict, code: "deploy_running", message: "A deploy of this app is still running."},
	{err: service.ErrImage, field: "image", message: "Use an image reference like ghcr.io/me/app:tag."},
	{err: service.ErrDeployHook, status: http.StatusNotFound, code: "not_found", message: "This deploy hook does not exist. Create a new one for the app."},
	{err: service.ErrRegistryHost, field: "host", message: "Use a registry host name like ghcr.io."},
	{err: service.ErrRegistryLogin, field: "password", message: "Enter the username and the password or token."},
	{err: repository.ErrRegistryNotFound, status: http.StatusNotFound, code: "not_found", message: "No login for this registry."},
	{err: service.ErrDomain, field: "domains", message: "Use host names like blog.de."},
	{err: service.ErrCanonical, field: "canonical", message: "The canonical domain must be one of the domains."},
	{err: service.ErrProtect, field: "password", message: "Use a username without spaces or colons and a password of 8 to 72 characters."},
	{err: service.ErrNoSource, status: http.StatusConflict, code: "no_source", message: "No deploy has a source or an image yet. Run ops deploy in the repo first."},
	{err: service.ErrNoRollback, status: http.StatusConflict, code: "no_rollback", message: "There is no earlier successful deploy to roll back to."},
	{err: service.ErrEnvKey, field: "set", message: "Env keys are letters, digits and underscores and do not start with a digit."},
	{err: service.ErrEnvValue, field: "set", message: "Env values are at most 32 KB."},
	{err: service.ErrEnvLimit, field: "set", message: "An app has at most 200 env vars."},
	{err: repository.ErrFileNotFound, status: http.StatusNotFound, code: "not_found", message: "The file was not found."},
	{err: service.ErrNoDatabase, status: http.StatusNotFound, code: "no_database", message: "The live release has no SQLite database on a volume."},
	{err: service.ErrNoTable, status: http.StatusNotFound, code: "not_found", message: "The database has no such table."},
	{err: service.ErrNoColumn, field: "column", message: "The table has no such column."},
	{err: service.ErrNoRowID, status: http.StatusConflict, code: "not_editable", message: "Rows of views and WITHOUT ROWID tables cannot be edited here. Use SQL."},
	{err: service.ErrSQL, field: "sql", message: "Enter a query."},
	{err: service.ErrUploadBucket, status: http.StatusConflict, code: "no_bucket", message: "ops deploy uploads your code to your own bucket. Add one under Bucket first."},
	{err: service.ErrUpload, field: "upload", message: "The upload is unknown or expired. Run ops deploy again."},
	{err: service.ErrNoBackups, status: http.StatusConflict, code: "no_backups", message: "Backups are not set up. Add a bucket first."},
	{err: service.ErrBackupEndpoint, field: "endpoint", message: "Use an https:// URL, or leave it empty for AWS S3."},
	{err: service.ErrBackupBucket, field: "bucket", message: "Enter the bucket, the access key and the secret key."},
	{err: service.ErrBackupAccess, field: "secret_key", message: "ops could not write to the bucket with these keys. Check the bucket name, the endpoint and that the key may write."},
	{err: service.ErrRestoreTarget, field: "snapshot", message: "Choose a snapshot or a point in time."},
	{err: service.ErrGitHubOff, status: http.StatusConflict, code: "github_off", message: "The GitHub App is not set up on this control plane."},
	{err: service.ErrGitHubState, status: http.StatusBadRequest, code: "github_state", message: "The GitHub install link expired or belongs to another account. Connect again."},
	{err: service.ErrRepo, field: "repo", message: "ops cannot read this repo. Install the ops GitHub App on its account and give it the repo."},
	{err: service.ErrRepoName, field: "repo", message: "Name the repo as owner/name."},
	{err: service.ErrNoRepo, status: http.StatusConflict, code: "no_repo", message: "The app follows no repo. Link one first."},
	{err: service.ErrRef, field: "ref", message: "GitHub knows no branch, tag or commit by this name."},
	{err: service.ErrNoCloudflare, status: http.StatusConflict, code: "no_cloudflare", message: "Add a Cloudflare API token first."},
	{err: service.ErrNoTailscale, status: http.StatusConflict, code: "no_tailscale", message: "Add a Tailscale auth key first."},
	{err: service.ErrCloudflareToken, field: "cloudflare_token", message: "Cloudflare does not accept this token. It needs Account Cloudflare Tunnel Edit, Zone DNS Edit and Zone Settings Edit."},
	{err: service.ErrTailscaleKey, field: "tailscale_key", message: "A Tailscale auth key starts with tskey-."},
}
