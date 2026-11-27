package cli

import (
	"reflect"
	"testing"
)

func TestLogLines(t *testing.T) {
	text := "web-1  | 2026-11-27T16:39:08.672243096Z hello | world\n" +
		"db-1   | 2026-11-27T16:39:09Z\n" +
		"no prefix at all\n"
	want := []logLine{
		{Time: "2026-11-27T16:39:08.672243096Z", Container: "web-1", Line: "hello | world"},
		{Time: "2026-11-27T16:39:09Z", Container: "db-1", Line: ""},
		{Line: "no prefix at all"},
	}
	if got := logLines(text); !reflect.DeepEqual(got, want) {
		t.Errorf("logLines =\n%+v\nwant\n%+v", got, want)
	}
	if got := logLines(""); got == nil || len(got) != 0 {
		t.Errorf("logLines of nothing = %#v, want an empty list for JSON []", got)
	}
}
