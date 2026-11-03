package tui

import (
	"strings"
	"testing"
)

// TestAppBucket: in the backups of an app, bucket <name> asks before it
// moves the app, and says the older backups stay; a snapshot with a bucket
// asks for the app's name like any restore. :bucket types the move in.
func TestAppBucket(t *testing.T) {
	m := frames()["detail-backups"]
	m.input = "bucket client-a"
	m = press(m, "enter")
	if m.mode != modeConfirm || !strings.Contains(m.confirm.question, "the bucket client-a") || !strings.Contains(m.confirm.detail, "older backups stay") {
		t.Errorf("bucket client-a: mode %v, confirm %+v, err %v", m.mode, m.confirm, m.err)
	}
	m = frames()["detail-backups"]
	m.input = "bucket -"
	if m = press(m, "enter"); m.mode != modeConfirm || !strings.Contains(m.confirm.question, "the default bucket") {
		t.Errorf("bucket -: mode %v, confirm %+v", m.mode, m.confirm)
	}
	m = frames()["detail-backups"]
	m.input = "20261031T143638Z-manual main"
	if m = press(m, "enter"); m.mode != modeConfirmName || !strings.Contains(m.confirm.question, "from the bucket main") {
		t.Errorf("snapshot in an older bucket: mode %v, confirm %+v, err %v", m.mode, m.confirm, m.err)
	}

	m = command(frames()["apps"], "bucket client-a")
	if m.mode != modeBackups || m.input != "bucket client-a" {
		t.Errorf(":bucket client-a: mode %v, input %q", m.mode, m.input)
	}
}

// TestSettingsBuckets: every bucket is a row; b adds one, enter changes
// one with the secret key optional, D asks before it makes one the
// default, and the default says so.
func TestSettingsBuckets(t *testing.T) {
	m := frames()["settings"]
	rows := m.settingRows()
	var names []string
	for _, row := range rows {
		names = append(names, row.name)
	}
	if !strings.Contains(strings.Join(names, ","), "Bucket default,Bucket client-a") {
		t.Fatalf("rows = %v", names)
	}
	at := func(name string) int {
		for i, row := range rows {
			if row.name == name {
				return i
			}
		}
		t.Fatalf("no row %q in %v", name, names)
		return 0
	}
	m.settingSelected = at("Bucket client-a")
	if next, _ := m.settingKey("D"); next.(model).mode != modeConfirm {
		t.Errorf("D on client-a: mode %v", next.(model).mode)
	}
	m.settingSelected = at("Bucket default")
	if next, _ := m.settingKey("D"); next.(model).mode == modeConfirm || !strings.Contains(next.(model).notice, "already") {
		t.Errorf("D on the default: mode %v, notice %q", next.(model).mode, next.(model).notice)
	}
	next, _ := m.settingKey("enter")
	edit := next.(model)
	if edit.mode != modeForm || edit.form.title != "Bucket default" || !edit.form.fields[6].optional {
		t.Errorf("edit form: mode %v, title %q", edit.mode, edit.form.title)
	}
	next, _ = m.settingKey("b")
	add := next.(model)
	if add.mode != modeForm || add.form.title != "New bucket" || add.form.fields[6].optional {
		t.Errorf("add form: mode %v, title %q", add.mode, add.form.title)
	}
	// Without a bucket the row adds the first, named default.
	empty := frames()["settings-empty"]
	for index, row := range empty.settingRows() {
		if row.kind == "buckets" {
			empty.settingSelected = index
		}
	}
	next, _ = empty.settingKey("enter")
	if first := next.(model); first.mode != modeForm || first.form.fields[0].value != "default" {
		t.Errorf("first bucket form: mode %v, fields %+v", first.mode, first.form.fields)
	}
}
