package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/axadrn/moin/hub/internal/ctxkeys"
	"github.com/axadrn/moin/hub/internal/service"
	"github.com/axadrn/moin/hub/internal/ui/layouts"
	"github.com/axadrn/moin/hub/internal/ui/pages"
)

func (h *AccountHandler) UploadAvatar(w http.ResponseWriter, r *http.Request) {
	user := ctxkeys.User(r.Context())
	if err := r.ParseMultipartForm(10 << 20); err != nil {
		answerFailed(w, r, "Picture not uploaded", "The upload could not be read. Try again.")
		return
	}

	file, header, err := r.FormFile("avatar")
	if err != nil {
		answerFailed(w, r, "Picture not uploaded", "Choose an image first.")
		return
	}
	defer func() {
		if err := file.Close(); err != nil {
			slog.Error("failed to close file", "error", err)
		}
	}()

	err = h.fileService.SetAvatar(user.ID, file, header)
	var invalid *service.InvalidFileError
	if errors.As(err, &invalid) {
		answerFailed(w, r, "Picture not uploaded", fileProblem(err))
		return
	}
	if err != nil {
		slog.Error("failed to upload avatar", "error", err, "user_id", user.ID)
		answerFailed(w, r, "Picture not uploaded", "Something went wrong. Please try again.")
		return
	}

	updatedUser, err := h.userService.ByID(user.ID)
	if err != nil {
		slog.Error("failed to reload user", "error", err, "user_id", user.ID)
	}
	answerDone(w, r, "Picture saved", "")
	if updatedUser != nil {
		renderFragment(w, r, pages.SettingsAvatarSection(updatedUser), "settings-avatar-display")
		renderFragment(w, r, pages.SettingsAvatarSection(updatedUser), "settings-avatar-form")
		render(w, r, layouts.IdentityUpdates(updatedUser))
	}
}

func (h *AccountHandler) DeleteAvatar(w http.ResponseWriter, r *http.Request) {
	user := ctxkeys.User(r.Context())
	if err := h.fileService.DeleteUserAvatar(user.ID); err != nil {
		slog.Error("failed to delete avatar", "error", err, "user_id", user.ID)
		answerFailed(w, r, "Picture not removed", "Something went wrong. Please try again.")
		return
	}

	updatedUser, err := h.userService.ByID(user.ID)
	if err != nil {
		slog.Error("failed to reload user", "error", err, "user_id", user.ID)
	}
	answerDone(w, r, "Picture removed", "")
	if updatedUser != nil {
		renderFragment(w, r, pages.SettingsAvatarSection(updatedUser), "settings-avatar-display")
		renderFragment(w, r, pages.SettingsAvatarSection(updatedUser), "settings-avatar-form")
		render(w, r, layouts.IdentityUpdates(updatedUser))
	}
}

// fileProblem is the reason a file was refused, as a sentence.
func fileProblem(err error) string {
	message := err.Error()
	if message == "" {
		return "Choose another image."
	}
	return strings.ToUpper(message[:1]) + message[1:] + "."
}
