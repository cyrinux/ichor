package ichorgo

// metallbFindings are MetalLB's own troubles: a pool with no address left (critical while a
// Service waits), a pool nothing advertises, a refused configuration, a BGP session down.
func metallbFindings(in lbInputs, waiting bool) []checkupFinding {
	findings := []checkupFinding{}

	if in.metallb == nil {
		return findings
	}

	for _, p := range in.metallb.pools {
		row := metallbPoolRow(in, p, waiting)

		if row.Available == 0 {
			f := checkupFinding{Kind: findLBPoolExhausted, Severity: sevWarning, Name: row.Name, Value: float64(row.Total)}
			if waiting {
				f.Severity = sevCritical
			}

			findings = append(findings, f)
		}

		if len(row.AdvertisedBy) == 0 {
			findings = append(findings, checkupFinding{Kind: findLBPoolUnadvertised, Severity: sevWarning, Name: row.Name})
		}
	}

	for _, e := range in.metallb.configErrors() {
		findings = append(findings, checkupFinding{Kind: findLBConfigError, Severity: sevCritical, Name: e.Component, Node: e.Node, Message: e.Message})
	}

	for _, p := range lbPeerRows(in.metallb, in.sessions) {
		for _, s := range p.Sessions {
			if !s.Up {
				findings = append(findings, checkupFinding{Kind: findLBBGPPeerDown, Severity: sevCritical, Name: p.Name, Node: s.Node, Extra: p.PeerAddress})
			}
		}
	}

	return findings
}
