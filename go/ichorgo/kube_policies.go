package ichorgo

// Kyverno's API groups: its policies, and the standard policy reports it writes. Each read
// asks the preferred version its discovery offers; the report tells which one it got. The
// policies themselves are read from the preferred version when it serves them, else from
// kyvernoPoliciesVersion: Kyverno 1.15 made v2 the group's preferred version while
// ClusterPolicy and Policy stay under v1 (v2 serves updaterequests, admissionreports,
// cleanuppolicies), and a future release may serve them under v2.
const (
	groupKyverno      = "kyverno.io"
	groupPolicyReport = "wgpolicyk8s.io"

	// kyvernoPoliciesVersion is the storage version of ClusterPolicy and Policy, served by
	// every Kyverno release; the fallback when the preferred version does not serve them.
	kyvernoPoliciesVersion = "v1"
)

// What one report result says, and the action a policy applies its rules with. "Mixed" is
// ours: Kyverno's rule-level failureAction (1.13+) may disagree with the policy-level one.
const (
	policyResultPass  = "pass"
	policyResultFail  = "fail"
	policyResultWarn  = "warn"
	policyResultError = "error"
	policyResultSkip  = "skip"

	policyActionEnforce = "Enforce"
	policyActionAudit   = "Audit"
	policyActionMixed   = "Mixed"
	// policyActionWarn is Gatekeeper's warn: the request is admitted with a warning.
	policyActionWarn = "Warn"

	// The engines a report reads, as each row and engine names them.
	policyEngineKyverno    = "kyverno"
	policyEngineGatekeeper = "gatekeeper"
)

const (
	// policyReportsMax is how many report objects one read takes: Kyverno 1.11+ writes one
	// report per resource, so a big cluster has tens of thousands. Beyond it the read stops
	// taking content and reportsTruncated says so.
	policyReportsMax = 20000
	// policyFailingMax is how many failing resources one policy's row keeps (the worst first).
	policyFailingMax = 20
	// policyDescriptionMax is how long a policy's description annotation gets (clipUTF8
	// cuts at bytes and appends the ellipsis).
	policyDescriptionMax = 200
)

// policiesReport is the admission policies of the engines the cluster runs (Kyverno policies,
// Gatekeeper constraints) and their findings. Policies is empty (not nil, Installed false)
// when it runs neither.
type policiesReport struct {
	Installed bool `json:"installed"` // Kyverno or Gatekeeper
	// Kyverno and Gatekeeper say which engines run; Engines has one entry for each.
	Kyverno    bool   `json:"kyverno"`
	Gatekeeper bool   `json:"gatekeeper"`
	Version    string `json:"version"` // the served kyverno.io version, "" without Kyverno
	// Error is a Kyverno policy or engine read that failed; the rest of the report still shows.
	Error string `json:"error,omitempty"`
	// GatekeeperError is a Gatekeeper template, constraint or engine read that failed.
	GatekeeperError string `json:"gatekeeperError,omitempty"`
	// ReportsError is the policy reports being unreadable (often a 403 for a restricted
	// kubeconfig): the policies still show, their counts just stay at 0.
	ReportsError     string         `json:"reportsError,omitempty"`
	Engines          []policyEngine `json:"engines"`
	Policies         []policyRow    `json:"policies"`
	Namespaces       []policyNSRow  `json:"namespaces"`
	ReportsRead      int            `json:"reportsRead"`
	ReportsTruncated bool           `json:"reportsTruncated"`
}

// engine is the named engine of the report, false when it does not run.
func (r policiesReport) engine(name string) (policyEngine, bool) {
	for _, e := range r.Engines {
		if e.Name == name {
			return e, true
		}
	}

	return policyEngine{}, false
}

// policyEngine is one policy engine: its controllers, and what happens to the workloads'
// requests when it is down (the webhook's failurePolicy).
type policyEngine struct {
	Name        string             `json:"name"` // kyverno or gatekeeper
	Namespace   string             `json:"namespace"`
	Controllers []policyController `json:"controllers"`
	Webhook     policyWebhook      `json:"webhook"`
}

