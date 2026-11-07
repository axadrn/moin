package protocol

// LogsRequest asks for the last Tail lines of every container of an app.
type LogsRequest struct {
	App  string `json:"app"`
	Tail int    `json:"tail"`
}

// LogsResult carries the lines, oldest first.
type LogsResult struct {
	Lines []string `json:"lines"`
}

// AppRemove names the app to remove.
type AppRemove struct {
	App string `json:"app"`
}

// AppStop names the app to stop.
type AppStop struct {
	App string `json:"app"`
}
