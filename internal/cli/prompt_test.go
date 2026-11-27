package cli

import (
	"errors"
	"os"
	"os/signal"
	"syscall"
	"testing"
	"time"
)

// TestReadLineStopsOnCtrlC: Ctrl-C ends a question that waits for its
// answer, without Enter.
func TestReadLineStopsOnCtrlC(t *testing.T) {
	// The test catches Ctrl-C too, so a signal that comes before readLine
	// listens does not end the test.
	caught := make(chan os.Signal, 1)
	signal.Notify(caught, os.Interrupt)
	defer signal.Stop(caught)
	never, typed, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer never.Close()
	defer typed.Close()
	got := make(chan error, 1)
	go func() {
		_, err := readLine(never)
		got <- err
	}()
	deadline := time.After(5 * time.Second)
	for {
		if err := syscall.Kill(os.Getpid(), syscall.SIGINT); err != nil {
			t.Fatal(err)
		}
		select {
		case err := <-got:
			if !errors.Is(err, errStopped) {
				t.Fatalf("err = %v, want errStopped", err)
			}
			return
		case <-deadline:
			t.Fatal("readLine still waits after Ctrl-C")
		case <-time.After(20 * time.Millisecond):
		}
	}
}

// TestReadLineReadsTheAnswer: without Ctrl-C, readLine gives the line.
func TestReadLineReadsTheAnswer(t *testing.T) {
	answer, typed, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer answer.Close()
	if _, err := typed.WriteString("  yes \n"); err != nil {
		t.Fatal(err)
	}
	typed.Close()
	if line, err := readLine(answer); err != nil || line != "yes" {
		t.Fatalf("readLine = %q, %v", line, err)
	}
}