type policyController struct {
	Name    string `json:"name"` // app.kubernetes.io/component, else the Deployment's name
	Ready   int    `json:"ready"`
	Desired int    `json:"desired"`
}

type policyWebhook struct {
	Name          string `json:"name,omitempty"` // the ValidatingWebhookConfiguration's
	Present       bool   `json:"present"`
	FailurePolicy string `json:"failurePolicy,omitempty"` // Fail or Ignore
	// Service is the webhook's backend as "namespace/name", for the checkup's endpoint check.
	Service string `json:"service,omitempty"`
}

// policyRow is one policy row: what it enforces, whether it is ready, and what it found.
type policyRow struct {
	Namespace    string          `json:"namespace"` // "" for a ClusterPolicy
	Name         string          `json:"name"`
	Action       string          `json:"action"` // Enforce, Audit or Mixed
	Background   bool            `json:"background"`
	Ready        bool            `json:"ready"`
	ReadyReason  string          `json:"readyReason,omitempty"`
	ReadyMessage string          `json:"readyMessage,omitempty"`
	Category     string          `json:"category,omitempty"`
	Severity     string          `json:"severity,omitempty"`
	Description  string          `json:"description,omitempty"`
	Rules        policyRules     `json:"rules"`
	RuleNames    []string        `json:"ruleNames"`
	Pass         int             `json:"pass"`
	Fail         int             `json:"fail"`
	Warn         int             `json:"warn"`
	ErrorCount   int             `json:"error"` // a result value collides with Go's error type
	Skip         int             `json:"skip"`
	Failing      []policyFailure `json:"failing"`
	// Engine is kyverno or gatekeeper. Kind is a Gatekeeper constraint's kind (its template's).
	Engine string `json:"engine"`
	Kind   string `json:"kind,omitempty"`
	// FailingCapped: Failing is the first violations Gatekeeper kept (its
	// --constraint-violations-limit), not all of Fail.
	FailingCapped bool `json:"failingCapped,omitempty"`
	// AuditAt is when Gatekeeper's audit last wrote the violations, unix seconds, 0 for Kyverno.
	AuditAt int64 `json:"auditAt,omitempty"`
	// ByNamespace is the same counts per namespace, sorted by name, for the screen's namespace
	// filter: a ClusterPolicy's results spread over every namespace it checks.
	ByNamespace []policyNSRow `json:"byNamespace"`
}

// policyRules counts a policy's rules by kind, as their spec blocks are set or not.
type policyRules struct {
	Validate     int `json:"validate"`
	Mutate       int `json:"mutate"`
	Generate     int `json:"generate"`
	VerifyImages int `json:"verifyImages"`
}

// policyFailure is the first resource one failing report result names.
type policyFailure struct {
	Kind      string `json:"kind,omitempty"`
	Namespace string `json:"namespace,omitempty"`
	Name      string `json:"name,omitempty"`
	Rule      string `json:"rule,omitempty"`
	Message   string `json:"message,omitempty"`
	Severity  string `json:"severity,omitempty"`
	At        int64  `json:"at"` // unix seconds, 0 when the result had no timestamp
}

// policyNSRow sums one namespace's report results, for the screen's namespace filter.
type policyNSRow struct {
	Namespace  string `json:"namespace"`
	Pass       int    `json:"pass"`
	Fail       int    `json:"fail"`
	Warn       int    `json:"warn"`
	ErrorCount int    `json:"error"`
}

// KubePolicies reads the cluster's admission policies: Kyverno policies with their PolicyReport
// findings, and Gatekeeper constraints with their audit violations (os:admin):
// {"installed","kyverno","gatekeeper","version","error","gatekeeperError","reportsError",
// "engines","policies","namespaces","reportsRead","reportsTruncated"} (see policiesReport). A
// cluster with neither answers {"installed":false} and no error. kubeServer: see KubePods.
func KubePolicies(configYAML, contextName, kubeServer string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)

	return kubeReadJSON(kubeTarget{configYAML, contextName, kubeServer}, demoPolicies, readPolicies)
}
