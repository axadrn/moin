package protocol

import "time"

// The env values of an app live only on the server, where the CLI puts
// them over SSH: <state>/apps/<app>/app.env, 0600 (EnvSet).

// EnvSet sets the env values of Set and removes the keys of Remove in the
// app's app.env. Every compose up of the app reads that one file, so a
// change reaches the containers with the next deploy, restart or start.
type EnvSet struct {
	App    string            `json:"app"`
	Set    map[string]string `json:"set,omitempty"`
	Remove []string          `json:"remove,omitempty"`
}

// AppEnv names the env keys an app has on the server, from its app.env,
// and when that file last changed: never a value. The server is the truth
// for them; the CLI shows what it says.
type AppEnv struct {
	Keys      []string  `json:"keys"`
	ChangedAt time.Time `json:"changed_at"`
}
