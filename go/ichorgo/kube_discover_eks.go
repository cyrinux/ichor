package ichorgo

import (
	"context"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"
)

// EKS discovery with IAM Identity Center (like `aws configure sso`, then `aws eks
// list-clusters` in every region): StartDiscoverSignIn runs the device-code sign-in, the
// apps let the user pick among the accounts and roles it may assume (DiscoverSignInChoices),
// then DiscoverClusters reads the clusters of each chosen role in each region, a few at a
// time, with the role credentials the portal hands out (no STS). The imported contexts sign
// in with the same session and the role their cluster was found with (KubeSetCredentials).

const (
	// awsFieldRegions narrows an Identity Center discovery to these regions (commas or
	// spaces); empty is the Identity Center region and the partition's common regions.
	awsFieldRegions = "awsRegions"
	// awsFieldRoleSelection is the roles to read clusters with: "<accountId>/<roleName>",
	// several separated by commas or spaces.
	awsFieldRoleSelection = "awsRoleSelection"
	// eksParallelRegions is how many role × region listings run at once.
	eksParallelRegions = 8
	// eksMaxPages caps the pages of one region's cluster list.
	eksMaxPages = 20
)

// eksCommonRegions are the regions an Identity Center discovery reads when none are given.
var eksCommonRegions = []string{
	"us-east-1", "us-east-2", "us-west-2", "eu-west-1", "eu-west-2", "eu-west-3", "eu-central-1", "eu-north-1",
	"ap-southeast-1", "ap-southeast-2", "ap-northeast-1", "ap-south-1", "ca-central-1", "sa-east-1",
}

var eksChinaRegions = []string{"cn-north-1", "cn-northwest-1"}

// eksNeedsBrowser tells EKS discovery credentials that sign in at the access portal first.
func eksNeedsBrowser(s map[string]string) bool { return s[awsFieldSSOStartURL] != "" }

// eksDiscoveryRegions is the regions raw names, else the Identity Center region and its
// partition's common ones.
func eksDiscoveryRegions(raw, ssoRegion string) ([]string, error) {
	var out []string

	for _, r := range strings.FieldsFunc(raw, func(c rune) bool { return c == ',' || c == ' ' || c == '\n' || c == '\t' }) {
		if !kubeNamePattern.MatchString(r) {
			return nil, fmt.Errorf("%q is not an AWS region", r)
		}

		if !slices.Contains(out, r) {
			out = append(out, r)
		}
	}

	if len(out) > 0 {
		return out, nil
	}

	common := eksCommonRegions
	if strings.HasPrefix(ssoRegion, "cn-") {
		common = eksChinaRegions
	}

	out = append(out, ssoRegion)

	for _, r := range common {
		if r != ssoRegion {
			out = append(out, r)
		}
	}

	return out, nil
}

// eksSSOMethod is the Identity Center sign-in of a discovery: no kubeconfig, no cluster yet.
func eksSSOMethod(s map[string]string) *eksMethod {
	return &eksMethod{region: s[awsFieldSSORegion], http: awsHTTPClient()}
}

// runEKSDiscoverSignIn signs in at the access portal and keeps the session, with the role
// list when the user has several to pick from.
func runEKSDiscoverSignIn(ctx context.Context, secrets map[string]string, prompt func(signInPrompt)) error {
	m := eksSSOMethod(secrets)

	state, err := m.fromSecrets(pick(secrets, awsFieldSSOStartURL, awsFieldSSORegion))
	if err != nil {
		return err
	}

	ForgetDiscoverSignIn()

	signed, err := m.signIn(ctx, state, prompt, nil)
	if err != nil {
		return err
	}

	signed.Method = authEKS

	discoverySignIn.mu.Lock()
	defer discoverySignIn.mu.Unlock()

	discoverySignIn.state = &signed

	return nil
}

