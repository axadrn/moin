package cli

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// fakeServer puts an ssh in PATH that notes its arguments and its stdin
// and answers get to `here env get`, answer to anything else.
func fakeServer(t *testing.T, get, answer string) (runs, stdin string) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir := t.TempDir()
	runs, stdin = filepath.Join(dir, "runs"), filepath.Join(dir, "stdin")
	cat, err := exec.LookPath("cat")
	if err != nil {
		t.Skip("no cat")
	}
	tr, _ := exec.LookPath("tr")
	// The words come quoted, like 'env' 'get': runs keeps them without.
	fake := "#!/bin/sh\necho \"$@\" | " + tr + " -d \"'\" >> " + runs + "\n" + cat + " >> " + stdin + "\ncase \"$*\" in\n" +
		"*here*env*get*) printf '%b' '" + get + "' ;;\n" +
		"*) printf '%s' '" + answer + "' ;;\nesac\n"
	if err := os.WriteFile(filepath.Join(dir, "ssh"), []byte(fake), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	return runs, stdin
}

func readFile(t *testing.T, name string) string {
	t.Helper()
	body, _ := os.ReadFile(name)
	return string(body)
}

// TestEnvSetNeverTakesTheValueAsAnArgument: KEY=value on the command line
// would stay in the shell history, so moin refuses it and runs nothing.
func TestEnvSetNeverTakesTheValueAsAnArgument(t *testing.T) {
	runs, _ := fakeServer(t, "", "")
	err := envSet(context.Background(), []string{"shop", "API_KEY=hunter22", "-s", "prod"}, strings.NewReader(""), &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "never goes on the command line") || strings.Contains(err.Error(), "hunter22") {
		t.Fatalf("err = %v, want a refusal without the value", err)
	}
	if readFile(t, runs) != "" {
		t.Error("ssh ran")
	}
}

// TestEnvSetBeforeTheFirstDeploy: the value comes on stdin, goes to the
// server on stdin, and the app is known to run there from then on.
func TestEnvSetBeforeTheFirstDeploy(t *testing.T) {
	runs, stdin := fakeServer(t, "", `{"added":["API_KEY"],"replaced":[],"removed":[],"live":false}`)
	var out bytes.Buffer
	if err := envSet(context.Background(), []string{"shop", "API_KEY", "-s", "prod"}, strings.NewReader("hunter22\n"), &out); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, stdin); got != "hunter22" {
		t.Errorf("the server read %q, want the value without its line break", got)
	}
	if ran := readFile(t, runs); !strings.Contains(ran, "here env set shop API_KEY") || strings.Contains(ran, "hunter22") {
		t.Errorf("ssh ran %q", ran)
	}
	if !strings.Contains(out.String(), "first deploy") || strings.Contains(out.String(), "hunter22") {
		t.Errorf("out = %q", out.String())
	}
	if known := knownServers("shop"); !slices.Equal(known, []string{"prod"}) {
		t.Errorf("known servers = %v, want prod", known)
	}
}

// TestEnvPushShowsTheNamesAndAsks: push lists what changes by name, never
// a value, and without a terminal changes nothing unless --yes.
func TestEnvPushShowsTheNamesAndAsks(t *testing.T) {
	runs, stdin := fakeServer(t, `OLD_TOKEN="x"\nPORT="3000"\n`, `{"added":["API_KEY"],"replaced":[],"removed":["OLD_TOKEN"],"live":false}`)
	file := filepath.Join(t.TempDir(), "prod.env")
	text := "PORT=3000\nAPI_KEY=hunter22\n"
	os.WriteFile(file, []byte(text), 0o600)
	var out bytes.Buffer
	err := envPush(context.Background(), []string{"shop", file, "-s", "prod"}, strings.NewReader(""), &out)
	if err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("err = %v, want it to ask for --yes", err)
	}
	if shown := out.String(); !strings.Contains(shown, "API_KEY") || !strings.Contains(shown, "OLD_TOKEN") || strings.Contains(shown, "PORT") || strings.Contains(shown, "hunter22") {
		t.Errorf("shown = %q, want the changed names only", shown)
	}
	if strings.Contains(readFile(t, runs), "env set") {
		t.Fatal("it set the env without a yes")
	}
	if err := envPush(context.Background(), []string{"shop", file, "-s", "prod", "-y"}, strings.NewReader(""), &out); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, stdin); got != text {
		t.Errorf("the server read %q, want the file", got)
	}
}

// TestEnvOfAnAppNoServerRuns: without a terminal, moin names -s.
func TestEnvOfAnAppNoServerRuns(t *testing.T) {
	fakeServer(t, "", "")
	err := env(context.Background(), []string{"new-app"}, strings.NewReader(""), &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "-s") {
		t.Errorf("err = %v, want it to name -s", err)
	}
}

// TestCompose prints the file as it is, or as JSON.
func TestCompose(t *testing.T) {
	fakeServer(t, "", `{"app":"shop","release":3,"compose":"name: moin-shop\n"}`)
	var out bytes.Buffer
	if err := compose(context.Background(), []string{"shop", "-s", "prod"}, &out); err != nil {
		t.Fatal(err)
	}
	if out.String() != "name: moin-shop\n" {
		t.Errorf("out = %q, want the file alone", out.String())
	}
	out.Reset()
	if err := compose(context.Background(), []string{"shop", "-s", "prod", "--json"}, &out); err != nil || !strings.Contains(out.String(), `"release": 3`) {
		t.Errorf("json = %q, %v", out.String(), err)
	}
}

// TestRemoveAppAsksFirst: without a terminal it needs --yes; then the
// server removes it and moin forgets where it ran.
func TestRemoveAppAsksFirst(t *testing.T) {
	runs, _ := fakeServer(t, "", "")
	rememberApp("prod", "shop")
	err := removeApp(context.Background(), []string{"shop"}, strings.NewReader(""), &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "--yes") || strings.Contains(readFile(t, runs), "here rm") {
		t.Fatalf("err = %v, ran %q: want a question first", err, readFile(t, runs))
	}
	var out bytes.Buffer
	if err := removeApp(context.Background(), []string{"shop", "-y"}, strings.NewReader(""), &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(readFile(t, runs), "here rm shop") {
		t.Errorf("ssh ran %q", readFile(t, runs))
	}
	if !strings.Contains(out.String(), "volumes stay") {
		t.Errorf("out = %q, want it to say the volumes stay", out.String())
	}
	if known := knownServers("shop"); len(known) != 0 {
		t.Errorf("known servers = %v, want none", known)
	}
}
