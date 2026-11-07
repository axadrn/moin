package daemon

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/axadrn/moin/protocol"
)

// TestRailpackPrepareArgs: prepare learns the env names but never their
// values, except Railpack's own settings; names a secret cannot take stay
// out.
func TestRailpackPrepareArgs(t *testing.T) {
	env := map[string]string{
		"DATABASE_URL":       "postgres://user:hunter2@db/app",
		"STRIPE_KEY":         "sk_live_secret",
		"RAILPACK_START_CMD": "node server.js",
		"bad name":           "x",
	}
	args := railpackPrepareArgs("/rel/src", "/rel", env)
	joined := strings.Join(args, " ")
	for _, secret := range []string{"hunter2", "sk_live_secret", "bad name"} {
		if strings.Contains(joined, secret) {
			t.Errorf("args hold %q: %v", secret, args)
		}
	}
	for _, want := range []string{"prepare /rel/src", "--plan-out /rel/railpack-plan.json", "--info-out /rel/railpack-info.json", "--env DATABASE_URL=set", "--env STRIPE_KEY=set", "--env RAILPACK_START_CMD=node server.js"} {
		if !strings.Contains(joined, want) {
			t.Errorf("args lack %q: %v", want, args)
		}
	}
}

// TestRailpackSecrets: the build gets each env name as a secret read from
// the environment, and a hash that changes with the values; no value lands
// in the compose file.
func TestRailpackSecrets(t *testing.T) {
	build := func(env map[string]string) (map[string]any, string) {
		config := map[string]any{}
		service := map[string]any{"build": map[string]any{"args": map[string]any{"cache-key": "moin-shop"}}}
		railpackSecrets(config, service, env)
		config["services"] = map[string]any{"web": service}
		body, _ := json.Marshal(config)
		args := service["build"].(map[string]any)["args"].(map[string]any)
		hash, _ := args["secrets-hash"].(string)
		return config, string(body) + "|" + hash
	}
	_, one := build(map[string]string{"API_KEY": "first-value", "MODE": "a"})
	if strings.Contains(one, "first-value") {
		t.Fatalf("compose holds a value: %s", one)
	}
	for _, want := range []string{`"API_KEY":{"environment":"API_KEY"}`, `"secrets":["API_KEY","MODE"]`, `"cache-key":"moin-shop"`} {
		if !strings.Contains(one, want) {
			t.Errorf("compose lacks %s: %s", want, one)
		}
	}
	_, two := build(map[string]string{"API_KEY": "second-value", "MODE": "a"})
	if strings.Split(one, "|")[1] == strings.Split(two, "|")[1] {
		t.Error("secrets-hash must change with a value")
	}
	config := map[string]any{}
	service := map[string]any{"image": "moin-shop-web:2"}
	railpackSecrets(config, service, map[string]string{"A": "b"})
	if len(config) != 0 || service["build"] != nil {
		t.Errorf("a service not built gets no secrets: %v %v", config, service)
	}
	if env := buildEnv(map[string]string{"B": "2", "A": "1", "x y": "3"}); strings.Join(env, ",") != "A=1,B=2" {
		t.Errorf("buildEnv = %v", env)
	}
}

// TestRailpackSummary reads what prepare wrote about the repo.
func TestRailpackSummary(t *testing.T) {
	var info railpackInfoFile
	var plan railpackPlanFile
	json.Unmarshal([]byte(`{"success":true,"detectedProviders":["node"],"resolvedPackages":{"node":{"name":"node","resolvedVersion":"24.22.1"}},"logs":[{"Level":"warn","Msg":"No node package manager detected"}]}`), &info)
	json.Unmarshal([]byte(`{"deploy":{"startCommand":"npm run start"}}`), &plan)
	if got := railpackSummary(info, plan); got != "Node 24.22.1, start: npm run start" {
		t.Errorf("summary = %q", got)
	}
	json.Unmarshal([]byte(`{"success":true,"detectedProviders":["python"],"resolvedPackages":{"python":{"resolvedVersion":"3.13.2"}}}`), &info)
	if got := railpackSummary(info, railpackPlanFile{}); got != "Python 3.13.2" {
		t.Errorf("summary = %q", got)
	}
	var failed railpackInfoFile
	json.Unmarshal([]byte(`{"success":false,"logs":[{"Level":"info","Msg":"x"},{"Level":"error","Msg":"No start command was found."}]}`), &failed)
	if got := railpackErrors(failed); got != "No start command was found." {
		t.Errorf("errors = %q", got)
	}
}

// TestFinishGivesRailpackItsSecrets: finish hands the env to a Railpack
// build and to the running container, and leaves other kinds alone.
func TestFinishGivesRailpackItsSecrets(t *testing.T) {
	base := map[string]any{"services": map[string]any{"web": map[string]any{"build": map[string]any{"context": "/src", "args": map[string]any{"cache-key": "moin-shop"}}}}}
	file := t.TempDir() + "/compose.json"
	start := protocol.DeployStart{App: "shop", Number: 3, Env: map[string]string{"TOKEN": "abc"}}
	for kind, wantSecrets := range map[string]bool{"railpack": true, "dockerfile": false} {
		err := finish(base, start, protocol.Detected{Kind: kind, Service: "web", Port: 8080}, nil, file)
		if err != nil {
			t.Fatal(err)
		}
		var written map[string]any
		body, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		json.Unmarshal(body, &written)
		_, has := written["secrets"]
		if has != wantSecrets {
			t.Errorf("%s: secrets = %v:\n%s", kind, has, body)
		}
		if !strings.Contains(string(body), `"TOKEN": "abc"`) {
			t.Errorf("%s: the container lacks its env:\n%s", kind, body)
		}
	}
}