// DiscoverSignInChoices is, as JSON, the accounts and roles the discovery's Identity Center
// sign-in may assume, when the user has several to pick from: [{accountId, accountName,
// roleName}]. "" when there is no choice to make (one role, taken as is, or no such sign-in).
func DiscoverSignInChoices() (out string, err error) {
	defer maskResult(&out, &err)

	discoverySignIn.mu.Lock()
	defer discoverySignIn.mu.Unlock()

	if discoverySignIn.state == nil || discoverySignIn.state.Method != authEKS {
		return "", nil
	}

	roles := ssoChoices(*discoverySignIn.state)
	if len(roles) == 0 {
		return "", nil
	}

	return toJSON(roles)
}

// eksDiscoverySession is the discovery's Identity Center sign-in for the start URL and
// region of s, nil when there is none.
func eksDiscoverySession(s map[string]string) *kubeAuthState {
	discoverySignIn.mu.Lock()
	defer discoverySignIn.mu.Unlock()

	signed := discoverySignIn.state
	if signed == nil || signed.Method != authEKS || signed.secret(awsFieldSSOStartURL) != s[awsFieldSSOStartURL] ||
		signed.secret(awsFieldSSORegion) != s[awsFieldSSORegion] {
		return nil
	}

	out := *signed
	out.Session = maps.Clone(signed.Session)

	return &out
}

// chosenRoles is the roles a discovery reads clusters with: those of awsRoleSelection, else
// the account and role typed, else the one the sign-in took.
func chosenRoles(s map[string]string, signed kubeAuthState) ([]awsSSORole, error) {
	var out []awsSSORole

	for _, item := range strings.Fields(strings.ReplaceAll(s[awsFieldRoleSelection], ",", " ")) {
		account, role, ok := strings.Cut(item, "/")
		if !ok || account == "" || role == "" {
			return nil, fmt.Errorf("%q is not <account ID>/<role name>", item)
		}

		out = append(out, roleNamed(signed, account, role))
	}

	switch {
	case len(out) > 0:
	case s[awsFieldAccountID] != "" && s[awsFieldRoleName] != "":
		out = append(out, roleNamed(signed, s[awsFieldAccountID], s[awsFieldRoleName]))
	case signed.secret(awsFieldAccountID) != "" && signed.secret(awsFieldRoleName) != "":
		out = append(out, awsSSORole{AccountID: signed.secret(awsFieldAccountID), RoleName: signed.secret(awsFieldRoleName)})
	default:
		return nil, signInRequired(authEKS, "choose an account and role")
	}

	return out, nil
}

// roleNamed is the account and role, with the account's name from the sign-in's list.
func roleNamed(signed kubeAuthState, account, role string) awsSSORole {
	for _, r := range ssoChoices(signed) {
		if r.AccountID == account && r.RoleName == role {
			return r
		}
	}

	return awsSSORole{AccountID: account, RoleName: role}
}

// eksRegionScan is one role's clusters in one region.
type eksRegionScan struct {
	// rank orders the scans: roles in the order chosen, then regions.
	rank   int
	role   awsSSORole
	region string
	creds  awsCredentials
}

// discoverEKSSSOClusters reads the clusters of the chosen roles in the regions asked for,
// with the session of the discovery's sign-in. A region that refuses is skipped (and
// reported), unless every one does.
func discoverEKSSSOClusters(ctx context.Context, s map[string]string) ([]discoveredCluster, error) {
	m := eksSSOMethod(s)

	if _, err := m.fromSecrets(s); err != nil {
		return nil, err
	}

	regions, err := eksDiscoveryRegions(s[awsFieldRegions], s[awsFieldSSORegion])
	if err != nil {
		return nil, err
	}

	signed := eksDiscoverySession(s)
	if signed == nil {
		return nil, signInRequired(authEKS, "sign in to IAM Identity Center before finding clusters")
	}

	token, refreshed, err := m.ssoAccessToken(ctx, *signed)
	if err != nil {
		return nil, err
	}

	keepEKSDiscoverySession(refreshed)

	roles, err := chosenRoles(s, refreshed)
	if err != nil {
		return nil, err
	}

	var (
		scans   []eksRegionScan
		roleErr error
	)

	for _, role := range roles {
		creds, _, err := m.ssoRoleCredentials(ctx, token, withChosenRole(refreshed, role))
		if err != nil {
			// A role the portal no longer hands out is skipped; the others still count.
			roleErr = fmt.Errorf("%s: %w", ssoRoleLabel(role), err)

			continue
		}

		for _, region := range regions {
			scans = append(scans, eksRegionScan{rank: len(scans), role: role, region: region, creds: creds})
		}
	}

	if len(scans) == 0 {
		return nil, roleErr
	}

	found, refused, err := scanEKSRegions(ctx, scans)
	if err != nil {
		return nil, err
	}

	if len(found) == 0 && len(refused) > 0 {
		return nil, fmt.Errorf("no EKS cluster found; no access in %s", strings.Join(refused, ", "))
	}

	return namedEKSClusters(found), nil
}

