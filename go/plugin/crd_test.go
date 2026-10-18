package plugin

import (
	"slices"
	"strings"
	"testing"
)

// gadgetCRD is a CRD with what Lint reads: printer columns, condition types in the schema, a
// scale subresource, and an older served version.
const gadgetCRD = `apiVersion: v1
kind: Namespace
metadata: { name: demo-lab }
---
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata: { name: gadgets.demo.example.com }
spec:
  group: demo.example.com
  names: { kind: Gadget, plural: gadgets, singular: gadget, shortNames: [gd], categories: [all] }
  scope: Namespaced
  versions:
    - name: v1beta1
      served: true
      storage: false
    - name: v1
      served: true
      storage: true
      additionalPrinterColumns:
        - { name: Size, type: integer, jsonPath: .spec.size }
        - { name: Ready, type: string, jsonPath: '.status.conditions[?(@.type=="Ready")].status' }
        - { name: Age, type: date, jsonPath: .metadata.creationTimestamp }
      subresources:
        status: {}
        scale: { specReplicasPath: .spec.replicas, statusReplicasPath: .status.replicas }
      schema:
        openAPIV3Schema:
          type: object
          properties:
            spec:
              type: object
              properties:
                size: { type: integer }
                replicas: { type: integer }
            status:
              type: object
              properties:
                conditions:
                  type: array
                  items:
                    type: object
                    properties:
                      type: { type: string, enum: [Ready, Degraded] }
                      status: { type: string }
`

func TestLoadCRD(t *testing.T) {
	crd, err := LoadCRD([]byte(gadgetCRD))
	if err != nil {
		t.Fatal(err)
	}

	if crd.Metadata.Name != "gadgets.demo.example.com" || crd.Spec.Group != "demo.example.com" || crd.Spec.Names.Kind != "Gadget" ||
		crd.Spec.Scope != "Namespaced" || !slices.Equal(crd.Spec.Names.ShortNames, []string{"gd"}) {
		t.Errorf("crd %+v", crd)
	}

	v := crd.StorageVersion()
	if v.Name != "v1" || len(v.AdditionalPrinterColumns) != 3 || v.Subresources == nil || v.Subresources.Scale == nil ||
		v.Subresources.Scale.SpecReplicasPath != ".spec.replicas" {
		t.Errorf("storage version %+v", v)
	}

	if got := v.ConditionTypes(); !slices.Equal(got, []string{"Ready", "Degraded"}) {
		t.Errorf("condition types %v", got)
	}

	if v.Property("spec", "size")["type"] != "integer" || v.Property("spec", "nope") != nil {
		t.Error("Property")
	}

	if crd.Version("v1beta1") == nil || crd.Version("v2") != nil || crd.Version("v1beta1").ConditionTypes() != nil {
		t.Error("Version")
	}
}

func TestLoadCRDReadsJSON(t *testing.T) {
	crd, err := LoadCRD([]byte(`{"apiVersion":"apiextensions.k8s.io/v1","kind":"CustomResourceDefinition",
		"spec":{"group":"a.example.com","names":{"kind":"A","plural":"as"},"scope":"Cluster","versions":[{"name":"v1","served":true}]}}`))
	if err != nil {
		t.Fatal(err)
	}

	// No storage version: the last served one.
	if crd.StorageVersion().Name != "v1" || crd.Spec.Scope != "Cluster" {
		t.Errorf("crd %+v", crd)
	}
}

func TestLoadCRDRefuses(t *testing.T) {
	cases := map[string]string{
		"apiVersion: v1\nkind: Namespace\n": "no CustomResourceDefinition",
		"kind: CustomResourceDefinition\nspec: { names: { kind: A, plural: as }, versions: [{ name: v1 }] }\n": "no spec.group",
		"kind: CustomResourceDefinition\nspec: { group: a.example.com, versions: [{ name: v1 }] }\n":           "no spec.names",
		"kind: CustomResourceDefinition\nspec: { group: a.example.com, names: { kind: A, plural: as } }\n":     "no spec.versions",
		"kind: [\n": "does not parse",
		"":          "no CustomResourceDefinition",
	}

	for src, want := range cases {
		if _, err := LoadCRD([]byte(src)); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: %v, want %q", src, err, want)
		}
	}
}
