package access

var Product = Catalog{
	Signup: SignupPolicy{
		Mode:          SignupFreemium,
		TrialPlanID:   PlanPro,
		TrialDays:     7,
		TrialFallback: FallbackFree,
	},
	Lifecycle: LifecyclePolicy{
		PastDueGraceDays: 7,
		PaidFallback:     FallbackFree,
	},
	Plans: []Plan{
		{
			ID:          PlanFree,
			Name:        "Free",
			Description: "For looking around before you subscribe",
			Limits:      map[Limit]int{LimitServers: 0, LimitMembers: Unlimited},
			Benefits: []string{
				"Invite your team",
				"Community support",
			},
		},
		{
			ID:           PlanPro,
			Name:         "ops",
			Description:  "Deploys, backups and data for your servers",
			MonthlyPrice: 2900,
			YearlyPrice:  29000,
			Limits:       map[Limit]int{LimitServers: Unlimited, LimitMembers: Unlimited},
			Benefits: []string{
				"Unlimited servers and apps",
				"Your whole team, no seat price",
				"Continuous backups with daily restore drills",
				"Data browser, SQL and the GitHub App",
				"Monthly, or yearly with two months free",
			},
			Purchasable: true,
		},
	},
}

// SelfHosted is the catalog of a control plane with billing off: one free
// plan with everything.
var SelfHosted = Catalog{
	Signup:    SignupPolicy{Mode: SignupFreemium},
	Lifecycle: LifecyclePolicy{PaidFallback: FallbackFree},
	Plans: []Plan{{
		ID:          PlanFree,
		Name:        "Self-hosted",
		Description: "Everything, on your own control plane",
		Limits:      map[Limit]int{LimitServers: Unlimited, LimitMembers: Unlimited},
	}},
}

type Catalog struct {
	Signup    SignupPolicy
	Lifecycle LifecyclePolicy
	Plans     []Plan
}

func PlanByID(planID string) (Plan, bool) {
	for _, plan := range Product.Plans {
		if plan.ID == planID {
			return plan, true
		}
	}
	return Plan{}, false
}
