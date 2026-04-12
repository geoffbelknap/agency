package authz

type Resolver struct{}

func (r Resolver) Resolve(req Request) (Decision, error) {
	for _, grant := range req.Grants {
		if grant.Subject != req.Subject || grant.Target != req.Target {
			continue
		}
		if !contains(grant.Actions, req.Action) {
			continue
		}
		if grant.Consent != nil && contains(grant.Consent.RequiredFor, req.Action) && !req.ConsentProvided {
			return Decision{
				Allow:         false,
				ConsentNeeded: true,
				Reasons:       []string{"consent required"},
			}, nil
		}
		return Decision{Allow: true}, nil
	}
	return Decision{Allow: false, Reasons: []string{"no matching grant"}}, nil
}

func contains(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
