package ichorgo

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"sync"
	"time"
)

const (
	groupSealedSecrets         = "bitnami.com"
	sealedSecretsVersion       = "v1alpha1"
	sealedSecretsKeyLabel      = "sealedsecrets.bitnami.com/sealed-secrets-key"
	sealedSecretsMaxItems      = 500
	sealedReasonNotSynced      = "notSynced"
	sealedReasonUnmanaged      = "unmanaged"
	sealedReasonStale          = "stale"
	sealedReasonControllerDown = "controllerDown"
	sealedReasonNoKeys         = "noKeys"
	sealedReasonKeyCompromised = "keyCompromised"
	sealedReasonKeyStale       = "keyStale"
)

type sealedSecretsStatus struct {
	Version     string                   `json:"version"`
	Error       string                   `json:"error"`
	Health      string                   `json:"health"`
	Reasons     []string                 `json:"reasons"`
	Controller  *sealedSecretsController `json:"controller"`
	Items       []sealedSecret           `json:"items"`
	Truncated   bool                     `json:"truncated"`
	SecretCount int                      `json:"secretCount"`
	Keys        []sealingKey             `json:"keys"`
}

type sealedSecretsController struct {
	Namespace     string   `json:"namespace"`
	Name          string   `json:"name"`
	Ready         bool     `json:"ready"`
	Replicas      int      `json:"replicas"`
	ReadyReplicas int      `json:"readyReplicas"`
	Health        string   `json:"health"`
	Reasons       []string `json:"reasons"`
}

type sealedSecret struct {
	Namespace          string   `json:"namespace"`
	Name               string   `json:"name"`
	Keys               []string `json:"keys"`
	KeyCount           int      `json:"keyCount"`
	Type               string   `json:"type"`
	Immutable          bool     `json:"immutable"`
	Scope              string   `json:"scope"`
	Synced             bool     `json:"synced"`
	Reason             string   `json:"reason"`
	Message            string   `json:"message"`
	LastTransitionTime int64    `json:"lastTransitionTime"`
	ObservedGeneration int64    `json:"observedGeneration"`
	Stale              bool     `json:"stale"`
	Age                int64    `json:"age"` // creation time, unix ms (matching the issue's wire contract)
	Health             string   `json:"health"`
	Reasons            []string `json:"reasons"`
}

type sealingKey struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	State     string `json:"state"`
	CreatedAt int64  `json:"createdAt"`
	Age       int64  `json:"age"` // elapsed milliseconds, zero for an unknown timestamp
}

type sealedMetadata struct {
	Namespace         string            `json:"namespace"`
	Name              string            `json:"name"`
	Generation        int64             `json:"generation"`
	CreationTimestamp string            `json:"creationTimestamp"`
	Annotations       map[string]string `json:"annotations"`
	Labels            map[string]string `json:"labels"`
}

// sealedDataKeys discards encrypted values without decoding them into strings.
type sealedDataKeys []string

func (keys *sealedDataKeys) UnmarshalJSON(data []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	*keys = make([]string, 0, len(fields))
	for name := range fields {
		*keys = append(*keys, name)
	}
	slices.Sort(*keys)
	return nil
}

type sealedSecretObject struct {
	Metadata sealedMetadata `json:"metadata"`
	Spec     struct {
		EncryptedData sealedDataKeys `json:"encryptedData"`
		Template      struct {
			Type      string `json:"type"`
			Immutable bool   `json:"immutable"`
		} `json:"template"`
	} `json:"spec"`
	Status struct {
		Conditions         []kubeCondition `json:"conditions"`
		ObservedGeneration int64           `json:"observedGeneration"`
	} `json:"status"`
}

// Sealing-key objects deliberately have no data/stringData fields.
type sealingKeyObject struct {
	Metadata sealedMetadata `json:"metadata"`
}

type sealedDeploymentObject struct {
	Metadata sealedMetadata `json:"metadata"`
	Spec     struct {
		Replicas *int `json:"replicas"`
		Template struct {
			Spec struct {
				Containers []struct {
					Image string `json:"image"`
				} `json:"containers"`
			} `json:"spec"`
		} `json:"template"`
	} `json:"spec"`
	Status struct {
		ReadyReplicas int `json:"readyReplicas"`
	} `json:"status"`
}

