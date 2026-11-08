// Package kubediff is what a GitOps tool would change in the cluster, object by object: the
// manifests it would apply go to the API server as a server-side apply dry run (defaults,
// webhooks and field ownership applied, nothing written), and the answer is compared with the
// live object, as a unified diff with Secret values masked. Shared by the Flux diff in
// go/kube/flux, and by the Argo CD diff and the apply preview still in the facade.
package kubediff
