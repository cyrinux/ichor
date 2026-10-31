package ichorgo

import (
	"context"

	"github.com/cyrinux/ichor/go/kube/kubediff"
)

// What a GitOps tool would change in the cluster, object by object, is go/kube/kubediff. These
// are the names the other files call.

const (
	diffChangeCreated   = kubediff.ChangeCreated
	diffChangeChanged   = kubediff.ChangeChanged
	diffChangeDeleted   = kubediff.ChangeDeleted
	diffChangeUnchanged = kubediff.ChangeUnchanged
	diffChangeEncrypted = kubediff.ChangeEncrypted
	diffChangeIgnored   = kubediff.ChangeIgnored
	diffChangeError     = kubediff.ChangeError

	diffSecretMask = kubediff.SecretMask
	diffMaxBytes   = kubediff.MaxBytes
)

type (
	gitOpsDiff        = kubediff.Result
	kubeDiffResource  = kubediff.Resource
	kubeDiffMasker    = kubediff.Masker
	kubeResourceIndex = kubediff.Index
	diffLiveChange    = kubediff.LiveChange
)

func newKubeDiffMasker() *kubeDiffMasker { return kubediff.NewMasker() }

func newKubeResourceIndex(k *kubeClient) *kubeResourceIndex { return kubediff.NewIndex(k.api()) }

func diffNormalize(obj map[string]any, masker *kubeDiffMasker) string {
	return kubediff.Normalize(obj, masker)
}

func readLive(ctx context.Context, k *kubeClient, path string) (map[string]any, error) {
	return kubediff.ReadLive(ctx, k.api(), path)
}

func diffObjects(r *kubeDiffResource, live, wanted map[string]any, masker *kubeDiffMasker) {
	kubediff.Objects(r, live, wanted, masker)
}

func diffRenderRaw(obj map[string]any) string { return kubediff.RenderRaw(obj) }

func unifiedDiff(a, b, fromName, toName string) string {
	return kubediff.Unified(a, b, fromName, toName)
}

func diffSplitLines(s string) []string { return kubediff.SplitLines(s) }

func splitAPIVersion(apiVersion string) (group, version string) {
	return kubediff.SplitAPIVersion(apiVersion)
}

func sortDiffResources(rs []kubeDiffResource) { kubediff.Sort(rs) }