// keepEKSDiscoverySession stores the renewed session of the discovery's sign-in, so the next
// renewal (another discovery, an import) uses the newest refresh token.
func keepEKSDiscoverySession(renewed kubeAuthState) {
	discoverySignIn.mu.Lock()
	defer discoverySignIn.mu.Unlock()

	signed := discoverySignIn.state
	if signed == nil || signed.Method != authEKS || signed.secret(awsFieldSSOStartURL) != renewed.secret(awsFieldSSOStartURL) ||
		signed.secret(awsFieldSSORegion) != renewed.secret(awsFieldSSORegion) {
		return
	}

	kept := *signed
	kept.Session = maps.Clone(renewed.Session)
	discoverySignIn.state = &kept
}

// eksFoundCluster is a discovered cluster and the role it was read with.
type eksFoundCluster struct {
	rank                         int
	role                         awsSSORole
	name, region, server, caData string
}

// scanEKSRegions lists the clusters of each scan, eksParallelRegions at a time; the regions
// that refused every role come back sorted.
func scanEKSRegions(ctx context.Context, scans []eksRegionScan) ([]eksFoundCluster, []string, error) {
	var (
		mu       sync.Mutex
		wg       sync.WaitGroup
		found    []eksFoundCluster
		refused  []string
		firstErr error
		// left counts, per region, the roles not yet refused there.
		left = map[string]int{}
	)

	for _, scan := range scans {
		left[scan.region]++
	}

	discoveryProgress.projects.Store(int64(len(scans)))

	slots := make(chan struct{}, eksParallelRegions)

	for _, scan := range scans {
		wg.Add(1)

		go func() {
			defer wg.Done()

			slots <- struct{}{}
			defer func() { <-slots }()

			clusters, err := listEKSClusters(ctx, scan)

			mu.Lock()
			defer mu.Unlock()

			discoveryProgress.scanned.Add(1)
			discoveryProgress.clusters.Add(int64(len(clusters)))

			switch {
			case unauthorized(err):
				if left[scan.region]--; left[scan.region] == 0 {
					refused = append(refused, scan.region)
					discoveryProgress.addRefused(scan.region)
				}
			case err != nil:
				if firstErr == nil {
					firstErr = err
				}
			default:
				found = append(found, clusters...)
			}
		}()
	}

	wg.Wait()

	slices.Sort(refused)
	slices.SortStableFunc(found, func(a, b eksFoundCluster) int { return a.rank - b.rank })

	return found, refused, firstErr
}

