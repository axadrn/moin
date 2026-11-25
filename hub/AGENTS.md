# AGENTS.md

## Custom rules

moin stands on its own: it started from a starter template, but it follows only its own rules now and never keeps code just to stay easy to merge with one.

- This is the moin hub. How moin works is in `content/docs`, the only source of truth.
- The daemon, CLI, TUI and the protocol package live at the root of this repo, one Go module for all. The repo is public: never commit secrets, `.env` files or anything private.
- Every feature works in the web UI, the JSON API (with `openapi.yaml`), the CLI and an MCP tool; it is done when each one works end to end. The TUI covers the daily work and opens the web for setup. `content/docs/reference/surfaces.md` ("Where things work") lists what each surface does; update it with every feature.
- Everything belongs to a workspace: moin services take the workspace ID to read and a `service.Actor` (workspace, user, how it came in) to change. Every change records itself in the activity with `record` (`internal/service/activity.go`), never with secret values or user data (see Activity in `DESIGN.md`), so the team sees who did what on the web, in `moin activity`, the TUI, the API and MCP.
- Every feature is in the docs under `content/docs`. Change the docs in the same commit as the feature: the guide, the CLI and TUI reference, the API table and the MCP tool table. The docs are the only source of truth; READMEs stay short and point to them.

## Stack

- Go 1.26 with the standard library `net/http` router.
- templ, htmx 4.0, vanilla JavaScript, and moin UI (`../ui`): native CSS, no build step.
- sqlx with raw SQL and SQLite.
- go-webauthn for passkeys: every WebAuthn check is security code we never write ourselves. The browser side uses only the native `PublicKeyCredential` JSON methods.
- Cloudflare Email Sending for transactional email. Without credentials in development, emails go to the log.
- Stripe for subscription billing.
- S3-compatible object storage.

## House rules

- Keep handlers thin. Put business rules in services and SQL in repositories.
- Keep product access provider-agnostic. Stripe or Polar updates local subscription state. `internal/access` decides what that state unlocks.
- Templates may mirror access for presentation. Services must enforce every protected operation with explicit early returns.
- Enforce resource, seat, and quota increments in the same database transaction as the protected write.
- Use `$1, $2` SQL placeholders. sqlx rebinds them for the selected database.
- Prefer explicit constructor injection and direct control flow.
- Add dependencies only when the standard library does not solve the problem well.
- Keep secrets out of templates. Request context contains only `config.RenderConfig`, the small public value templates need.
- Keep brand identity in `internal/brand` and architecture choices in Go source. Env is only for deployment coordinates, external IDs, operator switches, and secrets.
- Never edit generated `*_templ.go` files. Edit `.templ` files and let `task dev` regenerate them.

## Architecture

```text
HTTP request
  -> Handler
  -> Service
  -> Repository
  -> Database
```

Application wiring lives in `internal/app/app.go`. Route registration and middleware order live in `internal/routes/routes.go`.
Full configuration stays in startup wiring. Do not put secrets, database settings, operator allowlists, or trusted proxy ranges in request context.
Tenant-owned data is scoped by `workspace_id`. Load workspace context with `middleware.WithWorkspace` and enforce capabilities with `WorkspaceMember.Can`.

## JSON API

