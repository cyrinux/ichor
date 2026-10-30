// Package helm is the Helm release logic of the Go core, without the Helm SDK: reading a
// release and its history from its Secrets, planning and running a rollback (hooks, three-way
// merge patches, the revision bookkeeping `helm rollback` does) and waiting for its workloads.
//
// What belongs here: everything that decides what to read from and write to the API server,
// given a kube.Client. Its tests answer from go/kube/kubetest and never import the facade.
//
// What does not: the demo data, the privacy mask, the audit log, opening a client for a
// cluster, the listeners the apps implement. Those stay in the gomobile facade (go/ichorgo),
// whose exported functions come down to: demo check, funnel, a call here, mask and audit,
// JSON. What Helm needs of another domain still in the facade (Flux's suspend, a workload's
// rollout state, the sign-in error) comes through Env.
//
// The rule for the next domains out of the facade (go/kube/<domain>): one domain per pull
// request, the pure logic moves, the demo, mask and audit stay; the exported facade signatures
// and their JSON do not change, and the contract fixtures prove it. Export only what the
// facade calls.
package helm
