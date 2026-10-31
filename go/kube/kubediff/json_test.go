package kubediff

import (
	"encoding/json"
	"testing"
)

// The apps decode these names: a renamed field must not rename its key.
func TestResourceJSONKeys(t *testing.T) {
	out, err := json.Marshal(Result{Kind: "Kustomization", Warnings: []string{}, Resources: []Resource{{
		Kind: "Deployment", Change: ChangeChanged, LiveChanges: []LiveChange{{Path: []string{"spec"}, Label: "spec", Wanted: "1", Live: "2", Type: "int"}},
	}}})
	if err != nil {
		t.Fatal(err)
	}

	want := `{"kind":"Kustomization","namespace":"","name":"","revision":"","applied":"","resources":[{"group":"","version":"","kind":"Deployment","namespace":"","name":"","change":"changed","diff":"","truncated":false,"error":"","liveChanges":[{"path":["spec"],"label":"spec","wanted":"1","live":"2","type":"int"}]}],"warnings":[]}`
	if string(out) != want {
		t.Fatalf("got %s", out)
	}
}