- JSON handlers live in `internal/api` and call the same services as the HTML handlers. Never duplicate a rule or a permission check there.
- Every route under `/api/v1` is listed once in `api.Routes` in `internal/api/routes.go`. `internal/api/openapi.yaml` documents each one and is served at `GET /api/v1/openapi.yaml`. `internal/api/openapi_test.go` fails when the two drift.
- People sign in in the browser, with GitHub through the moin GitHub App or with a passkey, and get the `session_token` cookie; a hub without the app takes a recovery key from `./main recovery-key <email>`. Cookie requests that change state send `X-CSRF-Token` with the token from `GET /api/v1/csrf`. The CLI, the TUI and AI agents are connections (`at_...` access tokens from `POST /api/v1/auth/token`); CI sends a CI token (`pat_...`); both as `Authorization: Bearer`. A session is never a bearer token.
- A connection is a client the person let in from the browser (`service.ConnectionService`), a CI token one made by hand (`service.CITokenService`); both act with rights: read, deploy, env, delete, servers (`model.Rights`). Each route in `api.Routes` names the right it needs, or `api.NoToken` to keep them out; that table is the one place, and `content/docs/reference/rights.md` and the `bearerAuth` roles in `openapi.yaml` are checked against it. A right a handler can only tell from the body, like SQL that writes, is asked with `api.Allowed`. Rights never add to a role. A connection or CI token is bound to one workspace; `WithWorkspaceAPI` answers 404 for every other workspace.
- `POST /api/v1/auth/token`, `/api/v1/auth/revoke` and `/api/v1/auth/register` follow OAuth (RFC 6749, 8628, 7009, 7591): forms in (registration is JSON), `{"error", "error_description"}` out, not the envelope.
- Every non-2xx answer is the envelope `{"error":{"code","message","fields"}}`. Codes are stable lowercase snake case. `fields` appears only for `validation_failed`.
- `CORS_ALLOWED_ORIGINS` lists the browser origins that may call `/api/` with credentials. Origins match exactly, without wildcards. The public OAuth endpoints (the `/.well-known` metadata, `/api/v1/auth/register` and `/api/v1/auth/token`) answer every origin with `*` and never with credentials, for MCP clients in a web page (`middleware.CORS`).
- Email change emails link to the HTML route under `APP_URL/auth/...`. An API client that receives the token another way calls `POST /api/v1/auth/email-change/verify`.

## MCP server

- `internal/mcp` serves an MCP server at `/mcp`: Streamable HTTP, stateless, `POST` only. It is a third transport next to the HTML handlers and the JSON API. Tools call the same services and never repeat a rule or a permission check.
- AI agents authenticate as a connection or with a CI token, `Authorization: Bearer`. `/mcp` answers 401 `unauthenticated` to sessions, cookies, and unknown, removed or expired tokens, and 403 `app_only` to a CI token for one app.
- Built-in tools: `whoami`, `list_workspaces`, `get_workspace`, `list_activity`, `get_billing`, `invite_member`, `resend_invitation`, `revoke_invitation`, `rename_workspace`, `set_email_alerts`. `registerTools` in `internal/mcp/tools.go` registers them in one place.
- Register every tool with `mcp.Add` and the right it needs. Add ends the description with "Needs the deploy right.", puts the rights in the tool's `_meta`, and answers `missing_right` without them. The Right column of the tools table in `content/docs/guides/mcp.md` is checked against it. Workspace tools take an optional `workspace` slug, default to the connection's workspace, and answer `not_found` for every other workspace.
- Failed calls return the JSON API error envelope as the tool result. Return service errors unchanged; `api.ErrorFor` maps them to the same codes as the API. Use `mcp.InvalidField` for a `validation_failed` field.
- Tools are a curated list for AI agents, not a copy of the JSON API. Never add tools for checkout, billing management, account or workspace deletion, role changes, member removal, admin, impersonation, sign-in, or email.
- `mcp.Services` in `internal/mcp/server.go` lists the services tools may call. Add your service there and pass it where `internal/routes/routes.go` calls `mcp.NewServer`.
- Raise `mcp.Version` when the tool list or a tool's schema changes.

Add a tool next to its service. This example is tested in `internal/mcp/example_test.go`. Inside `internal/mcp`, drop the `mcp.` prefixes and call it from `registerTools`:

