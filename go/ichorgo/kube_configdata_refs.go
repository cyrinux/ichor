package ichorgo

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"slices"
	"strings"
)

// Ways a pod uses a Secret or ConfigMap (kubeConfigUse.Via).
const (
	configViaEnv             = "env"
	configViaEnvFrom         = "envFrom"
	configViaVolume          = "volume"
	configViaProjected       = "projected"
	configViaImagePullSecret = "imagePullSecret"
)

type configNameRef struct {
	Name string `json:"name"`
}

type configRefContainer struct {
	Env []struct {
		ValueFrom *struct {
			SecretKeyRef    *configNameRef `json:"secretKeyRef"`
			ConfigMapKeyRef *configNameRef `json:"configMapKeyRef"`
		} `json:"valueFrom"`
	} `json:"env"`
	EnvFrom []struct {
		SecretRef    *configNameRef `json:"secretRef"`
		ConfigMapRef *configNameRef `json:"configMapRef"`
	} `json:"envFrom"`
}

// configRefPod is what a pod says of the Secrets and ConfigMaps it uses.
type configRefPod struct {
	Metadata struct {
		Name string `json:"name"`
	} `json:"metadata"`
	Spec struct {
		Containers          []configRefContainer `json:"containers"`
		InitContainers      []configRefContainer `json:"initContainers"`
		EphemeralContainers []configRefContainer `json:"ephemeralContainers"`
		Volumes             []struct {
			Secret *struct {
				SecretName string `json:"secretName"`
			} `json:"secret"`
			ConfigMap *configNameRef `json:"configMap"`
			Projected *struct {
				Sources []struct {
					Secret    *configNameRef `json:"secret"`
					ConfigMap *configNameRef `json:"configMap"`
				} `json:"sources"`
			} `json:"projected"`
		} `json:"volumes"`
		ImagePullSecrets []configNameRef `json:"imagePullSecrets"`
	} `json:"spec"`
}

// listConfigRefPods reads the pods of namespace, page by page.
func listConfigRefPods(ctx context.Context, k *kubeClient, namespace string) ([]configRefPod, error) {
	var list kubeList[configRefPod]
	if err := getList(ctx, k, scopedPath("/api/v1", namespace, "pods"), &list); err != nil {
		return nil, err
	}

	return list.Items, nil
}

// configUsers lists the pods using the Secret or ConfigMap (kind) name, sorted by name.
func configUsers(pods []configRefPod, kind, name string) []kubeConfigUse {
	out := []kubeConfigUse{}

	for _, p := range pods {
		if via := podConfigRefs(p, kind, name); len(via) > 0 {
			out = append(out, kubeConfigUse{Pod: p.Metadata.Name, Via: via})
		}
	}

	slices.SortFunc(out, func(a, b kubeConfigUse) int { return strings.Compare(a.Pod, b.Pod) })

	return out
}

// podConfigRefs tells how pod uses the object, each way once, in a fixed order.
func podConfigRefs(pod configRefPod, kind, name string) []string {
	secret := kind == configKindSecret
	is := func(ref *configNameRef) bool { return ref != nil && ref.Name == name }
	found := map[string]bool{}

	for _, c := range slices.Concat(pod.Spec.Containers, pod.Spec.InitContainers, pod.Spec.EphemeralContainers) {
		for _, e := range c.Env {
			if e.ValueFrom != nil && is(refFor(secret, e.ValueFrom.SecretKeyRef, e.ValueFrom.ConfigMapKeyRef)) {
				found[configViaEnv] = true
			}
		}

		for _, e := range c.EnvFrom {
			if is(refFor(secret, e.SecretRef, e.ConfigMapRef)) {
				found[configViaEnvFrom] = true
			}
		}
	}

	for _, v := range pod.Spec.Volumes {
		if secret && v.Secret != nil && v.Secret.SecretName == name || !secret && is(v.ConfigMap) {
			found[configViaVolume] = true
		}

		if v.Projected == nil {
			continue
		}

		for _, s := range v.Projected.Sources {
			if is(refFor(secret, s.Secret, s.ConfigMap)) {
				found[configViaProjected] = true
			}
		}
	}

	if secret && slices.ContainsFunc(pod.Spec.ImagePullSecrets, func(r configNameRef) bool { return r.Name == name }) {
		found[configViaImagePullSecret] = true
	}

	via := []string{}

	for _, w := range []string{configViaEnv, configViaEnvFrom, configViaVolume, configViaProjected, configViaImagePullSecret} {
		if found[w] {
			via = append(via, w)
		}
	}

	return via
}

// refFor is the Secret reference or the ConfigMap one.
func refFor(secret bool, secretRef, configMapRef *configNameRef) *configNameRef {
	if secret {
		return secretRef
	}

	return configMapRef
}

// parseCertInfo reads the certificates of a PEM value: the first one, and how many there
// are. nil when it holds none (a private key).
func parseCertInfo(raw []byte) *kubeCertInfo {
	var (
		info  *kubeCertInfo
		count int
	)

	for rest := raw; ; {
		var block *pem.Block

		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}

		if block.Type != "CERTIFICATE" {
			continue
		}

		count++

		if info != nil {
			continue
		}

		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			continue
		}

		info = &kubeCertInfo{
			Subject: cert.Subject.String(), Issuer: cert.Issuer.String(),
			NotBefore: cert.NotBefore.Unix(), NotAfter: cert.NotAfter.Unix(), DNSNames: nonNil(cert.DNSNames),
		}
	}

	if info == nil {
		return nil
	}

	info.Count = count
	learnCertNames(info)

	return info
}

// learnCertNames teaches screenshot mode the names a certificate is for: they name the
// user's domains, which the mask may not know yet.
func learnCertNames(info *kubeCertInfo) {
	if !privacy.isEnabled() {
		return
	}

	for _, n := range info.DNSNames {
		if n = strings.TrimPrefix(n, "*."); strings.Contains(n, ".") {
			privacy.learnDomains(n)
		}
	}
}
