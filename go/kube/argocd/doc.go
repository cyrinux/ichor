// Package argocd is the Argo CD logic of the Go core, read and driven through its custom
// resources (argoproj.io Applications, ApplicationSets, AppProjects) the way `argocd --core`
// works: reading the apps with their operation, resources, history and owners, the actions
// (one merge patch on the Application each, guarded by its resourceVersion), the AppProject
// sync windows and Ichor's freezes, and the forge links of a revision.
//
// What belongs here: everything that decides what to read from and write to the API server,
// given a kube.Client. Its tests answer from go/kube/kubetest and never import the facade.
// Names come back unmasked and are never logged.
//
// What does not: the demo data, the privacy mask, the audit log, the icon catalog, opening a
// client for a cluster, the network map (it reads the routes of every networking domain still
// in the facade). Those stay in the gomobile facade (go/ichorgo). What Argo CD needs of a
// domain still in the facade (the controller's image tag, the pods that are not ready, an app's
// icon, the sign-in error) comes through Env. See go/kube/helm/doc.go for the rule these moves
// follow.
package argocd
