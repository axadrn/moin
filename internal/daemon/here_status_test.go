package daemon

import (
	"strings"
	"testing"
	"time"

	"github.com/axadrn/moin/protocol"
)

// An app with a database and no bucket has no backup yet. With a bucket,
// a copy that did not restore is a need, with the last copy that did.
func TestBackupNeeds(t *testing.T) {
	shop := map[string][]protocol.DatabaseBackup{"shop": {{Database: "data/app.db"}}, "site": nil}
	needs := backupNeeds(Config{Name: "prod"}, shop)
	if len(needs) != 1 || needs[0].Text != "shop: no backup yet." || needs[0].Command != "moin bucket set prod" {
		t.Fatalf("no bucket: %+v", needs)
	}
	withBucket := Config{Name: "prod", Bucket: &Bucket{}}
	if needs := backupNeeds(withBucket, shop); len(needs) != 0 {
		t.Fatalf("a bucket and no failed check: %+v", needs)
	}
	proven := protocol.NewCopyID(time.Date(2026, 11, 26, 3, 0, 0, 0, time.UTC))
	failed := map[string][]protocol.DatabaseBackup{"shop": {{Database: "data/app.db", CheckError: "no such table: users", CheckedCopy: proven}}}
	needs = backupNeeds(withBucket, failed)
	if len(needs) != 1 || needs[0].Text != "shop: today's backup did not restore. Last proven copy: Nov 26 03:00 UTC." || needs[0].Command != "moin backups shop" {
		t.Fatalf("a failed check: %+v", needs)
	}
	failed["shop"][0].CheckedCopy = ""
	if needs := backupNeeds(withBucket, failed); len(needs) != 1 || !strings.HasSuffix(needs[0].Text, "Last proven copy: none yet.") {
		t.Fatalf("never proven: %+v", needs)
	}
}
