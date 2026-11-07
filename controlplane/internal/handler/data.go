package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/axadrn/moin/controlplane/internal/ctxkeys"
	"github.com/axadrn/moin/controlplane/internal/repository"
	"github.com/axadrn/moin/controlplane/internal/service"
	"github.com/axadrn/moin/controlplane/internal/ui/pages"
	"github.com/axadrn/moin/protocol"
)

const dataPageRows = 50

// Data is the Data section of an app, read live from the server: its
// databases, one database with its tables and backups (?db=), or the rows
// of one table (?db=&table=). A table without a database, an older link,
// is in the first database.
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
	view := pages.DataView{App: *app, Databases: databases, Limit: dataPageRows, Now: time.Now()}
	query := r.URL.Query()
	database, table := query.Get("db"), query.Get("table")
	if !slices.ContainsFunc(databases, func(d protocol.Database) bool { return d.Path == database }) {
		database = ""
		if table != "" && len(databases) > 0 {
			database = databases[0].Path
		}
	}
	view.Backups, err = h.backupsView(r, app, database != "" && table == "")
	if err != nil {
		renderError(w, "load backups failed", err)
		return
	}
	if database == "" {
		view.Status, err = h.apps.BackupStatus(workspace.ID, app.ID)
		if err != nil {
			renderError(w, "load backup status failed", err)
			return
		}
		render(w, r, pages.AppData(view))
		return
	}
	view.Database = database
	if table == "" {
		tables, err := h.apps.Tables(r.Context(), workspace.ID, app.ID, database)
		if err != nil {
			view.Error = errorMessage(err)
		}
		view.Tables, view.Migration = tables.Tables, tables.Migration
		render(w, r, pages.AppData(view))
		return
	}
	view.Offset, _ = strconv.Atoi(query.Get("offset"))
	rows, err := h.apps.Rows(r.Context(), workspace.ID, app.ID, database, table, view.Offset, view.Limit)
	if err != nil {
		view.Error = errorMessage(err)
		view.Table = &protocol.DBTable{Name: table}
		render(w, r, pages.AppData(view))
		return
	}
	view.Table, view.Result, view.Offset = &rows.Table, rows.Result, rows.Offset
	render(w, r, pages.AppData(view))
}

// DataCell saves one cell and answers it again, green when saved. Clearing
// a cell that was NULL keeps it NULL.
func (h *AppsHandler) DataCell(w http.ResponseWriter, r *http.Request) {
	ref, row := dataRow(r)
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
	err := h.apps.UpdateCell(r.Context(), actor(r), r.PathValue("app"), database, table, row, columnName, value)
	if err != nil {
		answerError(w, r, "Cell not saved", errorMessage(err))
		state = "failed"
	}
	render(w, r, pages.DataCell(dataPath, database, table, ref, column, shown, state))
}

// DataDelete deletes one row; the page drops it.
func (h *AppsHandler) DataDelete(w http.ResponseWriter, r *http.Request) {
	ref, row := dataRow(r)
	err := h.apps.DeleteRow(r.Context(), actor(r), r.PathValue("app"), r.FormValue("db"), r.FormValue("table"), row)
	if err != nil {
		failed(w, r, "Row not deleted", err, nil)
		return
	}
	// The row goes; the answer only carries the feedback.
	w.Header().Set("HX-Reswap", "delete")
	answerDone(w, r, ref.Label()+" deleted", "")
}

// dataRow reads the row a cell or delete names: a rowid, or the values of
// its primary key as JSON in key.
func dataRow(r *http.Request) (pages.DataRowRef, service.RowKey) {
	ref := pages.DataRowRef{RowID: r.FormValue("rowid"), Key: r.FormValue("key")}
	row := service.RowKey{}
	row.RowID, _ = strconv.ParseInt(ref.RowID, 10, 64)
	if ref.Key != "" {
		row.Key = map[string]string{}
		_ = json.Unmarshal([]byte(ref.Key), &row.Key)
	}
	return ref, row
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
		failed(w, r, "Row not added", err, nil)
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
		failed(w, r, "Row not added", err, nil)
		return
	}
	query := url.Values{}
	query.Set("db", database)
	query.Set("table", table)
	w.Header().Set("HX-Redirect", ctxkeys.AppBasePath(r.Context())+"/apps/"+r.PathValue("app")+"/data?"+query.Encode())
}
