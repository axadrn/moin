package storage

import (
	"errors"
	"io"
)

// ErrNoBucket is what a control plane without S3_BUCKET answers when a
// file would be stored, such as an avatar.
var ErrNoBucket = errors.New("no bucket for files: set S3_BUCKET and the other S3_ settings")

// None stands in for a bucket a self-hosted control plane does not have
// yet: nothing is stored and nothing is there to delete.
type None struct{}

func (None) Save(string, io.Reader) error { return ErrNoBucket }

func (None) Delete(string) error { return nil }

func (None) URL(string) string { return "" }
