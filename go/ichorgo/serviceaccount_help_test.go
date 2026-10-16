package ichorgo

import (
	"encoding/json"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"
)

// The fields the templates use, decoded strictly: a typo in a key fails the test.
type saTestObject struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Type       string `json:"type,omitempty"`
	Metadata   struct {
		Name        string            `json:"name"`
		Namespace   string            `json:"namespace,omitempty"`
		Annotations map[string]string `json:"annotations,omitempty"`
	} `json:"metadata"`
	Rules []struct {
		APIGroups []string `json:"apiGroups"`
		Resources []string `json:"resources"`
		Verbs     []string `json:"verbs"`
	} `json:"rules,omitempty"`
	RoleRef *struct {
		APIGroup string `json:"apiGroup"`
		Kind     string `json:"kind"`
		Name     string `json:"name"`
	} `json:"roleRef,omitempty"`
	Subjects []struct {
		Kind      string `json:"kind"`
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
	} `json:"subjects,omitempty"`
}

func decodeSATemplate(t *testing.T, text string) []saTestObject {
	t.Helper()

	var objects []saTestObject

	for i, doc := range strings.Split(text, "\n---\n") {
		var obj saTestObject
		if err := yaml.UnmarshalStrict([]byte(doc), &obj); err != nil {
			t.Fatalf("document %d: %v\n%s", i, err, doc)
		}

		objects = append(objects, obj)
	}

	return objects
}

var saAPIVersions = map[string]string{
	"ServiceAccount": "v1", "Secret": "v1",
	"ClusterRole": "rbac.authorization.k8s.io/v1", "ClusterRoleBinding": "rbac.authorization.k8s.io/v1",
	"Role": "rbac.authorization.k8s.io/v1", "RoleBinding": "rbac.authorization.k8s.io/v1",
}

var saVerbs = map[string]bool{"get": true, "list": true, "watch": true, "patch": true, "update": true}

func TestServiceAccountTemplates(t *testing.T) {
	out, err := ServiceAccountTemplates()
	if err != nil {
		t.Fatal(err)
	}

	var listed []serviceAccountTemplate
	if err := json.Unmarshal([]byte(out), &listed); err != nil || len(listed) != 3 {
		t.Fatalf("ServiceAccountTemplates() = %.60q, %v", out, err)
	}

	for _, tmpl := range listed {
		t.Run(tmpl.ID, func(t *testing.T) { checkSATemplate(t, tmpl) })
	}
}

func checkSATemplate(t *testing.T, tmpl serviceAccountTemplate) {
	objects := decodeSATemplate(t, tmpl.YAML)
	if len(objects) < 3 || objects[0].Kind != "ServiceAccount" || objects[1].Kind != "Secret" {
		t.Fatalf("want the ServiceAccount, then its token Secret, then RBAC; got %d objects", len(objects))
	}

	sa := objects[0].Metadata
	secret := objects[1]

	if secret.Type != "kubernetes.io/service-account-token" ||
		secret.Metadata.Annotations["kubernetes.io/service-account.name"] != sa.Name ||
		secret.Metadata.Namespace != sa.Namespace {
		t.Errorf("token Secret not tied to the ServiceAccount: %+v", secret)
	}

	roles := map[string]bool{"view": true, "admin": true}
	bindings := 0

	for _, obj := range objects {
		if want := saAPIVersions[obj.Kind]; want == "" || obj.APIVersion != want {
			t.Errorf("%s %s: apiVersion %q", obj.Kind, obj.Metadata.Name, obj.APIVersion)
		}

		switch obj.Kind {
		case "ClusterRole":
			roles[obj.Metadata.Name] = true

			for _, rule := range obj.Rules {
				for _, verb := range rule.Verbs {
					if !saVerbs[verb] {
						t.Errorf("%s: verb %q is beyond least privilege", obj.Metadata.Name, verb)
					}
				}
			}
		case "ClusterRoleBinding", "RoleBinding":
			bindings++

			checkSABinding(t, obj, sa.Name, sa.Namespace)
		}
	}

	for _, obj := range objects {
		if obj.RoleRef != nil && !roles[obj.RoleRef.Name] {
			t.Errorf("%s binds the undefined role %q", obj.Metadata.Name, obj.RoleRef.Name)
		}
	}

	if bindings == 0 || strings.Contains(tmpl.YAML, "cluster-admin") {
		t.Errorf("%d bindings, or cluster-admin", bindings)
	}

	if !strings.Contains(tmpl.Kubeconfig, `SA=`+sa.Name) || !strings.Contains(tmpl.Kubeconfig, `"$SA-token"`) ||
		!strings.Contains(tmpl.Kubeconfig, "--embed-certs=true") || !strings.Contains(tmpl.Apply, "kubectl apply") {
		t.Errorf("commands do not match the template:\n%s\n%s", tmpl.Apply, tmpl.Kubeconfig)
	}
}

func checkSABinding(t *testing.T, obj saTestObject, name, namespace string) {
	t.Helper()

	ref := obj.RoleRef
	if ref == nil || ref.APIGroup != "rbac.authorization.k8s.io" || ref.Kind != "ClusterRole" {
		t.Errorf("%s: roleRef %+v", obj.Metadata.Name, ref)
	}

	if obj.Kind == "RoleBinding" && obj.Metadata.Namespace != namespace {
		t.Errorf("%s: RoleBinding outside %s", obj.Metadata.Name, namespace)
	}

	if len(obj.Subjects) != 1 || obj.Subjects[0].Kind != "ServiceAccount" ||
		obj.Subjects[0].Name != name || obj.Subjects[0].Namespace != namespace {
		t.Errorf("%s: subjects %+v", obj.Metadata.Name, obj.Subjects)
	}
}
