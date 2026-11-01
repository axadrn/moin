package api

import "github.com/axadrn/ops/controlplane/internal/model"

// avatarURL returns the public avatar URL of a user, or nil without one.
func avatarURL(user *model.User) *string {
	if user.AvatarURL == "" {
		return nil
	}
	url := user.AvatarURL
	return &url
}
