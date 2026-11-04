package service

import (
	"errors"
	"strings"
	"testing"

	"github.com/axadrn/moin/controlplane/internal/model"
)

func serverNames(servers []model.Server) string {
	names := make([]string, 0, len(servers))
	for _, server := range servers {
		names = append(names, server.Name)
	}
	return strings.Join(names, ",")
}

func TestServersKeepTheirOrder(t *testing.T) {
	servers := newServerService(t)
	for _, name := range []string{"web", "db", "cache"} {
		if _, _, err := servers.Create(owner, name); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := servers.Create(other, "elsewhere"); err != nil {
		t.Fatal(err)
	}
	list, _ := servers.List(owner.WorkspaceID)
	if got := serverNames(list); got != "web,db,cache" {
		t.Fatalf("new servers go to the end: %s", got)
	}
	// By name or by ID.
	reordered, err := servers.Reorder(owner, []string{"cache", list[0].ID, " db "})
	if err != nil || serverNames(reordered) != "cache,web,db" {
		t.Fatalf("reorder = %s, %v", serverNames(reordered), err)
	}
	list, _ = servers.List(owner.WorkspaceID)
	if got := serverNames(list); got != "cache,web,db" {
		t.Fatalf("list after reorder = %s", got)
	}
	if _, _, err := servers.Create(owner, "new"); err != nil {
		t.Fatal(err)
	}
	list, _ = servers.List(owner.WorkspaceID)
	if got := serverNames(list); got != "cache,web,db,new" {
		t.Fatalf("a new server after a reorder = %s", got)
	}
	for _, order := range [][]string{
		{"cache", "web", "db"},
		{"cache", "web", "db", "db"},
		{"cache", "web", "db", "nope"},
		{"cache", "web", "db", "elsewhere"},
		nil,
	} {
		if _, err := servers.Reorder(owner, order); !errors.Is(err, ErrOrder) {
			t.Errorf("order %v: err = %v", order, err)
		}
	}
	other, _ := servers.List(other.WorkspaceID)
	if got := serverNames(other); got != "elsewhere" {
		t.Fatalf("another workspace = %s", got)
	}
}

func TestReorderRecordsOnce(t *testing.T) {
	servers := newServerService(t)
	for _, name := range []string{"web", "db"} {
		if _, _, err := servers.Create(owner, name); err != nil {
			t.Fatal(err)
		}
	}
	for range 2 {
		if _, err := servers.Reorder(owner, []string{"db", "web"}); err != nil {
			t.Fatal(err)
		}
	}
	events, err := servers.audit.ListByWorkspace(owner.WorkspaceID, 50)
	if err != nil {
		t.Fatal(err)
	}
	found := 0
	for _, event := range events {
		if event.Action == "servers.reordered" {
			found++
			if event.Summary() != "put the servers in the order db, web" {
				t.Errorf("summary = %q", event.Summary())
			}
		}
	}
	if found != 1 {
		t.Fatalf("%d reorders recorded, want 1: the same order again is no change", found)
	}
}

func TestMoved(t *testing.T) {
	order := []string{"a", "b", "c"}
	for _, test := range []struct {
		name string
		to   int
		want string
	}{
		{"b", 0, "b,a,c"},
		{"a", 1, "b,a,c"},
		{"a", 9, "b,c,a"},
		{"c", -1, "c,a,b"},
		{"x", 0, "a,b,c"},
	} {
		if got := strings.Join(Moved(order, test.name, test.to), ","); got != test.want {
			t.Errorf("Moved(%s, %d) = %s, want %s", test.name, test.to, got, test.want)
		}
	}
	if strings.Join(order, ",") != "a,b,c" {
		t.Errorf("Moved changed its input: %v", order)
	}
}
