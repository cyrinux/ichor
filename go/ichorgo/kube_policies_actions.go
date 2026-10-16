package ichorgo

import (
	"cmp"
	"context"
	"fmt"
	"net/url"
	"strings"
)

// Switching a Kyverno policy between Audit and Enforce, the incident move when a policy
// refuses a hotfix: relax it, roll out, put it back. Kyverno 1.13+ reads the rule-level
// validate.failureAction before the deprecated spec.validationFailureAction, so both are set:
// patching the spec field alone would change nothing on a policy whose rules carry their own.
// The patch is a JSON patch tested against the resourceVersion read just before, so an edit
// made in between makes it fail instead of being overwritten.

// policyActionResult is what KubePolicySetAction answers: the policy as read after the patch.
type policyActionResult struct {
	Namespace    string `json:"namespace"`
	Name         string `json:"name"`
	Action       string `json:"action"`
	Ready        bool   `json:"ready"`
	RulesPatched int    `json:"rulesPatched"`
}

// kyvernoPolicyPatchRef is what the switch reads before patching: the resource version and,
// per rule, its validate block and image checks (the parts that carry a failureAction).
type kyvernoPolicyPatchRef struct {
	Metadata struct {
		ResourceVersion string `json:"resourceVersion"`
	} `json:"metadata"`
	Spec struct {
		Rules []struct {
			Validate     map[string]any   `json:"validate"`
			VerifyImages verifyImagesRule `json:"verifyImages"`
		} `json:"rules"`
	} `json:"spec"`
}

// KubePolicySetAction switches the Kyverno policy namespace/name ("" namespace: a
// ClusterPolicy) to action, "Audit" or "Enforce" (os:admin; get and patch on the policy):
// {"namespace","name","action","ready","rulesPatched"}. Refused on demo contexts and when the
// credentials may not patch the policy. kubeServer: see KubePods.
func KubePolicySetAction(configYAML, contextName, kubeServer, namespace, name, action string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)
	namespace, name = privacy.revealNamespace(strings.TrimSpace(namespace)), privacy.reveal(strings.TrimSpace(name))
	action = strings.TrimSpace(action)

	object := "ClusterPolicy/" + name
	if namespace != "" {
		object = "Policy/" + name
	}

	defer recordAction(&err, configYAML, contextName, auditAction{
		Server: kubeServer, Action: "policy-set-action", Namespace: namespace, Object: object, Params: action,
	})

	if action != policyActionAudit && action != policyActionEnforce {
		return "", fmt.Errorf("unknown policy action %q: Audit or Enforce", action)
	}

	// A ClusterPolicy has no namespace: the name alone is checked.
	if err := validateKubeName("policy", cmp.Or(namespace, "cluster"), name); err != nil {
		return "", err
	}

	var result policyActionResult

	err = kubeMutate(kubeTarget{configYAML, contextName, kubeServer}, func(ctx context.Context, k *kubeClient) error {
		r, err := setPolicyAction(ctx, k, namespace, name, action)
		result = r

		return err
	})
	if err != nil {
		return "", err
	}

	return toJSON(result)
}

func setPolicyAction(ctx context.Context, k *kubeClient, namespace, name, action string) (policyActionResult, error) {
	groups, err := readAPIGroups(ctx, k)
	if err != nil {
		return policyActionResult{}, err
	}

	if groups[groupKyverno] == "" {
		return policyActionResult{}, &kubeAPIError{Code: 404, Reason: "NotFound", Message: "Kyverno is not installed"}
	}

	// The policies live under kyvernoPoliciesVersion on every release (see readKyverno).
	resource, path := "clusterpolicies", "/apis/"+groupKyverno+"/"+kyvernoPoliciesVersion+"/clusterpolicies/"+url.PathEscape(name)
	if namespace != "" {
		resource = "policies"
		path = "/apis/" + groupKyverno + "/" + kyvernoPoliciesVersion + "/namespaces/" + url.PathEscape(namespace) + "/policies/" + url.PathEscape(name)
	}

	access, err := canDo(ctx, k, ssarAttributes{Namespace: namespace, Verb: "patch", Group: groupKyverno, Resource: resource, Name: name})
	if err == nil && !access.Allowed {
		return policyActionResult{}, &kubeAPIError{Code: 403, Reason: "Forbidden",
			Message: strings.TrimSpace(fmt.Sprintf("these credentials may not patch %s.%s %s %s", resource, groupKyverno, name, access.Reason))}
	}

	var ref kyvernoPolicyPatchRef
	if err := k.get(ctx, path, &ref); err != nil {
		return policyActionResult{}, err
	}

	ops, rules := policyActionPatch(ref, action)
	if err := k.patch(ctx, path, "application/json-patch+json", ops, nil); err != nil {
		return policyActionResult{}, err
	}

	var after kyvernoPolicyObject
	if err := k.get(ctx, path, &after); err != nil {
		return policyActionResult{}, err
	}

	row := kyvernoPolicyOf(after)

	return policyActionResult{Namespace: namespace, Name: name, Action: row.Action, Ready: row.Ready, RulesPatched: rules}, nil
}

// policyActionPatch is the JSON patch that sets the spec-level action, every validate rule's
// failureAction and every image check's, guarded by the resource version; and how many rules
// it touches. "add" replaces a member that is there and creates one that is not.
func policyActionPatch(ref kyvernoPolicyPatchRef, action string) ([]map[string]any, int) {
	ops := []map[string]any{
		{"op": "test", "path": "/metadata/resourceVersion", "value": ref.Metadata.ResourceVersion},
		{"op": "add", "path": "/spec/validationFailureAction", "value": action},
	}

	rules := 0

	for i, r := range ref.Spec.Rules {
		touched := false

		if r.Validate != nil {
			ops = append(ops, map[string]any{"op": "add", "path": fmt.Sprintf("/spec/rules/%d/validate/failureAction", i), "value": action})
			touched = true
		}

		for j := range r.VerifyImages.checks {
			ops = append(ops, map[string]any{"op": "add", "path": fmt.Sprintf("/spec/rules/%d/verifyImages/%d/failureAction", i, j), "value": action})
			touched = true
		}

		if touched {
			rules++
		}
	}

	return ops, rules
}
