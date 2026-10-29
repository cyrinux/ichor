// Package kube is the Kubernetes transport of the Go core: JSON over HTTPS to an API server,
// its answers and errors, a cache of opened clients, and the poll loop of a followed
// operation. It replaces client-go, which would add megabytes to the apps for a few calls.
//
// What belongs here: building a request below a client's base address, the bearer token
// header, reading and capping an answer, turning a non-2xx answer or a failed dial into an
// error a person can read, keeping one client per target.
//
// What does not: parsing a kubeconfig or a talosconfig, signing in, the demo inventory, the
// privacy mask and the audit log. Those stay in the gomobile facade (go/ichorgo), which wraps
// every entry point here in a thin unexported function.
package kube
