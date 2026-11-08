package ichorgo

import "github.com/cyrinux/ichor/go/kube"

// One Kubernetes client per cluster the app looks at, opened on first use and kept for a
// while: the kubeconfig fetch and the API server probe are the slow part of a call. The
// cache itself is kube.ClientCache.

// kubeTarget is what a Kubernetes client is opened for: a talosconfig context and the API
// server address the user set for it, if any.
type kubeTarget struct {
	config, context, server string
}

func (t kubeTarget) key() string { return cacheKey(t.config, t.context+"\x00"+t.server) }

// kubeClients caches one client per kubeTarget for kubeClientTTL.
var kubeClients = newKubeClientCache(openKubeClientForContext)

type kubeClientCache struct {
	clients *kube.ClientCache[kubeTarget, *kubeClient]
	open    func(kubeTarget) (*kubeClient, error)
}

func newKubeClientCache(open func(kubeTarget) (*kubeClient, error)) *kubeClientCache {
	clients := kube.NewClientCache(open, kubeTarget.key, (*kubeClient).close, kubeClientTTL)

	return &kubeClientCache{clients: clients, open: open}
}

// get returns the cached client, or opens one; fresh tells the client was opened for this call.
func (c *kubeClientCache) get(target kubeTarget) (k *kubeClient, fresh bool, err error) {
	return c.clients.Get(target)
}

// forget drops k if it is still the cached client, so the next call fetches a new
// kubeconfig and probes again; a client another call already replaced it with stays.
func (c *kubeClientCache) forget(target kubeTarget, k *kubeClient) { c.clients.Forget(target, k) }

// forgetWhere drops every client whose target matches.
func (c *kubeClientCache) forgetWhere(match func(kubeTarget) bool) { c.clients.ForgetWhere(match) }

// forgetConfig drops every client of the context (any API server address): its credentials
// changed (signed in or out).
func (c *kubeClientCache) forgetConfig(config, context string) {
	c.forgetWhere(func(t kubeTarget) bool { return t.config == config && t.context == context })
}
