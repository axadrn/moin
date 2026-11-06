package pages

import "testing"

// A deploy log sorts into steps and folds, and a fold the stream left
// open goes on in the next chunk.
func TestLogFolderSortsStepsAndFolds(t *testing.T) {
	var folder LogFolder
	first := folder.Add([]string{"receiving source (4.1 MB)", "building web", "#1 [web 1/2] FROM golang", ""})
	if len(first.Items) != 3 || first.Items[0].Line == nil || first.Items[1].Line == nil || first.Items[2].Fold == nil {
		t.Fatalf("first chunk = %+v, want step, step, fold", first.Items)
	}
	fold := first.Items[2].Fold
	if fold.Step != "building" || fold.Count != 1 || foldLabel(*fold) != "1 build line" {
		t.Fatalf("fold = %+v (%s), want one build line", fold, foldLabel(*fold))
	}

	second := folder.Add([]string{"#2 [web 2/2] RUN go build", "#3 ERROR: go build failed", "starting web"})
	if second.Continue == nil || second.Continue.ID != fold.ID || len(second.Continue.Lines) != 2 || second.Continue.Count != 3 {
		t.Fatalf("continue = %+v, want two more lines in fold %d, three in all", second.Continue, fold.ID)
	}
	if kind := second.Continue.Lines[1].Kind; kind != "err" {
		t.Fatalf("error output kind = %q, want err", kind)
	}
	if len(second.Items) != 1 || second.Items[0].Line.Text != "starting web" || second.Items[0].Line.N != 7 {
		t.Fatalf("items = %+v, want the step starting web as line 7", second.Items)
	}

	end := folder.End(3, "succeeded", "")
	if len(end.Items) != 1 || end.Items[0].Line.Text != "v3 is live" || end.Items[0].Line.Kind != "ok" {
		t.Fatalf("end = %+v, want v3 is live", end.Items)
	}
	if again := folder.End(3, "succeeded", ""); len(again.Items) != 0 {
		t.Fatalf("end twice = %+v, want nothing: the log says it already", again.Items)
	}
}

// A step that names a long path shows it short.
func TestLogStepShortensLongPaths(t *testing.T) {
	line := logStep(1, "building web (/var/lib/moin/apps/shop/releases/12/source/Dockerfile)")
	if line.Text != "building web" || line.Path == "" || shortPath(line.Path) != "…/12/source/Dockerfile" {
		t.Fatalf("line = %+v, short %q", line, shortPath(line.Path))
	}
}