// readSealedSecrets confirms the resource inside the shared Bitnami group, then reads
// independent listings. A denied listing never hides the data from another one.
func readSealedSecrets(ctx context.Context, k *kubeClient, now time.Time) *sealedSecretsStatus {
	out := &sealedSecretsStatus{Version: sealedSecretsVersion, Health: healthOK, Reasons: []string{}, Items: []sealedSecret{}, Keys: []sealingKey{}}
	var discovery struct {
		Resources []struct {
			Name string `json:"name"`
		} `json:"resources"`
	}
	if err := k.get(ctx, "/apis/"+groupSealedSecrets+"/"+sealedSecretsVersion, &discovery); err != nil {
		out.Error = sectionError(err)
		return out
	}
	found := false
	for _, r := range discovery.Resources {
		found = found || r.Name == "sealedsecrets"
	}
	if !found {
		return nil
	}
	var errs [3]error
	var wg sync.WaitGroup
	wg.Go(func() { errs[0] = readSealedSecretItems(ctx, k, out) })
	wg.Go(func() {
		errs[1] = k.listAll(ctx, "/api/v1/secrets", pageQuery{labelSelector: sealedSecretsKeyLabel, metadataOnly: true}, func() { out.Keys = []sealingKey{} }, func(page kubePage) error {
			keys, err := decodeItems[sealingKeyObject](page)
			if err != nil {
				return err
			}
			for _, key := range keys {
				state, ok := key.Metadata.Labels[sealedSecretsKeyLabel]
				if !ok {
					continue
				}
				created := unixMilli(key.Metadata.CreationTimestamp)
				age := int64(0)
				if created != 0 {
					age = max(int64(0), now.UnixMilli()-created)
				}
				out.Keys = append(out.Keys, sealingKey{Namespace: key.Metadata.Namespace, Name: key.Metadata.Name, State: state, CreatedAt: created, Age: age})
			}
			return nil
		})
	})
	wg.Go(func() { out.Controller, errs[2] = readSealedSecretsController(ctx, k) })
	wg.Wait()
	out.Error = sectionError(errs[:]...)
	for _, item := range out.Items {
		if healthRank(item.Health) < healthRank(out.Health) {
			out.Health = item.Health
		}
	}
	if errs[1] == nil {
		sealedSecretsKeyHealth(out, now)
	}
	if errs[2] == nil && (out.Controller == nil || !out.Controller.Ready) {
		out.Health = healthCritical
		out.Reasons = append(out.Reasons, sealedReasonControllerDown)
	}
	slices.SortFunc(out.Items, byHealthThenKey(func(x sealedSecret) (string, string) { return x.Health, x.Namespace + "/" + x.Name }))
	slices.SortFunc(out.Keys, func(a, b sealingKey) int {
		if a.CreatedAt > b.CreatedAt {
			return -1
		}
		if a.CreatedAt < b.CreatedAt {
			return 1
		}
		return strings.Compare(a.Namespace+"/"+a.Name, b.Namespace+"/"+b.Name)
	})
	return out
}

func readSealedSecretItems(ctx context.Context, k *kubeClient, out *sealedSecretsStatus) error {
	return k.listAll(ctx, "/apis/"+groupSealedSecrets+"/"+sealedSecretsVersion+"/sealedsecrets", pageQuery{limit: sealedSecretsMaxItems}, func() {
		out.Items = []sealedSecret{}
		out.SecretCount = 0
		out.Truncated = false
	}, func(page kubePage) error {
		items, err := decodeItems[sealedSecretObject](page)
		if err != nil {
			return err
		}
		for _, item := range items {
			out.SecretCount++
			if len(out.Items) < sealedSecretsMaxItems {
				out.Items = append(out.Items, mapSealedSecret(item))
			} else {
				out.Truncated = true
			}
		}
		return nil
	})
}

