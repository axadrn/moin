package workspace

import "time"

// Billing is what Stripe says of the subscription of a workspace.
type Billing struct {
	// Status is trial, paid or ended.
	Status       string
	Customer     string
	Subscription string
	PaidUntil    time.Time
}

// ByStripeCustomer is the workspace of a customer in Stripe.
func (s *Store) ByStripeCustomer(customer string) (Workspace, error) {
	if customer == "" {
		return Workspace{}, ErrNotFound
	}
	return s.one(`SELECT `+columns+` FROM workspaces WHERE stripe_customer = ? ORDER BY created LIMIT 1`, customer)
}

// SetBilling keeps what Stripe says of a workspace's subscription.
func (s *Store) SetBilling(id string, b Billing) error {
	var paidUntil any
	if !b.PaidUntil.IsZero() {
		paidUntil = b.PaidUntil.Unix()
	}
	_, err := s.db.Exec(`UPDATE workspaces SET status = ?, stripe_customer = ?, stripe_subscription = ?, paid_until = ? WHERE id = ?`,
		b.Status, b.Customer, b.Subscription, paidUntil, id)
	return err
}
