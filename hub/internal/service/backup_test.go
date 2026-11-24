package service

import (
	"errors"
	"testing"
)

func TestRefusePrivate(t *testing.T) {
	for address, refused := range map[string]bool{
		"127.0.0.1:443":       true,
		"[::1]:443":           true,
		"10.0.0.5:443":        true,
		"172.16.3.4:443":      true,
		"192.168.1.1:443":     true,
		"169.254.169.254:80":  true,
		"[fe80::1]:443":       true,
		"[fd00::1]:443":       true,
		"0.0.0.0:443":         true,
		"[::]:443":            true,
		"100.100.100.100:443": true,
		"[::ffff:10.0.0.1]:1": true,
		"203.0.113.5:443":     false,
		"[2606:4700::1]:443":  false,
	} {
		err := refusePrivate("tcp", address, nil)
		if errors.Is(err, ErrPrivateAddress) != refused {
			t.Errorf("%s: err = %v, want refused %v", address, err, refused)
		}
	}
}