func mapSealedSecret(c sealedSecretObject) sealedSecret {
	cond := kubeConditions(c.Status.Conditions).get("Synced")
	out := sealedSecret{Namespace: c.Metadata.Namespace, Name: c.Metadata.Name, Keys: append([]string{}, c.Spec.EncryptedData...),
		KeyCount: len(c.Spec.EncryptedData), Type: c.Spec.Template.Type, Immutable: c.Spec.Template.Immutable, Scope: "strict",
		Synced: cond.Status == "True", Reason: cond.Reason, Message: cond.Message, LastTransitionTime: unixMilli(cond.LastTransitionTime),
		ObservedGeneration: c.Status.ObservedGeneration, Stale: c.Metadata.Generation > c.Status.ObservedGeneration, Age: unixMilli(c.Metadata.CreationTimestamp), Health: healthOK, Reasons: []string{}}
	switch {
	case c.Metadata.Annotations["sealedsecrets.bitnami.com/cluster-wide"] == "true":
		out.Scope = "cluster-wide"
	case c.Metadata.Annotations["sealedsecrets.bitnami.com/namespace-wide"] == "true":
		out.Scope = "namespace-wide"
	}
	if cond.Status == "False" {
		if cond.Reason == "Unmanaged" {
			out.Health = healthWarning
			out.Reasons = append(out.Reasons, sealedReasonUnmanaged)
		} else {
			out.Health = healthCritical
			out.Reasons = append(out.Reasons, sealedReasonNotSynced)
		}
	}
	if out.Stale {
		if out.Health == healthOK {
			out.Health = healthWarning
		}
		out.Reasons = append(out.Reasons, sealedReasonStale)
	}
	return out
}

func sealedSecretsKeyHealth(out *sealedSecretsStatus, now time.Time) {
	active := false
	newest := int64(0)
	compromised := false
	for _, key := range out.Keys {
		if key.State == "active" {
			active = true
			newest = max(newest, key.CreatedAt)
		}
		compromised = compromised || key.State == "compromised"
	}
	if !active {
		out.Health = healthCritical
		out.Reasons = append(out.Reasons, sealedReasonNoKeys)
	}
	if compromised {
		if out.Health == healthOK {
			out.Health = healthWarning
		}
		out.Reasons = append(out.Reasons, sealedReasonKeyCompromised)
	}
	if active && newest > 0 && now.Sub(time.UnixMilli(newest)) > 30*24*time.Hour {
		if out.Health == healthOK {
			out.Health = healthWarning
		}
		out.Reasons = append(out.Reasons, sealedReasonKeyStale)
	}
}

// Match the same catalog images as runningApps, even when the controller has no running
// pods. Read deployments directly so detection needs no additional pods-list permission.
func readSealedSecretsController(ctx context.Context, k *kubeClient) (*sealedSecretsController, error) {
	var controllers []sealedSecretsController
	catalog := loadAppCatalog()
	err := k.listAll(ctx, "/apis/apps/v1/deployments", pageQuery{}, func() { controllers = nil }, func(page kubePage) error {
		deployments, err := decodeItems[sealedDeploymentObject](page)
		if err != nil {
			return err
		}
		for _, d := range deployments {
			found := false
			for _, c := range d.Spec.Template.Spec.Containers {
				if id := catalog.identify(parseImageRef(c.Image)); id.app != nil && id.app.ID == "sealed-secrets" {
					found = true
				}
			}
			if !found {
				continue
			}
			replicas := 1
			if d.Spec.Replicas != nil {
				replicas = *d.Spec.Replicas
			}
			c := sealedSecretsController{Namespace: d.Metadata.Namespace, Name: d.Metadata.Name, Replicas: replicas, ReadyReplicas: d.Status.ReadyReplicas, Ready: replicas > 0 && d.Status.ReadyReplicas >= replicas, Health: healthOK, Reasons: []string{}}
			if !c.Ready {
				c.Health = healthCritical
				c.Reasons = append(c.Reasons, sealedReasonControllerDown)
			}
			controllers = append(controllers, c)
		}
		return nil
	})
	slices.SortFunc(controllers, byHealthThenKey(func(c sealedSecretsController) (string, string) { return c.Health, c.Namespace + "/" + c.Name }))
	if len(controllers) == 0 {
		return nil, err
	}
	return &controllers[0], err
}