```go
type nameInput struct {
	Name string `json:"name" jsonschema:"the new display name"`
}

func registerNameTool(server *mcpsdk.Server, services mcp.Services) {
	mcp.Add(server, &mcpsdk.Tool{
		Name:        "set_display_name",
		Description: "Changes the display name of the user this connection acts for.",
	}, model.RightServers, func(ctx context.Context, caller mcp.Caller, input nameInput) (api.UserResponse, error) {
		err := validation.ValidateName(input.Name)
		if err != nil {
			return api.UserResponse{}, mcp.InvalidField("name", api.FieldMessage(err))
		}
		user, err := services.Users.UpdateName(caller.User.ID, input.Name)
		if err != nil {
			return api.UserResponse{}, err
		}
		return api.UserResponse{User: api.NewUser(user)}, nil
	})
}
```

The description tells the AI agent what the tool returns or changes; Add says the right. Add a test with the in-memory transport like `TestAddedToolRunsWithItsRight`.

Client setup: the `/mcp` URL is all an MCP client needs. Its 401 carries `WWW-Authenticate: Bearer resource_metadata=...` (RFC 9728), the client finds the hub's OAuth metadata (RFC 8414), registers itself (RFC 7591, `POST /api/v1/auth/register`), sends the person to `/oauth/authorize`, the approval page of `moin login`, and trades the code with PKCE S256 at `POST /api/v1/auth/token`. The result is a connection like any other, bound to `/mcp` (`service.OAuthService`). Clients without OAuth send a CI token as `Authorization: Bearer pat_...`. The steps per client are in `content/docs/guides/mcp.md`.

## Commands

- `task dev` runs templ and the application.
- `task dev` selects available app and templ proxy ports automatically. Pass `APP_PORT`, `DEV_PORT`, or `OPEN_BROWSER=true` to override its development defaults.
- No foreign UI library, not even for icons or charts: everything comes from moin UI (`../ui`, shown at `/ui`). Icons are SVG files in `../ui/icons`, drawn on one 24 grid with `currentColor`; only brand marks keep their official shapes.
- `go test ./...` runs the test suite.
- `go test -race ./...` checks concurrency behavior.
- `go vet ./...` and `go build ./...` run static and build checks.

## Code map

- `cmd/server` contains application startup and graceful shutdown.
- `internal/config` owns typed environment configuration.
- `internal/handler` parses requests and renders responses.
- `internal/handler/htmx.go` contains the selected frontend's native response behavior.
- `internal/api` parses JSON requests and writes JSON responses.
- `internal/mcp` serves the MCP tools for AI agents.
- `internal/service` owns business rules.
- `internal/repository` owns raw SQL.
- `internal/ui` has three flat folders, each answering one question. `components/`: the Go side of moin UI (`../ui` at the repo root), one templ function per component, printing exactly its example; it knows looks, not moin's data. `layouts/`: what surrounds every page (the HTML frame, the app bar and palette, the site's navbar, footer and search). `pages/`: what one page shows, one file per page or topic. A piece goes to `components/` once two pages need it, to `layouts/` if it surrounds every page, else into its page. A test keeps every page on the components. No fourth folder without a good reason, no subfolders.
- `internal/db/migrations` contains the Goose schema.
- `internal/service/payment` contains the selected billing adapter.
- `internal/access` contains signup policy, plans, features, limits, quotas, and lifecycle rules.
- `internal/service/mailer` contains the selected mail adapter.
- `internal/service/workspace.go` owns workspaces, roles, invitations, seats, and the activity.
- `internal/storage` and `internal/service/file.go` own file storage.
- `content/` is embedded in the binary (`content.FS`), read from disk in development. The catalog is `catalog/` at the root of the repo, built into the hub and the daemon. `content/docs` and `internal/service/docs.go` contain the Docs module.
- `/ui` is the website of moin UI: `ui/docs` and `ui/blocks` at the repo root, `internal/handler/ui.go`, `internal/ui/pages/ui.templ`.

## Included product surface
This project includes sign-in with GitHub through the moin GitHub App and with passkeys, one-time recovery keys from a server command, revocable database sessions, account settings, legal pages, SEO basics, security headers, structured logging, subscription billing, provider-agnostic product access, and a global operator Admin panel.
It also includes workspaces with Owner, Admin, and Member roles, invitations, seat limits, workspace billing, and the activity.
