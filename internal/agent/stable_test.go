package agent

import (
	"context"
	"strconv"
	"testing"
	"time"
)

// TestStable: a service that restarts once while its database starts
// passes once it settles; one that keeps restarting, or never runs, fails.
func TestStable(t *testing.T) {
	stableFor, startWindow, stableTick = 30*time.Millisecond, 200*time.Millisecond, 5*time.Millisecond
	defer func() { stableFor, startWindow, stableTick = 10*time.Second, 30*time.Second, time.Second }()
	a := &apps{}
	calls := 0
	settles := func() (bool, string) {
		calls++
		if calls < 4 {
			return true, "restarts " + strconv.Itoa(calls)
		}
		return true, "restarts 4"
	}
	if ok, err := a.stable(context.Background(), settles); !ok || err != nil {
		t.Errorf("settles: %v %v", ok, err)
	}
	loop := 0
	crashes := func() (bool, string) {
		loop++
		return loop%2 == 0, strconv.Itoa(loop)
	}
	if ok, _ := a.stable(context.Background(), crashes); ok {
		t.Error("a crash loop passed")
	}
	if ok, _ := a.stable(context.Background(), func() (bool, string) { return false, "" }); ok {
		t.Error("a service that never runs passed")
	}
}
