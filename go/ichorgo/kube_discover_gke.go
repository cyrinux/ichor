package ichorgo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"sync"
)

// GKE discovery: a service account key lists the clusters of its own project; a Google
// account (gcloud user credentials) can see many projects, so they are listed first (Cloud
// Resource Manager), or taken from gcpProjects, and the clusters of each are read a few at a
// time. A project that refuses (GKE API off, no permission) is skipped. An organisation's
// account often sees hundreds of projects, most without GKE: they answer a quick 403.

const (
	// gcpFieldProjects narrows a Google account's discovery to these project IDs (commas or
	// spaces); empty is every project the account can see.
	gcpFieldProjects = "gcpProjects"
	// gcpMaxProjects caps the projects a Google account's discovery reads clusters from.
	gcpMaxProjects = 500
	// gcpParallelProjects is how many projects' clusters are read at once.
	gcpParallelProjects = 16
)

// gcpCRMEndpoint is Cloud Resource Manager, overridable in tests.
var gcpCRMEndpoint = "https://cloudresourcemanager.googleapis.com"

// gcpProjectPattern is a project ID: 6 to 30 lowercase letters, digits or hyphens, starting
// with a letter; legacy ones carry a domain prefix ("example.com:my-project").
var gcpProjectPattern = regexp.MustCompile(`^([a-z0-9][-a-z0-9.]*:)?[a-z][-a-z0-9]{4,28}[a-z0-9]$`)

// gkeAccount is what a GKE discovery calls the Google APIs with.
type gkeAccount struct {
	token string
	// quotaProject bills a user credential's calls (X-Goog-User-Project), "" for none.
	quotaProject string
}

func (a gkeAccount) get(ctx context.Context, u string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", "Bearer "+a.token)

	if a.quotaProject != "" {
		req.Header.Set("X-Goog-User-Project", a.quotaProject)
	}

	return cloudJSON(req, out)
}

func discoverGKEClusters(ctx context.Context, s map[string]string) ([]discoveredCluster, error) {
	if s[gcpFieldUserCredentials] == "" {
		var sa struct {
			ProjectID string `json:"project_id"`
		}

		if !isGCPUserCredentials(s[gcpFieldServiceAccount]) {
			if err := json.Unmarshal([]byte(s[gcpFieldServiceAccount]), &sa); err != nil || sa.ProjectID == "" {
				return nil, errors.New("the service account key names no project")
			}
		}

		account, err := gkeSignIn(ctx, s)
		if err != nil {
			return nil, err
		}

		return listGKEClusters(ctx, account, sa.ProjectID)
	}

	creds, err := parseUserCredentials(s[gcpFieldUserCredentials])
	if err != nil {
		return nil, err
	}

	account, err := gkeSignIn(ctx, s)
	if err != nil {
		return nil, err
	}

	account.quotaProject = creds.QuotaProjectID

	projects, err := gcpProjectsFilter(s[gcpFieldProjects])
	if err != nil {
		return nil, err
	}

	truncated := false

	if len(projects) == 0 {
		if projects, truncated, err = listGCPProjects(ctx, account); err != nil {
			return nil, err
		}
	}

	clusters, err := listGKEClustersOf(ctx, account, projects)
	if err == nil && len(clusters) == 0 && truncated {
		return nil, fmt.Errorf("no GKE cluster in the first %d projects this account sees: enter the project IDs", gcpMaxProjects)
	}

	return clusters, err
}

// gkeSignIn mints a token from the GKE credentials s holds (either kind).
func gkeSignIn(ctx context.Context, s map[string]string) (gkeAccount, error) {
	state, err := gkeMethod{}.fromSecrets(s)
	if err != nil {
		return gkeAccount{}, err
	}

	token, _, _, err := gkeMethod{}.mint(ctx, state)
	if err != nil {
		return gkeAccount{}, err
	}

	return gkeAccount{token: token}, nil
}

// gcpProjectsFilter is the project IDs raw lists (commas or spaces), deduplicated.
func gcpProjectsFilter(raw string) ([]string, error) {
	var out []string

	for _, p := range strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == ' ' || r == '\n' || r == '\t' }) {
		if !gcpProjectPattern.MatchString(p) {
			return nil, fmt.Errorf("%q is not a project ID", p)
		}

		if !slices.Contains(out, p) {
			out = append(out, p)
		}
	}

	return out, nil
}

