package ichorgo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"go.yaml.in/yaml/v4"
)

// Editing an object from the browser: the user changes the YAML KubeObjectYAML gave, sees
// what the API server would store (a dry run, as a diff), then saves. The save is an update
// at the resourceVersion the YAML was read at, so a change someone made in between is never
// overwritten: the API server answers Conflict and the user reloads.

// hiddenSecretMarker is how KubeObjectYAML replaces a Secret's values.
const hiddenSecretMarker = "<hidden, "

type kubeEditPreview struct {
	Changed bool   `json:"changed"`
	Diff    string `json:"diff"`
}

// KubeObjectUpdatePreview dry-runs the update of the object to edited (YAML) and returns a
// JSON kubeEditPreview: a unified diff of the stored object and the one it would become.
func KubeObjectUpdatePreview(configYAML, contextName, kubeServer, group, version, resource, namespace, name, edited string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)

	path, obj, err := editTarget(group, version, resource, privacy.reveal(strings.TrimSpace(namespace)), privacy.reveal(strings.TrimSpace(name)), edited)
	if err != nil {
		return "", err
	}

	if isDemoContext(configYAML, contextName) {
		return "", errDemoUnavailable
	}

	preview, err := withKube(kubeTarget{configYAML, contextName, kubeServer}, func(ctx context.Context, k *kubeClient) (kubeEditPreview, error) {
		live, err := readLive(ctx, k, path)
		if err != nil {
			return kubeEditPreview{}, err
		}

		if live == nil {
			return kubeEditPreview{}, errors.New("the object no longer exists")
		}

		wanted, err := putObject(ctx, k, path, obj, true)
		if err != nil {
			return kubeEditPreview{}, editError(err)
		}

		masker := newKubeDiffMasker()
		from, to := diffNormalize(live, masker), diffNormalize(wanted, masker)

		if from == to {
			return kubeEditPreview{}, nil
		}

		return kubeEditPreview{Changed: true, Diff: unifiedDiff(from, to, "stored", "edited")}, nil
	})
	if err != nil {
		return "", err
	}

	return toJSON(preview)
}

// KubeObjectUpdate saves edited (YAML) as the object, failing with a clear message when it
// changed since it was read.
func KubeObjectUpdate(configYAML, contextName, kubeServer, group, version, resource, namespace, name, edited string) (err error) {
	defer maskErr(&err)

	contextName = unmaskContext(configYAML, contextName)
	namespace, name = privacy.reveal(strings.TrimSpace(namespace)), privacy.reveal(strings.TrimSpace(name))

	defer recordAction(&err, configYAML, contextName, auditAction{Action: "edit", Namespace: namespace, Object: resource + "/" + name})

	path, obj, err := editTarget(group, version, resource, namespace, name, edited)
	if err != nil {
		return err
	}

	return kubeMutate(kubeTarget{configYAML, contextName, kubeServer}, func(ctx context.Context, k *kubeClient) error {
		_, err := putObject(ctx, k, path, obj, false)

		return editError(err)
	})
}

// editTarget checks edited against the object it must stay and returns its API path and
// content.
func editTarget(group, version, resource, namespace, name, edited string) (string, map[string]any, error) {
	ref, err := newResourceRef(group, version, resource)
	if err != nil {
		return "", nil, err
	}

	if err := validateNamespace(namespace); err != nil {
		return "", nil, err
	}

	if strings.Contains(edited, hiddenSecretMarker) {
		return "", nil, errors.New("show the Secret's values before editing it: hidden values would be saved as they read")
	}

	var obj map[string]any
	if err := yaml.Unmarshal([]byte(edited), &obj); err != nil {
		return "", nil, fmt.Errorf("invalid YAML: %w", err)
	}

	meta, _ := obj["metadata"].(map[string]any)

	switch {
	case obj == nil || meta == nil:
		return "", nil, errors.New("the YAML has no metadata")
	case meta["name"] != name:
		return "", nil, fmt.Errorf("metadata.name must stay %q", name)
	case namespace != "" && meta["namespace"] != nil && meta["namespace"] != namespace:
		return "", nil, fmt.Errorf("metadata.namespace must stay %q", namespace)
	case meta["resourceVersion"] == nil || meta["resourceVersion"] == "":
		return "", nil, errors.New("keep metadata.resourceVersion: it protects a change made in the meantime")
	}

	return ref.path(namespace) + "/" + url.PathEscape(name), obj, nil
}

func putObject(ctx context.Context, k *kubeClient, path string, obj map[string]any, dryRun bool) (map[string]any, error) {
	body, err := json.Marshal(obj)
	if err != nil {
		return nil, fmt.Errorf("encode object: %w", err)
	}

	q := url.Values{"fieldManager": {"ichor"}}
	if dryRun {
		q.Set("dryRun", "All")
	}

	var stored map[string]any
	if err := k.do(ctx, http.MethodPut, path+"?"+q.Encode(), "application/json", body, &stored); err != nil {
		return nil, err
	}

	return stored, nil
}

func editError(err error) error {
	if kubeCode(err) == http.StatusConflict {
		return errors.New("the object changed since it was opened: reload it and edit again")
	}

	return err
}
