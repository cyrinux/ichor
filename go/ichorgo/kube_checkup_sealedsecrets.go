package ichorgo

import (
	"context"
	"errors"
	"slices"
	"time"
)

// The findings of the secrets section that Sealed Secrets adds. Namespace and Name are the
// SealedSecret (or the sealing key) they are about.
const (
	findSealedSecretNotSynced = "sealedSecretNotSynced" // Reason, Message: its Synced condition's
	findSealingKeyMissing     = "sealingKeyMissing"     // Extra: the controller namespace: nothing can be unsealed
	findSealingKeyCompromised = "sealingKeyCompromised" // a key the controller keeps out of use
)

// sealedSecretsControllerName is the subject of a key finding when the reader found no
// controller deployment to name.
const sealedSecretsControllerName = "sealed-secrets"

// sealedSecretGrace is how long a SealedSecret may take to unseal before the checkup reports
// it: the controller reconciles seconds after a create, so a fresh one is not a problem yet.
const sealedSecretGrace = time.Minute

// sealedKeyCompromised is the state the controller labels a key it keeps out of use.
const sealedKeyCompromised = "compromised"

// checkupSealedSecrets reads the SealedSecrets that did not unseal and the sealing keys the
// controller cannot work with, through the reader of the Data services screen: the same
// Synced condition, the same keys. Not served without the operator.
func checkupSealedSecrets(ctx context.Context, k *kubeClient, in checkupInput) checkupPart {
	if _, ok := in.groups[groupSealedSecrets]; !ok {
		return checkupPart{}
	}

	status := readSealedSecrets(ctx, k, in.now)
	if status == nil {
		return checkupPart{}
	}

	findings := sealedSecretItemFindings(status.Items, in.now)
	findings = append(findings, sealingKeyFindings(status)...)

	part := checkupPart{served: true, checked: len(status.Items) + len(status.Keys), findings: findings}
	if status.Error != "" {
		part.errs = []error{errors.New(status.Error)}
	}

	return part
}

// sealedSecretItemFindings are the SealedSecrets whose Synced condition is False: the Secret
// their workloads need is missing. A hard failure is critical, anything else (an unmanaged
// one, say) a warning; a very new SealedSecret is skipped, the controller has not had its
// turn yet.
func sealedSecretItemFindings(items []sealedSecret, now time.Time) []checkupFinding {
	findings := []checkupFinding{}

	for _, s := range items {
		if s.Synced || (s.Age > 0 && now.UnixMilli()-s.Age < sealedSecretGrace.Milliseconds()) {
			continue
		}

		severity := sevWarning
		if sealedSecretFailedUnseal(s.Reason) {
			severity = sevCritical
		}

		findings = append(findings, checkupFinding{
			Kind: findSealedSecretNotSynced, Severity: severity, Namespace: s.Namespace, Name: s.Name,
			Reason: s.Reason, Message: s.Message,
		})
	}

	return findings
}

// sealedSecretFailedUnseal says whether a Synced condition's reason means the Secret is
// missing, rather than a SealedSecret the controller deliberately leaves alone.
func sealedSecretFailedUnseal(reason string) bool {
	return reason == "Error" || reason == "UnsealFailed"
}

// sealingKeyFindings are the sealing keys the controller cannot use: a compromised one, and
// no active key at all (nothing can be unsealed). It leans on the reader's reasons, which
// are only recorded from a listing that succeeded, so a refused keys read raises nothing.
func sealingKeyFindings(status *sealedSecretsStatus) []checkupFinding {
	findings := []checkupFinding{}

	if slices.Contains(status.Reasons, sealedReasonNoKeys) {
		namespace, name := "", sealedSecretsControllerName
		if status.Controller != nil {
			namespace, name = status.Controller.Namespace, status.Controller.Name
		}

		findings = append(findings, checkupFinding{
			Kind: findSealingKeyMissing, Severity: sevCritical, Namespace: namespace, Name: name,
			Extra: namespace, Reason: sealedReasonNoKeys,
		})
	}

	for _, key := range status.Keys {
		if key.State == sealedKeyCompromised {
			findings = append(findings, checkupFinding{
				Kind: findSealingKeyCompromised, Severity: sevWarning, Namespace: key.Namespace, Name: key.Name,
				Reason: sealedReasonKeyCompromised,
			})
		}
	}

	return findings
}
