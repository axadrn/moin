package service

import (
	"strconv"

	"github.com/axadrn/moin/hub/internal/model"
)

// SetPinned pins an app or unpins it. Lists show pinned apps above the
// others that run fine, for everyone in the workspace, on every surface.
func (s *AppService) SetPinned(actor Actor, idOrName string, on bool) (*model.App, error) {
	app, err := s.apps.ByIDOrNameForWorkspace(idOrName, actor.WorkspaceID)
	if err != nil {
		return nil, err
	}
	if app.Pinned == on {
		return app, nil
	}
	if err := s.apps.SetPinned(app.ID, on); err != nil {
		return nil, err
	}
	s.servers.record(actor, "app.pinned", "app", app.Name, map[string]string{"on": strconv.FormatBool(on)})
	s.notify(actor.WorkspaceID)
	return s.apps.ByID(app.ID)
}
