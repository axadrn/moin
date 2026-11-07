package service

import "github.com/axadrn/moin/hub/internal/repository"

type UserService struct {
	userRepository   *repository.Users
	fileService      *FileService
	emailService     *EmailService
	workspaceService *WorkspaceService
}

func NewUserService(users *repository.Users, files *FileService, email *EmailService, workspaces *WorkspaceService) *UserService {
	return &UserService{userRepository: users, fileService: files, emailService: email, workspaceService: workspaces}
}
