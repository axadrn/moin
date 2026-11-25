package client

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// TestAStreamComesBackAfterReconnect: the hub ends a stream with its
// access token and the event reconnect. The client opens it again, and a
// deploy's lines, which the hub sends from the first again, print once.
func TestAStreamComesBackAfterReconnect(t *testing.T) {
	var mu sync.Mutex
	opened := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		opened[r.URL.Path]++
		count := opened[r.URL.Path]
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		switch r.URL.Path {
		case "/api/v1/apps/shop/deploys/1/events":
			fmt.Fprint(w, "event: line\ndata: {\"line\":\"one\"}\n\nevent: line\ndata: {\"line\":\"two\"}\n\n")
			if count == 1 {
				fmt.Fprint(w, "event: reconnect\ndata: {}\n\n")
				return
			}
			fmt.Fprint(w, "event: line\ndata: {\"line\":\"three\"}\n\nevent: done\ndata: {\"number\":1,\"status\":\"live\"}\n\n")
		case "/api/v1/apps/events":
			fmt.Fprintf(w, "event: apps\ndata: {\"items\":[{\"name\":\"app-%d\"}]}\n\n", count)
			if count == 1 {
				fmt.Fprint(w, "event: reconnect\ndata: {}\n\n")
			}
		}
	}))
	t.Cleanup(server.Close)
	api, err := New(Config{URL: server.URL, Token: "pat_test"})
	if err != nil {
		t.Fatal(err)
	}

	var lines []string
	deploy, err := api.FollowDeploy(context.Background(), "shop", 1, func(line string) { lines = append(lines, line) })
	if err != nil || deploy.Number != 1 || strings.Join(lines, ",") != "one,two,three" {
		t.Fatalf("FollowDeploy() = %+v, %v, lines %v", deploy, err, lines)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var names []string
	err = api.WatchApps(ctx, func(apps []App) {
		names = append(names, apps[0].Name)
		if len(names) == 2 {
			cancel()
		}
	})
	if strings.Join(names, ",") != "app-1,app-2" {
		t.Fatalf("WatchApps() = %v, names %v", err, names)
	}
}
