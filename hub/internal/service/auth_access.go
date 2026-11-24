package service

import "errors"

var errAPITokensNotConfigured = errors.New("api token revocation is not configured")

// UseAPITokens lets RevokeAllAccess revoke the personal API tokens of a
// user. The application wires it once at startup.
func (s *AuthService) UseAPITokens(tokens *APITokenService) {
	s.apiTokens = tokens
}

// RevokeAllAccess ends every session and revokes every personal API token of
// the user. The admin kill switch calls it. Signing out
// everywhere ends sessions only and keeps the tokens.
func (s *AuthService) RevokeAllAccess(userID string) error {
	err := s.RevokeAllSessions(userID)
	if err != nil {
		return err
	}
	return s.revokeAPITokens(userID)
}

func (s *AuthService) revokeAPITokens(userID string) error {
	if s.apiTokens == nil {
		return errAPITokensNotConfigured
	}
	return s.apiTokens.RevokeAllForUser(userID)
}
