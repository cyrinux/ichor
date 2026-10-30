package helm

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

	"github.com/cyrinux/ichor/go/kube"
)

// Helm release Secrets: read, decode, encode and write a revision as Helm's storage does.

// readStored reads a revision's Secret into a generic payload and the typed fields.
func readStored(ctx context.Context, k kube.Client, ref SecretRef) (stored, error) {
	var secret struct {
		Data map[string]string `json:"data"`
	}

	if err := k.Get(ctx, kube.ScopedPath("/api/v1", ref.Namespace, "secrets")+"/"+url.PathEscape(ref.Secret), &secret); err != nil {
		return stored{}, err
	}

	payload, rel, err := decodePayload(secret.Data["release"])

	return stored{ref: ref, payload: payload, rel: rel}, err
}

func decodePayload(data string) (map[string]any, Release, error) {
	raw, err := releaseJSON(data)
	if err != nil {
		return nil, Release{}, err
	}

	var rel Release
	if err := json.Unmarshal(raw, &rel); err != nil {
		return nil, Release{}, fmt.Errorf("decode Helm release: %w", err)
	}

	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber() // values keep their exact numbers when written back

	var payload map[string]any
	if err := dec.Decode(&payload); err != nil {
		return nil, Release{}, fmt.Errorf("decode Helm release: %w", err)
	}

	return payload, rel, nil
}

// encodePayload is Helm's encoding: JSON, gzip, base64; the Secret adds its own base64.
func encodePayload(payload map[string]any) ([]byte, error) {
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

// createRevision records revision next: the target's chart, values, manifest and hooks,
// pending-rollback, first deployed when the current one was. A Secret already there means
// another operation got in first: the rollback stops.
func createRevision(ctx context.Context, k Env, rb *rollback, next int, description string, now time.Time) error {
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
	info["status"] = StatusPendingRollback
	info["description"] = description
	payload["info"] = info
	payload["version"] = next

	data, err := encodePayload(payload)
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
		"name": rb.plan.Name, "owner": "helm", "status": StatusPendingRollback,
		"version": strconv.Itoa(next), "createdAt": strconv.FormatInt(now.Unix(), 10),
	} {
		labels[key] = value
	}

	secret := map[string]any{
		"apiVersion": "v1", "kind": "Secret", "type": "helm.sh/release.v1",
		"metadata": map[string]any{
			"name": secretName(rb.plan.Name, next), "namespace": rb.plan.Namespace, "labels": labels,
		},
		"data": map[string][]byte{"release": data},
	}

	err = k.Post(ctx, kube.ScopedPath("/api/v1", rb.plan.Namespace, "secrets"), secret, nil)
	if kube.Code(err) == http.StatusConflict {
		return fmt.Errorf("revision %d already exists: another operation ran meanwhile", next)
	}

	return err
}

// setStatus rewrites a revision's status (and description when given) in its payload and
// labels, as Helm's storage does.
func setStatus(ctx context.Context, k Env, namespace, secretName, status, description string, now time.Time) error {
	path := kube.ScopedPath("/api/v1", namespace, "secrets") + "/" + url.PathEscape(secretName)

	var secret map[string]any
	if err := k.Get(ctx, path, &secret); err != nil {
		return fmt.Errorf("%s: %w", secretName, err)
	}

	data, _ := secret["data"].(map[string]any)
	encoded, _ := data["release"].(string)

	payload, _, err := decodePayload(encoded)
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

	release, err := encodePayload(payload)
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

	if err := k.Do(ctx, http.MethodPut, path, "application/json", body, nil); err != nil {
		return fmt.Errorf("%s: %w", secretName, err)
	}

	return nil
}
