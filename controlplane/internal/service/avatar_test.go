package service

import (
	"bytes"
	"errors"
	"mime/multipart"
	"net/http/httptest"
	"testing"

	"github.com/axadrn/moin/controlplane/internal/repository"
	"github.com/axadrn/moin/controlplane/internal/testutil"
)

// SetAvatar takes a picture and refuses any other file itself, so no
// transport can store one by skipping its own check.
func TestSetAvatarTakesOnlyPictures(t *testing.T) {
	database := testutil.NewDatabase(t)
	owner := testutil.SeedUser(t, database, testutil.UserOptions{ID: "avatar-owner"})
	files := repository.NewFiles(database)
	service := NewFileService(files, testutil.NewMemoryStorage())

	upload := func(name string, content []byte) (multipart.File, *multipart.FileHeader) {
		t.Helper()
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		part, err := writer.CreateFormFile("avatar", name)
		if err != nil {
			t.Fatal(err)
		}
		part.Write(content)
		writer.Close()
		request := httptest.NewRequest("POST", "/", &body)
		request.Header.Set("Content-Type", writer.FormDataContentType())
		file, header, err := request.FormFile("avatar")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { file.Close() })
		return file, header
	}

	file, header := upload("evil.png", []byte("#!/bin/sh\necho not a picture\n"))
	var invalid *InvalidFileError
	if err := service.SetAvatar(owner.User.ID, file, header); !errors.As(err, &invalid) {
		t.Fatalf("a script named .png: err = %v", err)
	}
	if stored, err := files.AllUserFiles(owner.User.ID); err != nil || len(stored) != 0 {
		t.Fatalf("a refused file was stored: %v, %v", stored, err)
	}

	png := append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 64)...)
	file, header = upload("me.png", png)
	if err := service.SetAvatar(owner.User.ID, file, header); err != nil {
		t.Fatalf("a picture: %v", err)
	}
	if stored, err := files.AllUserFiles(owner.User.ID); err != nil || len(stored) != 1 {
		t.Fatalf("the picture: %v, %v", stored, err)
	}
}
