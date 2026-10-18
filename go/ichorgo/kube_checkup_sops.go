package ichorgo

import (
	"context"
	"strings"
)

// findSopsDecryptionFailed is the secrets finding of a Flux Kustomization that cannot decrypt
// its SOPS files: nothing in it is applied any more. Namespace, Name: the Kustomization;
// Extra: spec.decryption.secretRef.name ("" when decryption is not configured); Reason,
// Message: its Ready condition's, the message clipped to sopsMessageLimit.
const findSopsDecryptionFailed = "sopsDecryptionFailed"

// Flux has no dedicated reason for a decryption failure, so the checkup matches the Ready
// message. Upstream: fluxcd/kustomize-controller internal/controller/kustomization_controller.go.
const (
	// sopsBuildFailedMarker is in the BuildFailed messages "error decrypting sources: …" (L834)
	// and "decryption failed for '<Kind>/<name>': …" (L852), matched case-insensitively.
	sopsBuildFailedMarker = "decrypt"
	// sopsNotConfiguredMarker is in the ReconciliationFailed message "… is SOPS encrypted,
	// configuring decryption is required for this secret to be reconciled" (L999).
	sopsNotConfiguredMarker = "SOPS encrypted"
)

// The Ready reasons kustomize-controller sets on those failures.
const (
	fluxReasonBuildFailed          = "BuildFailed"
	fluxReasonReconciliationFailed = "ReconciliationFailed"
)

// sopsMessageLimit caps a finding's message: upstream allows 20000 bytes in a condition.
const sopsMessageLimit = 300

// sopsKustomizationObject is the part of a Kustomization the SOPS check reads: never the
// Secret its secretRef names.
type sopsKustomizationObject struct {
	Metadata checkMeta `json:"metadata"`
	Spec     struct {
		Suspend    bool `json:"suspend"`
		Decryption *struct {
			Provider  string `json:"provider"`
			SecretRef *struct {
				Name string `json:"name"`
			} `json:"secretRef"`
		} `json:"decryption"`
	} `json:"spec"`
	Status struct {
		Conditions []kubeCondition `json:"conditions"`
	} `json:"status"`
}

// checkupSopsDecryption finds the Flux Kustomizations whose build stops on SOPS: a key that
// does not decrypt the files, or encrypted Secrets without decryption configured. Not
// served without Flux's kustomize-controller.
func checkupSopsDecryption(ctx context.Context, k *kubeClient, in checkupInput) checkupPart {
	version, ok := in.groups[groupFluxKustomize]
	if !ok {
		return checkupPart{}
	}

	items, err := listObjects[sopsKustomizationObject](ctx, k, "/apis/"+groupFluxKustomize+"/"+version+"/kustomizations")

	findings := []checkupFinding{}
	checked := 0

	for _, ks := range items {
		if ks.Spec.Decryption != nil {
			checked++
		}

		if f, ok := sopsFinding(ks); ok {
			findings = append(findings, f)
		}
	}

	return checkupPart{served: true, checked: checked, findings: findings, errs: []error{ignoreNotFound(err)}}
}

// sopsFinding is the finding of one Kustomization, false when it is suspended or its Ready
// condition is not a SOPS failure. Critical: the whole Kustomization is stuck.
func sopsFinding(ks sopsKustomizationObject) (checkupFinding, bool) {
	c := readyCondition(ks.Status.Conditions)
	if ks.Spec.Suspend || c.Status != "False" || !sopsFailure(c) {
		return checkupFinding{}, false
	}

	secretRef := ""
	if d := ks.Spec.Decryption; d != nil && d.SecretRef != nil {
		secretRef = d.SecretRef.Name
	}

	return checkupFinding{
		Kind: findSopsDecryptionFailed, Severity: sevCritical, Namespace: ks.Metadata.Namespace, Name: ks.Metadata.Name,
		Extra: secretRef, Reason: c.Reason, Message: clipUTF8(c.Message, sopsMessageLimit),
	}, true
}

// sopsFailure says whether a False Ready condition is a SOPS failure rather than another
// build error (a bad overlay stays a GitOps finding).
func sopsFailure(c kubeCondition) bool {
	switch c.Reason {
	case fluxReasonBuildFailed:
		return strings.Contains(strings.ToLower(c.Message), sopsBuildFailedMarker)
	case fluxReasonReconciliationFailed:
		return strings.Contains(c.Message, sopsNotConfiguredMarker)
	default:
		return false
	}
}
