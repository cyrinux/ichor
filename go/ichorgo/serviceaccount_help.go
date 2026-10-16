package ichorgo

// serviceAccountTemplate is a least-privilege ServiceAccount for the phone, as the help
// sheet shows it: the YAML to apply, then the commands that turn its token into a
// kubeconfig. Both apps list them from here, so they show the same text.
type serviceAccountTemplate struct {
	ID         string `json:"id"`
	YAML       string `json:"yaml"`
	Apply      string `json:"apply"`
	Kubeconfig string `json:"kubeconfig"`
}

// Template ids, in display order; the apps translate their titles and descriptions.
const (
	saTemplateRead      = "read"
	saTemplateOperate   = "operate"
	saTemplateNamespace = "namespace"
)

// The ServiceAccount and its long-lived token: a kubernetes.io/service-account-token
// Secret keeps a token until it is deleted, where a TokenRequest one (kubectl create
// token) expires within hours.
const saAccountYAML = `apiVersion: v1
kind: ServiceAccount
metadata:
  name: ichor-phone
  namespace: ichor
---
apiVersion: v1
kind: Secret
metadata:
  name: ichor-phone-token
  namespace: ichor
  annotations:
    kubernetes.io/service-account.name: ichor-phone
type: kubernetes.io/service-account-token
`

// The built-in view role reads namespaced objects only: nodes are cluster-wide.
const saNodesRules = `  - apiGroups: [""]
    resources: ["nodes"]
    verbs: ["get", "list", "watch"]
  - apiGroups: ["metrics.k8s.io"]
    resources: ["nodes"]
    verbs: ["get", "list"]
`

const saViewBindingYAML = `---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: ichor-phone-view
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: view
subjects:
  - kind: ServiceAccount
    name: ichor-phone
    namespace: ichor
`

const saReadYAML = saAccountYAML + saViewBindingYAML + `---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: ichor-phone-nodes
rules:
` + saNodesRules + `---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: ichor-phone-nodes
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: ichor-phone-nodes
subjects:
  - kind: ServiceAccount
    name: ichor-phone
    namespace: ichor
`

// A restart patches the pod template's annotation; a scale writes the scale subresource.
const saOperateYAML = saAccountYAML + saViewBindingYAML + `---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: ichor-phone-operate
rules:
` + saNodesRules + `  - apiGroups: ["apps"]
    resources: ["deployments", "statefulsets", "daemonsets"]
    verbs: ["patch"]
  - apiGroups: ["apps"]
    resources: ["deployments/scale", "statefulsets/scale"]
    verbs: ["get", "update", "patch"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: ichor-phone-operate
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: ichor-phone-operate
subjects:
  - kind: ServiceAccount
    name: ichor-phone
    namespace: ichor
`

// The built-in admin role, bound in one namespace only: nothing outside it.
const saNamespaceYAML = saAccountYAML + `---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: ichor-phone-admin
  namespace: ichor
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: admin
subjects:
  - kind: ServiceAccount
    name: ichor-phone
    namespace: ichor
`

const saApply = `kubectl create namespace ichor  # unless it exists
kubectl apply -f ichor-phone.yaml`

// From the current context's server and the Secret's token and CA, a self-contained
// kubeconfig-phone (POSIX shell; base64 -d works on Linux and macOS).
const saKubeconfig = `NS=ichor SA=ichor-phone CLUSTER=my-cluster
SERVER=$(kubectl config view --minify -o jsonpath='{.clusters[0].cluster.server}')
TOKEN=$(kubectl -n "$NS" get secret "$SA-token" -o jsonpath='{.data.token}' | base64 -d)
kubectl -n "$NS" get secret "$SA-token" -o jsonpath='{.data.ca\.crt}' | base64 -d > ca.crt
kubectl --kubeconfig=kubeconfig-phone config set-cluster "$CLUSTER" --server="$SERVER" --certificate-authority=ca.crt --embed-certs=true
kubectl --kubeconfig=kubeconfig-phone config set-credentials "$SA" --token="$TOKEN"
kubectl --kubeconfig=kubeconfig-phone config set-context "$CLUSTER" --cluster="$CLUSTER" --user="$SA" --namespace="$NS"
kubectl --kubeconfig=kubeconfig-phone config use-context "$CLUSTER"
rm ca.crt`

var serviceAccountTemplates = []serviceAccountTemplate{
	{saTemplateRead, saReadYAML, saApply, saKubeconfig},
	{saTemplateOperate, saOperateYAML, saApply, saKubeconfig},
	{saTemplateNamespace, saNamespaceYAML, saApply, saKubeconfig},
}

// ServiceAccountTemplates lists the help sheet's ServiceAccount templates, as JSON
// [{"id","yaml","apply","kubeconfig"}] in display order. Only the error is masked: the
// text is fixed and names no cluster.
func ServiceAccountTemplates() (out string, err error) {
	defer maskErr(&err)

	return toJSON(serviceAccountTemplates)
}
