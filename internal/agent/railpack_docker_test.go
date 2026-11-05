//go:build docker

package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/axadrn/moin/protocol"
)

// TestRailpackDocker builds tiny Node and Python apps without a Dockerfile
// the way a deploy does: generate, railpack prepare, compose config, finish
// with a secret, compose build with the env, then runs the image on PORT
// 8080 and asks it. It needs docker and MOIN_RAILPACK_BINARY.
//
//	MOIN_RAILPACK_BINARY=/path/to/railpack go test -tags docker -run RailpackDocker ./internal/agent/
func TestRailpackDocker(t *testing.T) {
	if os.Getenv("MOIN_RAILPACK_BINARY") == "" {
		t.Skip("set MOIN_RAILPACK_BINARY to a railpack " + railpackVersion + " binary")
	}
	apps := map[string]map[string]string{
		"node": {
			"package.json": `{"name":"hello","version":"1.0.0","scripts":{"start":"node server.js"}}`,
			"server.js":    `require('http').createServer((q, s) => s.end('hello node')).listen(process.env.PORT)`,
		},
		"python": {
			"requirements.txt": "",
			"main.py":          "import http.server, os\nclass H(http.server.BaseHTTPRequestHandler):\n    def do_GET(self):\n        self.send_response(200); self.end_headers(); self.wfile.write(b'hello python')\nhttp.server.HTTPServer(('', int(os.environ['PORT'])), H).serve_forever()\n",
		},
	}
	for name, files := range apps {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
			defer cancel()
			release := t.TempDir()
			src := filepath.Join(release, "src")
			os.MkdirAll(src, 0o755)
			for file, body := range files {
				os.WriteFile(filepath.Join(src, file), []byte(body), 0o644)
			}
			a := newApps(t.TempDir(), func(protocol.MessageType, string, any) {})
			app := "rptest-" + name
			env := map[string]string{"BUILD_TOKEN": "s3cr3t-value"}
			config, detected, err := a.resolve(ctx, app, src, env)
			if err != nil {
				t.Fatalf("resolve: %v", err)
			}
			if detected.Kind != "railpack" || detected.Builder == "" || detected.Port != railpackPort {
				t.Fatalf("detected = %+v", detected)
			}
			t.Logf("Railpack: %s", detected.Builder)
			composeFile := filepath.Join(release, "compose.json")
			start := protocol.DeployStart{App: app, Number: 1, Env: env}
			err = finish(config, start, detected, nil, composeFile)
			if err != nil {
				t.Fatal(err)
			}
			// The container's environment holds the values, as for every app;
			// the build and its secrets only name them.
			body, _ := os.ReadFile(composeFile)
			var written map[string]any
			json.Unmarshal(body, &written)
			build, _ := json.Marshal(written["services"].(map[string]any)["web"].(map[string]any)["build"])
			secrets, _ := json.Marshal(written["secrets"])
			if strings.Contains(string(build)+string(secrets), "s3cr3t") || !strings.Contains(string(secrets), "BUILD_TOKEN") {
				t.Fatalf("build %s, secrets %s", build, secrets)
			}
			var log strings.Builder
			logf := func(format string, args ...any) { fmt.Fprintf(&log, format+"\n", args...) }
			err = a.run(ctx, logf, buildEnv(env), "compose", "-p", project(app), "-f", composeFile, "--progress", "plain", "build")
			if err != nil {
				t.Fatalf("build: %v\n%s", err, tail(log.String(), 40))
			}
			image := fmt.Sprintf("%s-web:1", project(app))
			t.Cleanup(func() { exec.Command("docker", "image", "rm", "-f", image).Run() })
			container := "moin-rptest-" + name
			exec.Command("docker", "rm", "-f", container).Run()
			out, err := exec.Command("docker", "run", "-d", "--name", container, "-e", "PORT=8080", "-p", "127.0.0.1::8080", image).CombinedOutput()
			if err != nil {
				t.Fatalf("run: %v %s", err, out)
			}
			t.Cleanup(func() { exec.Command("docker", "rm", "-f", container).Run() })
			port, err := exec.Command("docker", "port", container, "8080/tcp").Output()
			if err != nil {
				t.Fatal(err)
			}
			address := strings.TrimSpace(strings.Split(string(port), "\n")[0])
			var got string
			for i := 0; i < 60; i++ {
				response, err := http.Get("http://" + address + "/")
				if err == nil {
					text, _ := io.ReadAll(response.Body)
					response.Body.Close()
					if response.StatusCode == http.StatusOK {
						got = string(text)
						break
					}
				}
				time.Sleep(time.Second)
			}
			if got != "hello "+name {
				logs, _ := exec.Command("docker", "logs", container).CombinedOutput()
				t.Fatalf("GET / = %q; logs:\n%s", got, logs)
			}
		})
	}
}

func tail(text string, n int) string {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
