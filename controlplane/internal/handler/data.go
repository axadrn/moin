package handler

import (
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/axadrn/ops/controlplane/internal/ctxkeys"
	"github.com/axadrn/ops/controlplane/internal/repository"
	"github.com/axadrn/ops/controlplane/internal/ui/pages"
	"github.com/axadrn/ops/protocol"
)

const dataPageRows = 50

// Data is the data page of an app: its tables and a page of rows of one,
// read live from the server.
func (h *AppsHandler) Data(w http.ResponseWriter, r *http.Request) {
	workspace := ctxkeys.Workspace(r.Context())
	app, databases, err := h.apps.Databases(workspace.ID, r.PathValue("app"))
	if errors.Is(err, repository.ErrAppNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		renderError(w, "load databases failed", err)
		return
	}
	view := pages.DataView{App: *app, Databases: databases, Limit: dataPageRows}
	if len(databases) == 0 {
		render(w, r, pages.AppData(view))
		return
	}
	query := r.URL.Query()
	view.Database = databases[0].Path
	if slices.ContainsFunc(databases, func(d protocol.Database) bool { return d.Path == query.Get("db") }) {
		view.Database = query.Get("db")
	}
	tables, err := h.apps.Tables(r.Context(), workspace.ID, app.ID, view.Database)
	if err != nil {
		view.Error = errorMessage(err)
		render(w, r, pages.AppData(view))
		return
	}
	view.Tables, view.Migration = tables.Tables, tables.Migration
	view.Schema = query.Get("view") == "schema"
	if len(view.Tables) == 0 {
		render(w, r, pages.AppData(view))
		return
	}
	name := query.Get("table")
	if !slices.ContainsFunc(view.Tables, func(t protocol.DBTable) bool { return t.Name == name }) {
		name = view.Tables[0].Name
	}
	if view.Schema {
		index := slices.IndexFunc(view.Tables, func(t protocol.DBTable) bool { return t.Name == name })
		view.Table = &view.Tables[index]
		render(w, r, pages.AppData(view))
		return
	}
	view.Offset, _ = strconv.Atoi(query.Get("offset"))
	rows, err := h.apps.Rows(r.Context(), workspace.ID, app.ID, view.Database, name, view.Offset, view.Limit)
	if err != nil {
		view.Error = errorMessage(err)
		render(w, r, pages.AppData(view))
		return
	}
	view.Table, view.Result, view.Offset = &rows.Table, rows.Result, rows.Offset
	render(w, r, pages.AppData(view))
}

// DataCell saves one cell and answers it again, green when saved. Clearing
// a cell that was NULL keeps it NULL.
func (h *AppsHandler) DataCell(w http.ResponseWriter, r *http.Request) {
	rowID, _ := strconv.ParseInt(r.FormValue("rowid"), 10, 64)
	database, table, columnName := r.FormValue("db"), r.FormValue("table"), r.FormValue("column")
	text := r.FormValue("value")
	var value *string
	if text != "" || r.FormValue("was_null") == "" {
		value = &text
	}
	column := protocol.DBColumn{Name: columnName}
	dataPath := ctxkeys.AppBasePath(r.Context()) + "/apps/" + r.PathValue("app") + "/data"
	var shown any
	if value != nil {
		shown = *value
	}
	state := "saved"
	err := h.apps.UpdateCell(r.Context(), actor(r), r.PathValue("app"), database, table, rowID, columnName, value)
	if err != nil {
		h.toast(w, r, "Cell not saved", err)
		state = "failed"
	}
	render(w, r, pages.DataCell(dataPath, database, table, strconv.FormatInt(rowID, 10), column, shown, state))
}

// DataDelete deletes one row; the page drops it.
func (h *AppsHandler) DataDelete(w http.ResponseWriter, r *http.Request) {
	rowID, _ := strconv.ParseInt(r.FormValue("rowid"), 10, 64)
	err := h.apps.DeleteRow(r.Context(), actor(r), r.PathValue("app"), r.FormValue("db"), r.FormValue("table"), rowID)
	if err != nil {
		w.Header().Set("HX-Reswap", "none")
		h.toast(w, r, "Row not deleted", err)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// DataQuery runs the SQL console. Changes need the box ticked.
func (h *AppsHandler) DataQuery(w http.ResponseWriter, r *http.Request) {
	write := r.FormValue("write") == "1"
	result, err := h.apps.Query(r.Context(), actor(r), r.PathValue("app"), r.FormValue("db"), r.FormValue("sql"), nil, write, 200)
	if err != nil {
		render(w, r, pages.DataResult(result, write, errorMessage(err)))
		return
	}
	render(w, r, pages.DataResult(result, write, ""))
}

// DataInsert adds a row from the new row form, then shows the table again.
// Empty fields get the column's default.
func (h *AppsHandler) DataInsert(w http.ResponseWriter, r *http.Request) {
	err := r.ParseForm()
	if err != nil {
		h.toast(w, r, "Row not added", err)
		return
	}
	values := map[string]string{}
	for key, value := range r.PostForm {
		column, ok := strings.CutPrefix(key, "value:")
		if ok && len(value) > 0 && value[0] != "" {
			values[column] = value[0]
		}
	}
	database, table := r.FormValue("db"), r.FormValue("table")
	err = h.apps.InsertRow(r.Context(), actor(r), r.PathValue("app"), database, table, values)
	if err != nil {
		w.Header().Set("HX-Reswap", "none")
		h.toast(w, r, "Row not added", err)
		return
	}
	query := url.Values{}
	query.Set("db", database)
	query.Set("table", table)
	w.Header().Set("HX-Redirect", ctxkeys.AppBasePath(r.Context())+"/apps/"+r.PathValue("app")+"/data?"+query.Encode())
}
