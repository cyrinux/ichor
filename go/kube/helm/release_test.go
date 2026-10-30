package helm

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"strconv"
	"testing"
)

// helmSecretData is a release as Helm stores it in a Secret's data (JSON-escaped for the fake).
func helmSecretData(t *testing.T, release map[string]any) string {
	t.Helper()

	raw, err := json.Marshal(release)
	if err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer

	zw := gzip.NewWriter(&buf)
	_, _ = zw.Write(raw)
	_ = zw.Close()

	helm := base64.StdEncoding.EncodeToString(buf.Bytes())

	return base64.StdEncoding.EncodeToString([]byte(helm))
}

func helmRow(secret, name string, version int, status string) string {
	return `{"cells":[],"object":{"metadata":{"name":"` + secret + `","namespace":"web","labels":{"owner":"helm","name":"` + name +
		`","version":"` + strconv.Itoa(version) + `","status":"` + status + `","modifiedAt":"1767225600"}}}}`
}

func TestDecodeHelmReleaseRejectsGarbage(t *testing.T) {
	for _, data := range []string{"!!", base64.StdEncoding.EncodeToString([]byte("!!")), base64.StdEncoding.EncodeToString([]byte(base64.StdEncoding.EncodeToString([]byte("{"))))} {
		if _, err := decodeRelease(data); err == nil {
			t.Errorf("%q accepted", data)
		}
	}
}
