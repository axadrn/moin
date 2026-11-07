package model

import "github.com/axadrn/moin/internal/link"

// Link is how a link names the workspace: <slug>-<id> (link.To).
func (w Workspace) Link() string { return link.To(w.Slug, w.ID) }

// Path is the address of the workspace in the web: /app/<link>.
func (w Workspace) Path() string { return "/app/" + w.Link() }

// Link is how a link names the app: <name>-<id>.
func (a App) Link() string { return link.To(a.Name, a.ID) }

// Link is how a link names the server: <name>-<id>.
func (s Server) Link() string { return link.To(s.Name, s.ID) }

// Link is how a link names the bucket: <name>-<id>.
func (b Bucket) Link() string { return link.To(b.Name, b.ID) }
