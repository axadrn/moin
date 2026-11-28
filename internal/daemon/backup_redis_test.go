package daemon

import (
	"errors"
	"testing"
)

func TestRedisArg(t *testing.T) {
	args := []string{"redis-server", "--appendonly", "yes", "--requirepass", "s3cret", "--port"}
	for name, want := range map[string]string{"--requirepass": "s3cret", "--appendonly": "yes", "--port": "", "--save": ""} {
		if got := redisArg(args, name); got != want {
			t.Errorf("redisArg(%s) = %q, want %q", name, got, want)
		}
	}
	if got := redisArg(nil, "--port"); got != "" {
		t.Errorf("no command: %q", got)
	}
}

func TestRedisError(t *testing.T) {
	for _, test := range []struct{ stderr, want string }{
		{"sending REPLCONF capa eof\nSYNC with master failed: -NOAUTH Authentication required.\n", "Redis did not let moin in: moin reads the password from --requirepass in the command of the service, or from REDISCLI_AUTH in its environment"},
		{"Could not connect to Redis at 127.0.0.1:6379: Connection refused\n", "redis-cli: Could not connect to Redis at 127.0.0.1:6379: Connection refused"},
		{"", "redis-cli: exit status 1"},
	} {
		if got := redisError(test.stderr, errors.New("exit status 1")).Error(); got != test.want {
			t.Errorf("redisError(%q) = %q, want %q", test.stderr, got, test.want)
		}
	}
}
