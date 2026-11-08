package model

// Link is how a link names the workspace: its slug.
func (w Workspace) Link() string { return w.Slug }

// Path is the address of the workspace in the web: /app/<slug>.
func (w Workspace) Path() string { return "/app/" + w.Link() }

// Link is how a link names the app: its name.
func (a App) Link() string { return a.Name }

// Link is how a link names the server: its name.
func (s Server) Link() string { return s.Name }

// Link is how a link names the bucket: its name.
func (b Bucket) Link() string { return b.Name }
