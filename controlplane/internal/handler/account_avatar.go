package handler

import (
	"github.com/axadrn/ops/controlplane/internal/ctxkeys"
	"github.com/axadrn/ops/controlplane/internal/ui/components/toast"
	"github.com/axadrn/ops/controlplane/internal/ui/layouts"
	"github.com/axadrn/ops/controlplane/internal/ui/pages"
	"github.com/axadrn/ops/controlplane/internal/validation"
	"log/slog"
	"net/http"
)

func (h *AccountHandler) UploadAvatar(w http.ResponseWriter, r *http.Request) {
	user := ctxkeys.User(r.Context())
	if err := r.ParseMultipartForm(10 << 20); err != nil {
		accountToast(w, r, toast.Toast(toast.Props{Title: "Error", Description: "Failed to parse form", Type: toast.TypeError}))
		return
	}

	file, header, err := r.FormFile("avatar")
	if err != nil {
		accountToast(w, r, toast.Toast(toast.Props{Title: "Error", Description: "No file uploaded", Type: toast.TypeError}))
		return
	}
	defer func() {
		if err := file.Close(); err != nil {
			slog.Error("failed to close file", "error", err)
		}
	}()

	if err := validation.ValidateFile(header, validation.ImageConstraints); err != nil {
		accountToast(w, r, toast.Toast(toast.Props{Title: "Error", Description: err.Error(), Type: toast.TypeError}))
		return
	}

	if err := h.fileService.DeleteUserAvatar(user.ID); err != nil {
		slog.Warn("failed to delete old avatar", "error", err, "user_id", user.ID)
	}
	if _, err := h.fileService.Upload(user.ID, "user", user.ID, "avatar", file, header, true); err != nil {
		slog.Error("failed to upload avatar", "error", err, "user_id", user.ID)
		accountToast(w, r, toast.Toast(toast.Props{Title: "Error", Description: "Failed to upload avatar", Type: toast.TypeError}))
		return
	}

	updatedUser, err := h.userService.ByID(user.ID)
	if err != nil {
		slog.Error("failed to reload user", "error", err, "user_id", user.ID)
	}
	accountToast(w, r, toast.Toast(toast.Props{Title: "Success", Description: "Avatar uploaded successfully", Type: toast.TypeSuccess}))
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
		accountToast(w, r, toast.Toast(toast.Props{Title: "Error", Description: "Failed to delete avatar", Type: toast.TypeError}))
		return
	}

	updatedUser, err := h.userService.ByID(user.ID)
	if err != nil {
		slog.Error("failed to reload user", "error", err, "user_id", user.ID)
	}
	accountToast(w, r, toast.Toast(toast.Props{Title: "Success", Description: "Avatar removed successfully", Type: toast.TypeSuccess}))
	if updatedUser != nil {
		renderFragment(w, r, pages.SettingsAvatarSection(updatedUser), "settings-avatar-display")
		renderFragment(w, r, pages.SettingsAvatarSection(updatedUser), "settings-avatar-form")
		render(w, r, layouts.SidebarIdentityUpdates(updatedUser))
	}
}
