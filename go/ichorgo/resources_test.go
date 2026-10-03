package ichorgo

import (
	"strings"
	"testing"
	"time"

	"github.com/cosi-project/runtime/api/v1alpha1"
	"github.com/cosi-project/runtime/pkg/resource"
	"github.com/cosi-project/runtime/pkg/resource/meta"
	"github.com/cosi-project/runtime/pkg/resource/protobuf"
	"github.com/siderolabs/talos/pkg/machinery/resources/block"
	"go.yaml.in/yaml/v4"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func definition(t *testing.T, spec meta.ResourceDefinitionSpec) *meta.ResourceDefinition {
	t.Helper()

	rd, err := meta.NewResourceDefinition(spec)
	if err != nil {
		t.Fatal(err)
	}

	return rd
}

func testDefinitions(t *testing.T) []*meta.ResourceDefinition {
	t.Helper()

	return []*meta.ResourceDefinition{
		definition(t, meta.ResourceDefinitionSpec{Type: "VolumeStatuses.block.talos.dev", DefaultNamespace: "runtime", Aliases: []resource.Type{"vs"}}),
		definition(t, meta.ResourceDefinitionSpec{Type: "MachineConfigs.config.talos.dev", DefaultNamespace: "config", Aliases: []resource.Type{"mc"}, Sensitivity: meta.Sensitive}),
		definition(t, meta.ResourceDefinitionSpec{Type: "Disks.block.talos.dev", DefaultNamespace: "runtime"}),
	}
}

func TestMapResourceTypes(t *testing.T) {
	got := mapResourceTypes(testDefinitions(t))

	want := []resourceTypeInfo{
		{Type: "Disks.block.talos.dev", Aliases: []string{"disk"}, Namespace: "runtime"},
		{Type: "MachineConfigs.config.talos.dev", Aliases: []string{"mc", "machineconfig", "mcs"}, Namespace: "config", Sensitivity: "sensitive"},
		{Type: "VolumeStatuses.block.talos.dev", Aliases: []string{"vs", "volumestatus"}, Namespace: "runtime"},
	}

	if !equalJSON(t, got, want) {
		t.Error("types differ")
	}
}

func TestMatchResourceType(t *testing.T) {
	defs := testDefinitions(t)

	for _, name := range []string{"VolumeStatuses.block.talos.dev", "volumestatuses.block.talos.dev", "vs", "volumestatus", "VolumeStatuses"} {
		rd, err := matchResourceType(defs, name)
		if err != nil || rd == nil || rd.Type != "VolumeStatuses.block.talos.dev" {
			t.Errorf("%q: %v, %v", name, rd, err)
		}
	}

	// A type this Talos version does not have: nil, no error.
	if rd, err := matchResourceType(defs, smartStatusType); rd != nil || err != nil {
		t.Errorf("unknown type: %v, %v", rd, err)
	}

	ambiguous := append(defs, definition(t, meta.ResourceDefinitionSpec{Type: "Others.block.talos.dev", DefaultNamespace: "x", Aliases: []resource.Type{"vs"}}))
	if _, err := matchResourceType(ambiguous, "vs"); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Errorf("err = %v", err)
	}
}

func TestMapResourceItems(t *testing.T) {
	a := block.NewVolumeStatus(block.NamespaceName, "STATE")
	b := block.NewVolumeStatus(block.NamespaceName, "EPHEMERAL")
	b.Metadata().SetUpdated(time.UnixMilli(1700000000123))

	got := mapResourceItems(block.VolumeStatusType, "runtime", []resource.Resource{a, b})

	if got.Type != block.VolumeStatusType || got.Namespace != "runtime" || got.Truncated || len(got.Items) != 2 {
		t.Fatalf("got %+v", got)
	}

	if got.Items[0].ID != "EPHEMERAL" || got.Items[0].Updated != 1700000000123 || got.Items[0].Phase != "running" || got.Items[0].Namespace != "runtime" {
		t.Errorf("item = %+v", got.Items[0])
	}

	many := make([]resource.Resource, 0, maxResourceItems+5)
	for range maxResourceItems + 5 {
		many = append(many, a)
	}

	if capped := mapResourceItems("t", "n", many); len(capped.Items) != maxResourceItems || !capped.Truncated {
		t.Errorf("capped: %d", len(capped.Items))
	}

	if out, _ := toJSON(mapResourceItems("t", "n", nil)); out != `{"type":"t","namespace":"n","items":[],"truncated":false}` { //nolint:errcheck
		t.Errorf("empty list shape: %s", out)
	}
}

// untypedResource builds a resource the way the COSI client returns one whose type this
// client's machinery does not know: metadata plus the spec as YAML.
func untypedResource(t *testing.T, resourceType, id, specYAML string) resource.Resource {
	t.Helper()

	r, err := protobuf.Unmarshal(&v1alpha1.Resource{
		Metadata: &v1alpha1.Metadata{
			Namespace: "runtime", Type: resourceType, Id: id, Version: "3", Phase: "running",
			Created: timestamppb.New(time.Unix(1700000000, 0)), Updated: timestamppb.New(time.Unix(1700000100, 0)),
		},
		Spec: &v1alpha1.Spec{YamlSpec: specYAML},
	})
	if err != nil {
		t.Fatal(err)
	}

	// What the client does with every resource it receives.
	out, err := protobuf.UnmarshalResource(r)
	if err != nil {
		t.Fatal(err)
	}

	return out
}

func TestUntypedResourceToYAML(t *testing.T) {
	r := untypedResource(t, smartStatusType, "nvme0n1", smartNVMeYAML)

	text, err := resourceToYAML(r)
	if err != nil {
		t.Fatal(err)
	}

	var doc struct {
		Metadata map[string]any `yaml:"metadata"`
		Spec     map[string]any `yaml:"spec"`
	}

	if err := yaml.Unmarshal([]byte(text), &doc); err != nil {
		t.Fatalf("%v\n%s", err, text)
	}

	if doc.Metadata["id"] != "nvme0n1" || doc.Metadata["type"] != smartStatusType || doc.Spec["dev_type"] != "nvme" {
		t.Errorf("yaml = %s", text)
	}

	// A type the client knows renders the same way.
	vol := block.NewVolumeStatus(block.NamespaceName, "STATE")
	vol.TypedSpec().Phase = block.VolumePhaseReady

	text, err = resourceToYAML(vol)
	if err != nil || !strings.Contains(text, "phase: ready") || !strings.Contains(text, "id: STATE") {
		t.Errorf("yaml = %s, err = %v", text, err)
	}
}