// listEKSClusters is the clusters of one role in one region, every page, each described.
func listEKSClusters(ctx context.Context, scan eksRegionScan) ([]eksFoundCluster, error) {
	get := func(path string, query url.Values, out any) error {
		u := eksEndpoint(scan.region) + path
		if len(query) > 0 {
			u += "?" + query.Encode()
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return err
		}

		sigV4Sign(req, emptySHA256, scan.creds, scan.region, "eks", time.Now())

		return cloudJSON(req, out)
	}

	var names []string

	next := ""

	for range eksMaxPages {
		q := url.Values{"maxResults": {"100"}}
		if next != "" {
			q.Set("nextToken", next)
		}

		var page struct {
			Clusters  []string `json:"clusters"`
			NextToken string   `json:"nextToken"`
		}

		if err := get("/clusters", q, &page); err != nil {
			return nil, err
		}

		names = append(names, page.Clusters...)

		if next = page.NextToken; next == "" {
			break
		}
	}

	out := make([]eksFoundCluster, 0, len(names))

	for _, name := range names {
		var d struct {
			Cluster struct {
				Endpoint             string `json:"endpoint"`
				CertificateAuthority struct {
					Data string `json:"data"`
				} `json:"certificateAuthority"`
			} `json:"cluster"`
		}

		err := get("/clusters/"+url.PathEscape(name), nil, &d)
		if unauthorized(err) {
			// Listed but not describable with this role: left out, the others kept.
			continue
		}

		if err != nil {
			return nil, fmt.Errorf("EKS cluster %s in %s: %w", name, scan.region, err)
		}

		out = append(out, eksFoundCluster{
			rank: scan.rank, role: scan.role, name: name, region: scan.region, server: d.Cluster.Endpoint, caData: d.Cluster.CertificateAuthority.Data,
		})
	}

	return out, nil
}

// namedEKSClusters is the clusters found, once each (a cluster two roles see keeps the first
// role chosen), named <name>.<region>.eks, with the account ID before .eks when the roles
// span several accounts. The role of each is remembered for KubeSetCredentials.
func namedEKSClusters(found []eksFoundCluster) []discoveredCluster {
	accounts := map[string]bool{}
	for _, c := range found {
		accounts[c.role.AccountID] = true
	}

	discoverySignIn.mu.Lock()
	defer discoverySignIn.mu.Unlock()

	discoverySignIn.eksRoles = map[string]awsSSORole{}

	var out []discoveredCluster

	for _, c := range found {
		if c.server == "" {
			continue
		}

		if _, seen := discoverySignIn.eksRoles[c.server]; seen {
			continue
		}

		discoverySignIn.eksRoles[c.server] = c.role

		name := c.name + "." + c.region
		if len(accounts) > 1 {
			name += "." + c.role.AccountID
		}

		out = append(out, discoveredCluster{
			name: name + ".eks", server: c.server, caData: c.caData,
			user: execUser("aws", "--region", c.region, "eks", "get-token", "--cluster-name", c.name),
		})
	}

	slices.SortFunc(out, func(a, b discoveredCluster) int { return strings.Compare(a.name, b.name) })

	return out
}

// withEKSDiscoverySession is state (an EKS cluster at server, entered with the start URL and
// region the discovery signed in with) with that sign-in's session and the role its cluster
// was found with, so importing needs no second sign-in. Any other state comes back as is.
func withEKSDiscoverySession(state kubeAuthState, server string) kubeAuthState {
	if !eksNeedsBrowser(state.Secrets) {
		return state
	}

	signed := eksDiscoverySession(state.Secrets)
	if signed == nil {
		return state
	}

	out := state
	out.Session, out.SessionExpires = signed.Session, signed.SessionExpires
	// The discovery's role credentials are for another role, maybe: the cluster gets its own.
	out = out.withSession(map[string]string{"roleKey": "", "roleSecret": "", "roleToken": "", "roleExpiry": ""})

	if state.secret(awsFieldAccountID) != "" && state.secret(awsFieldRoleName) != "" {
		return out.withSession(map[string]string{ssoRolesSession: ""})
	}

	discoverySignIn.mu.Lock()
	role, found := discoverySignIn.eksRoles[server]
	discoverySignIn.mu.Unlock()

	switch {
	case found:
	case signed.secret(awsFieldAccountID) != "" && signed.secret(awsFieldRoleName) != "":
		role = awsSSORole{AccountID: signed.secret(awsFieldAccountID), RoleName: signed.secret(awsFieldRoleName)}
	default:
		// No role known for this cluster: the sign-in sheet offers the sign-in's list.
		return out
	}

	return withChosenRole(out, role).withSession(map[string]string{ssoRolesSession: ""})
}