// listGCPProjects is the active projects the account can see, at most gcpMaxProjects, and
// whether the account sees more.
func listGCPProjects(ctx context.Context, account gkeAccount) ([]string, bool, error) {
	var out []string

	token := ""

	for {
		q := url.Values{"filter": {"lifecycleState:ACTIVE"}, "pageSize": {"100"}}
		if token != "" {
			q.Set("pageToken", token)
		}

		var page struct {
			Projects []struct {
				ProjectID string `json:"projectId"`
			} `json:"projects"`
			NextPageToken string `json:"nextPageToken"`
		}

		if err := account.get(ctx, gcpCRMEndpoint+"/v1/projects?"+q.Encode(), &page); err != nil {
			return nil, false, fmt.Errorf("list Google Cloud projects (the quota project needs the Cloud Resource Manager API): %w", err)
		}

		for i, p := range page.Projects {
			out = append(out, p.ProjectID)
			if len(out) == gcpMaxProjects {
				return out, i < len(page.Projects)-1 || page.NextPageToken != "", nil
			}
		}

		if token = page.NextPageToken; token == "" {
			break
		}
	}

	if len(out) == 0 {
		return nil, false, errors.New("this account sees no Google Cloud project: enter the project IDs")
	}

	return out, false, nil
}

// listGKEClustersOf reads the clusters of projects, gcpParallelProjects at a time; a project
// that refuses is skipped, unless every one does.
func listGKEClustersOf(ctx context.Context, account gkeAccount, projects []string) ([]discoveredCluster, error) {
	var (
		mu       sync.Mutex
		wg       sync.WaitGroup
		out      []gkeProjectCluster
		skipped  int
		firstErr error
	)

	discoveryProgress.projects.Store(int64(len(projects)))

	slots := make(chan struct{}, gcpParallelProjects)

	for _, project := range projects {
		wg.Add(1)

		go func() {
			defer wg.Done()

			slots <- struct{}{}
			defer func() { <-slots }()

			clusters, err := listGKEClusters(ctx, account, project)

			mu.Lock()
			defer mu.Unlock()

			discoveryProgress.scanned.Add(1)
			discoveryProgress.clusters.Add(int64(len(clusters)))

			switch {
			case gcpRefused(err):
				skipped++
			case err != nil:
				if firstErr == nil {
					firstErr = err
				}
			default:
				for _, c := range clusters {
					out = append(out, gkeProjectCluster{project: project, cluster: c})
				}
			}
		}()
	}

	wg.Wait()

	if firstErr != nil {
		return nil, firstErr
	}

	if skipped == len(projects) {
		return nil, fmt.Errorf("no project lets this account list GKE clusters (%d refused): enable the Kubernetes Engine API or enter the project IDs", skipped)
	}

	named := uniqueGKENames(out)
	slices.SortFunc(named, func(a, b discoveredCluster) int { return strings.Compare(a.name, b.name) })

	return named, nil
}

// gkeProjectCluster is a discovered cluster and the project it was read from.
type gkeProjectCluster struct {
	project string
	cluster discoveredCluster
}

// uniqueGKENames is the clusters, those whose name (cluster.location.gke) another project's
// cluster shares named cluster.location.project.gke instead.
func uniqueGKENames(clusters []gkeProjectCluster) []discoveredCluster {
	count := map[string]int{}
	for _, c := range clusters {
		count[c.cluster.name]++
	}

	out := make([]discoveredCluster, 0, len(clusters))

	for _, c := range clusters {
		d := c.cluster
		if count[d.name] > 1 {
			d.name = strings.TrimSuffix(d.name, ".gke") + "." + c.project + ".gke"
		}

		out = append(out, d)
	}

	return out
}

// gcpRefused tells a project that will not list its clusters: no permission, or the API off.
func gcpRefused(err error) bool {
	var e *cloudHTTPError

	return errors.As(err, &e) && (e.status == http.StatusForbidden || e.status == http.StatusNotFound)
}

// listGKEClusters is the clusters of one project, every location.
func listGKEClusters(ctx context.Context, account gkeAccount, project string) ([]discoveredCluster, error) {
	var list struct {
		Clusters []struct {
			Name       string `json:"name"`
			Location   string `json:"location"`
			Endpoint   string `json:"endpoint"`
			MasterAuth struct {
				ClusterCACertificate string `json:"clusterCaCertificate"`
			} `json:"masterAuth"`
		} `json:"clusters"`
	}

	if err := account.get(ctx, gkeEndpoint+"/v1/projects/"+url.PathEscape(project)+"/locations/-/clusters", &list); err != nil {
		return nil, fmt.Errorf("GKE clusters: %w", err)
	}

	out := make([]discoveredCluster, 0, len(list.Clusters))
	for _, c := range list.Clusters {
		out = append(out, discoveredCluster{
			name: c.Name + "." + c.Location + ".gke", server: "https://" + c.Endpoint, caData: c.MasterAuth.ClusterCACertificate,
			user: execUser("gke-gcloud-auth-plugin"),
		})
	}

	return out, nil
}
