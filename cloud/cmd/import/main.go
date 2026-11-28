// Command import brings the paying customers of the old service into moin
// cloud, once:
//
//	go run ./cloud/cmd/import <old.db> <new.db>
//
// It opens the old database read-only and takes only the workspaces that
// have a Stripe customer, a subscription, a coupon or an offer. Each keeps
// its old id, so the workspace_id in Stripe's metadata still matches, and
// gets the mail of its owner. It refuses, and imports nothing, when a
// paying workspace would be left out. An offer, the early backers' deal,
// keeps its days free and its coupon. A claimed one counts its days from
// when the old workspace was made, as the old service did. One nobody
// claimed yet becomes a workspace of its mail, and its days count from the
// first report, like everyone's. Running it twice adds nothing twice.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/axadrn/moin/cloud/internal/db"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: go run ./cloud/cmd/import <old.db> <new.db>")
		os.Exit(2)
	}
	result, err := importOld(context.Background(), os.Args[1], os.Args[2])
	if err != nil {
		fmt.Fprintln(os.Stderr, "import:", err)
		os.Exit(1)
	}
	fmt.Printf("%d workspaces imported, %d already in moin cloud by id or mail, %d offers never claimed imported as workspaces of their mail.\n",
		result.imported, result.sameMail, result.offers)
}

// result counts what the import did.
type result struct {
	imported, sameMail, offers int
}

// seconds is SQL that turns a time of the old service into Unix seconds. It
// wrote text like 2026-11-03 10:00:00, in UTC.
func seconds(column string) string {
	return "CASE WHEN " + column + " IS NULL THEN NULL WHEN typeof(" + column + ") = 'integer' THEN " + column +
		" ELSE unixepoch(substr(" + column + ", 1, 19)) END"
}

// kept are the old workspaces worth a place in moin cloud, each with the
// mail of its owner, paying ones first.
var kept = `
FROM old.workspaces w
JOIN old.users u ON u.id = (
	SELECT m.user_id FROM old.workspace_members m
	WHERE m.workspace_id = w.id AND m.role = 'owner' ORDER BY m.created_at LIMIT 1)
LEFT JOIN old.subscriptions s ON s.workspace_id = w.id
LEFT JOIN old.offers o ON lower(trim(o.email)) = lower(trim(u.email))
WHERE s.provider_customer_id IS NOT NULL OR s.provider_subscription_id IS NOT NULL
	OR s.coupon_id IS NOT NULL OR o.email IS NOT NULL`

// importOld copies the workspaces of the old database at oldPath into the
// new one at newPath, all or nothing.
func importOld(ctx context.Context, oldPath, newPath string) (result, error) {
	var r result
	if _, err := os.Stat(oldPath); err != nil {
		return r, err
	}
	database, err := db.Open(newPath)
	if err != nil {
		return r, err
	}
	defer database.Close()
	// ATTACH holds for one connection only.
	conn, err := database.Conn(ctx)
	if err != nil {
		return r, err
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, `ATTACH DATABASE ? AS old`, "file:"+oldPath+"?mode=ro"); err != nil {
		return r, fmt.Errorf("open %s read-only: %w", oldPath, err)
	}
	defer conn.ExecContext(context.Background(), `DETACH DATABASE old`)

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return r, err
	}
	defer tx.Rollback()
	var candidates int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) `+kept).Scan(&candidates); err != nil {
		return r, err
	}
	inserted, err := tx.ExecContext(ctx, `
INSERT INTO workspaces (id, mail, status, stripe_customer, stripe_subscription, paid_until, coupon, trial_days, trial_ends, created)
SELECT w.id, lower(trim(u.email)),
	CASE
		WHEN s.provider_subscription_id IS NOT NULL AND s.status IN ('active', 'past_due') THEN 'paid'
		WHEN s.provider_subscription_id IS NOT NULL AND s.status IN ('canceled', 'unpaid', 'incomplete_expired') THEN 'ended'
		ELSE 'trial'
	END,
	s.provider_customer_id, s.provider_subscription_id,
	CASE WHEN s.provider_subscription_id IS NOT NULL THEN `+seconds("s.current_period_end")+` END,
	coalesce(s.coupon_id, o.coupon_id),
	coalesce(o.trial_days, 30),
	CASE WHEN o.email IS NOT NULL THEN coalesce(`+seconds("w.created_at")+`, unixepoch()) + o.trial_days * 86400 END,
	coalesce(`+seconds("w.created_at")+`, unixepoch())
`+kept+`
ORDER BY s.provider_subscription_id IS NOT NULL DESC, w.created_at, w.id
ON CONFLICT DO NOTHING`)
	if err != nil {
		return r, err
	}
	n, _ := inserted.RowsAffected()
	r.imported = int(n)
	r.sameMail = candidates - r.imported
	// A paying workspace whose owner has another one is left out above:
	// moin cloud has one workspace per mail. Nobody may pay for nothing,
	// so then nothing is imported.
	var dropped string
	err = tx.QueryRowContext(ctx, `SELECT coalesce(group_concat(s.workspace_id, ', '), '') FROM old.subscriptions s
		WHERE s.provider_subscription_id IS NOT NULL AND s.status IN ('active', 'past_due', 'trialing')
		AND NOT EXISTS (SELECT 1 FROM workspaces n WHERE n.id = s.workspace_id AND n.stripe_subscription = s.provider_subscription_id)`).Scan(&dropped)
	if err != nil {
		return r, err
	}
	if dropped != "" {
		return r, fmt.Errorf("these paying workspaces share their owner's mail with another workspace, and moin cloud has one per mail: %s. Nothing was imported. Move each subscription to one workspace in Stripe first", dropped)
	}
	// The offers nobody claimed yet and that still hold, as the old
	// service let them be claimed.
	offers, err := tx.ExecContext(ctx, `
INSERT INTO workspaces (id, mail, coupon, trial_days, created)
SELECT lower(hex(randomblob(16))), lower(trim(o.email)), o.coupon_id, o.trial_days, coalesce(`+seconds("o.created_at")+`, unixepoch())
FROM old.offers o
WHERE o.claimed_at IS NULL AND (o.expires_at IS NULL OR `+seconds("o.expires_at")+` > unixepoch())
ORDER BY o.created_at, o.email
ON CONFLICT DO NOTHING`)
	if err != nil {
		return r, err
	}
	n, _ = offers.RowsAffected()
	r.offers = int(n)
	return r, tx.Commit()
}
