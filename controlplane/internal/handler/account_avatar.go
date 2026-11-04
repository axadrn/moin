package handler

import (
	"github.com/axadrn/moin/controlplane/internal/ctxkeys"
	"github.com/axadrn/moin/controlplane/internal/ui/layouts"
	"github.com/axadrn/moin/controlplane/internal/ui/pages"
	"github.com/axadrn/moin/controlplane/internal/validation"
	"log/slog"
	"net/http"
	"strings"
)

func (h *AccountHandler) UploadAvatar(w http.ResponseWriter, r *http.Request) {
	user := ctxkeys.User(r.Context())
	if err := r.ParseMultipartForm(10 << 20); err != nil {
		toastFailed(w, r, "Picture not uploaded", "The upload could not be read. Try again.")
		return
	}

	file, header, err := r.FormFile("avatar")
	if err != nil {
		toastFailed(w, r, "Picture not uploaded", "Choose an image first.")
		return
	}
	defer func() {
		if err := file.Close(); err != nil {
			slog.Error("failed to close file", "error", err)
		}
	}()

	if err := validation.ValidateFile(header, validation.ImageConstraints); err != nil {
		toastFailed(w, r, "Picture not uploaded", fileProblem(err))
		return
	}

	if err := h.fileService.DeleteUserAvatar(user.ID); err != nil {
		slog.Warn("failed to delete old avatar", "error", err, "user_id", user.ID)
	}
	if _, err := h.fileService.Upload(user.ID, "user", user.ID, "avatar", file, header, true); err != nil {
		slog.Error("failed to upload avatar", "error", err, "user_id", user.ID)
		toastFailed(w, r, "Picture not uploaded", "Something went wrong. Please try again.")
		return
	}

	updatedUser, err := h.userService.ByID(user.ID)
	if err != nil {
		slog.Error("failed to reload user", "error", err, "user_id", user.ID)
	}
	toastDone(w, r, "Picture saved", "Your team sees it next to your name.")
	if updatedUser != nil {
		renderFragment(w, r, pages.SettingsAvatarSection(updatedUser), "settings-avatar-display")
		renderFragment(w, r, pages.SettingsAvatarSection(updatedUser), "settings-avatar-form")
		render(w, r, layouts.SidebarIdentityUpdates(updatedUser))
	}
}

func (h *AccountHandler) DeleteAvatar(w http.ResponseWriter, r *http.Request) {
	user := ctxkeys.User(r.Context())
	if err := h.fileService.DeleteUserAvatar(user.ID); err != nil {
		slog.Error("failed to delete avatar", "error", err, "user_id", user.ID)
		toastFailed(w, r, "Picture not removed", "Something went wrong. Please try again.")
		return
	}

	updatedUser, err := h.userService.ByID(user.ID)
	if err != nil {
		slog.Error("failed to reload user", "error", err, "user_id", user.ID)
	}
	toastDone(w, r, "Picture removed", "Your initial shows instead.")
	if updatedUser != nil {
		renderFragment(w, r, pages.SettingsAvatarSection(updatedUser), "settings-avatar-display")
		renderFragment(w, r, pages.SettingsAvatarSection(updatedUser), "settings-avatar-form")
		render(w, r, layouts.SidebarIdentityUpdates(updatedUser))
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
