package ichorgo

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// Helm release Secrets: read, decode, encode and write a revision as Helm's storage does.

// readHelmStored reads a revision's Secret into a generic payload and the typed fields.
func readHelmStored(ctx context.Context, k *kubeClient, ref helmSecretRef) (helmStored, error) {
	var secret struct {
		Data map[string]string `json:"data"`
	}

	if err := k.get(ctx, scopedPath("/api/v1", ref.namespace, "secrets")+"/"+url.PathEscape(ref.secret), &secret); err != nil {
		return helmStored{}, err
	}

	payload, rel, err := decodeHelmPayload(secret.Data["release"])

	return helmStored{ref: ref, payload: payload, rel: rel}, err
}

func decodeHelmPayload(data string) (map[string]any, helmRelease, error) {
	raw, err := helmReleaseJSON(data)
	if err != nil {
		return nil, helmRelease{}, err
	}

	var rel helmRelease
	if err := json.Unmarshal(raw, &rel); err != nil {
		return nil, helmRelease{}, fmt.Errorf("decode Helm release: %w", err)
	}

	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber() // values keep their exact numbers when written back

	var payload map[string]any
	if err := dec.Decode(&payload); err != nil {
		return nil, helmRelease{}, fmt.Errorf("decode Helm release: %w", err)
	}

	return payload, rel, nil
}

// encodeHelmPayload is Helm's encoding: JSON, gzip, base64; the Secret adds its own base64.
func encodeHelmPayload(payload map[string]any) ([]byte, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer

	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(raw); err != nil {
		return nil, err
	}

	if err := zw.Close(); err != nil {
		return nil, err
	}

	return base64.StdEncoding.AppendEncode(nil, buf.Bytes()), nil
}

// createHelmRevision records revision next: the target's chart, values, manifest and hooks,
// pending-rollback, first deployed when the current one was. A Secret already there means
// another operation got in first: the rollback stops.
func createHelmRevision(ctx context.Context, k *kubeClient, rb *helmRollback, next int, description string, now time.Time) error {
	payload := map[string]any{}
	for key, v := range rb.target.payload {
		payload[key] = v
	}

	info := map[string]any{}
	if was, ok := rb.target.payload["info"].(map[string]any); ok {
		for key, v := range was {
			info[key] = v
		}
	}

	if was, ok := rb.current.payload["info"].(map[string]any); ok {
		info["first_deployed"] = was["first_deployed"]
	}

	info["last_deployed"] = now.Format(time.RFC3339Nano)
	info["deleted"] = ""
	info["status"] = helmStatusPendingRollback
	info["description"] = description
	payload["info"] = info
	payload["version"] = next

	data, err := encodeHelmPayload(payload)
	if err != nil {
		return err
	}

	// The release's own labels (helm --labels), then Helm's, which they cannot override.
	labels := map[string]string{}

	if custom, ok := payload["labels"].(map[string]any); ok {
		for key, v := range custom {
			if value, ok := v.(string); ok {
				labels[key] = value
			}
		}
	}

	for key, value := range map[string]string{
		"name": rb.plan.Name, "owner": "helm", "status": helmStatusPendingRollback,
		"version": strconv.Itoa(next), "createdAt": strconv.FormatInt(now.Unix(), 10),
	} {
		labels[key] = value
	}

	secret := map[string]any{
		"apiVersion": "v1", "kind": "Secret", "type": "helm.sh/release.v1",
		"metadata": map[string]any{
			"name": helmSecretName(rb.plan.Name, next), "namespace": rb.plan.Namespace, "labels": labels,
		},
		"data": map[string][]byte{"release": data},
	}

	err = k.post(ctx, scopedPath("/api/v1", rb.plan.Namespace, "secrets"), secret, nil)
	if kubeCode(err) == http.StatusConflict {
		return fmt.Errorf("revision %d already exists: another operation ran meanwhile", next)
	}

	return err
}

// setHelmStatus rewrites a revision's status (and description when given) in its payload and
// labels, as Helm's storage does.
func setHelmStatus(ctx context.Context, k *kubeClient, namespace, secretName, status, description string, now time.Time) error {
	path := scopedPath("/api/v1", namespace, "secrets") + "/" + url.PathEscape(secretName)

	var secret map[string]any
	if err := k.get(ctx, path, &secret); err != nil {
		return fmt.Errorf("%s: %w", secretName, err)
	}

	data, _ := secret["data"].(map[string]any)
	encoded, _ := data["release"].(string)

	payload, _, err := decodeHelmPayload(encoded)
	if err != nil {
		return fmt.Errorf("%s: %w", secretName, err)
	}

	info, _ := payload["info"].(map[string]any)
	if info == nil {
		info = map[string]any{}
		payload["info"] = info
	}

	info["status"] = status
	if description != "" {
		info["description"] = description
	}

	release, err := encodeHelmPayload(payload)
	if err != nil {
		return err
	}

	data["release"] = base64.StdEncoding.EncodeToString(release)

	meta, _ := secret["metadata"].(map[string]any)
	delete(meta, "managedFields")

	labels, _ := meta["labels"].(map[string]any)

	if labels == nil {
		labels = map[string]any{}
		meta["labels"] = labels
	}

	labels["status"] = status
	labels["modifiedAt"] = strconv.FormatInt(now.Unix(), 10)

	body, err := json.Marshal(secret)
	if err != nil {
		return err
	}

	if err := k.do(ctx, http.MethodPut, path, "application/json", body, nil); err != nil {
		return fmt.Errorf("%s: %w", secretName, err)
	}

	return nil
}
