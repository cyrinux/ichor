// Package flux is the Flux v2 logic of the Go core: reading Kustomizations, HelmReleases and
// their sources, the actions the flux CLI runs (annotations, spec.suspend) and following a
// reconcile. It decodes the custom resources itself and imports none of Flux's packages.
//
// What belongs here: everything that decides what to read from and write to the API server,
// given a kube.Client. Its tests answer from go/kube/kubetest and never import the facade.
// Names come back unmasked and are never logged.
//
// What does not: the demo data, the privacy mask, the audit log, the icon catalog, opening a
// client for a cluster, the listeners the apps implement. Those stay in the gomobile facade
// (go/ichorgo). What Flux needs of a domain still in the facade (a pod's status, the
// controller's image tag, the sign-in error) comes through Env. See go/kube/helm/doc.go for the
// rule these moves follow.
package flux
