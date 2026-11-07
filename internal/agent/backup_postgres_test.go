package agent

import (
	"slices"
	"testing"
	"time"
)

func TestParseHistory(t *testing.T) {
	got := parseHistory("1\t0/3000148\tbefore 2026-11-07 13:00:00+00\n\n2\t0/5000000\tbefore 2026-11-07 14:30:00+00\n")
	if len(got) != 2 || got[1] != 0x3000148 || got[2] != 0x5000000 {
		t.Fatalf("history %v", got)
	}
	if lsn, ok := parseLSN("1/A0"); !ok || lsn != 1<<32|0xA0 {
		t.Fatalf("lsn %x", lsn)
	}
	if _, ok := parseLSN("nope"); ok {
		t.Fatal("read a bad lsn")
	}
}

func TestBaseStart(t *testing.T) {
	timeline, lsn, ok := baseStart("base_000000020000000100000003")
	if !ok || timeline != 2 || lsn != 1<<32|3<<24 {
		t.Fatalf("base start %d %x %v", timeline, lsn, ok)
	}
	if _, _, ok := baseStart("base_12"); ok {
		t.Fatal("read a short name")
	}
}

// TestPickBase: the newest base backup before the moment that lies on the
// way to the timeline live then.
func TestPickBase(t *testing.T) {
	at := func(hour int) time.Time { return time.Date(2026, 11, 7, hour, 0, 0, 0, time.UTC) }
	bases := []walgBackup{
		{name: "base_000000010000000000000002", at: at(8)},  // TL1, early
		{name: "base_000000010000000000000009", at: at(14)}, // TL1, after the branch at segment 5
		{name: "base_00000002000000000000000C", at: at(15)}, // TL2
	}
	// Timeline 1 at 16:00: the newest on TL1.
	if got := pickBase(bases, at(16), 1, nil); got != bases[1].name {
		t.Errorf("TL1 at 16: %s", got)
	}
	// Timeline 2 branched off TL1 at segment 5: the TL1 backup after the
	// branch is not on its way, the early one is; at 16 its own.
	parents := map[int]uint64{1: 5 << 24}
	if got := pickBase(bases, at(14), 2, parents); got != bases[0].name {
		t.Errorf("TL2 at 14: %s", got)
	}
	if got := pickBase(bases, at(16), 2, parents); got != bases[2].name {
		t.Errorf("TL2 at 16: %s", got)
	}
	// Timeline 3 branched off TL1 too, at segment 3: TL2's is not on its way.
	if got := pickBase(bases, at(16), 3, map[int]uint64{1: 3 << 24}); got != bases[0].name {
		t.Errorf("TL3 at 16: %s", got)
	}
	// Before every base backup: none.
	if got := pickBase(bases, at(7), 1, nil); got != "" {
		t.Errorf("at 7: %s", got)
	}
}

func TestMissingTables(t *testing.T) {
	if got := missingTables([]string{"public.a", "public.b"}, []string{"public.a"}); !slices.Equal(got, []string{"public.b"}) {
		t.Errorf("missing %v", got)
	}
	if got := missingTables(nil, splitList("")); len(got) != 0 {
		t.Errorf("missing %v", got)
	}
}

func TestWalgNames(t *testing.T) {
	if match := walgHistoryName.FindStringSubmatch("00000002.history.lz4"); match == nil || match[1] != "00000002" {
		t.Error("history name")
	}
	if match := walgSegmentName.FindStringSubmatch("000000010000000000000003.lz4"); match == nil {
		t.Error("segment name")
	}
	if walgSegmentName.MatchString("000000010000000000000003.partial.lz4") {
		t.Error("a partial segment counts")
	}
}

func TestBrokenFile(t *testing.T) {
	if !brokenFile("ERROR: 2026/11/07 12:11:32 Extraction error in part_001.tar.lz4: extractOne: tar extract failed: lz4: bad magic number") {
		t.Error("an extraction error is not broken")
	}
	if brokenFile("INFO: 2026/11/07 12:11:32 Finished extraction of part_001.tar.lz4") {
		t.Error("a finished file is broken")
	}
}
