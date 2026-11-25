package service

import "errors"

var errBearersNotConfigured = errors.New("connections and ci tokens are not configured")

// UseBearers lets RevokeAllAccess end the connections and CI tokens of a
// user. The application wires it once at startup.
func (s *AuthService) UseBearers(connections *ConnectionService, ciTokens *CITokenService) {
	s.connections, s.ciTokens = connections, ciTokens
}

// RevokeAllAccess ends every session, connection and CI token of the user.
// The admin kill switch calls it. Signing out everywhere ends sessions only
// and keeps the rest.
func (s *AuthService) RevokeAllAccess(userID string) error {
	err := s.RevokeAllSessions(userID)
	if err != nil {
		return err
	}
	if s.connections == nil || s.ciTokens == nil {
		return errBearersNotConfigured
	}
	if err := s.connections.EndAllForUser(userID); err != nil {
		return err
	}
	return s.ciTokens.DeleteAllForUser(userID)
}
