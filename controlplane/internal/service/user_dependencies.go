package service

import "github.com/axadrn/moin/controlplane/internal/repository"

type UserService struct {
	userRepository   repository.UserRepository
	fileService      *FileService
	emailService     *EmailService
	workspaceService *WorkspaceService
}

func NewUserService(users repository.UserRepository, files *FileService, email *EmailService, workspaces *WorkspaceService) *UserService {
	return &UserService{userRepository: users, fileService: files, emailService: email, workspaceService: workspaces}
}
