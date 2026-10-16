package ichorgo

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestCheckupCrossplane(t *testing.T) {
	_, k, groups := crossplaneAPI(t)

	s := checkupCrossplane(context.Background(), k, checkupInput{now: fixtureNow, groups: groups})
	if s.ID != checkCrossplane || s.Status != healthCritical || s.Error != "" || s.Checked != 13 {
		t.Fatalf("section %+v", s)
	}

	want := []string{
		"crossplaneProviderUnhealthy:critical:Provider/provider-broken",
		"crossplaneSyncFailed:critical:Bucket/broken-bucket",
		"crossplaneDefinitionNotEstablished:warning:CompositeResourceDefinition/xbroken.platform.example.org",
		"crossplaneNotReady:warning:Database/old-db",
	}
	if got := findingKinds(s); !reflect.DeepEqual(got, want) {
		t.Fatalf("findings %v", got)
	}

	provider, notReady := s.Findings[0], s.Findings[3]
	if provider.Extra != "registry.example.org/provider/broken:v1" || provider.Reason != "PullFailed" || provider.Message != "cannot pull package" {
		t.Errorf("provider %+v", provider)
	}

	if notReady.Namespace != "apps" || notReady.Reason != "Creating" || notReady.Count != 60 || notReady.Since != fixtureNow.Add(-time.Hour).UnixMilli() {
		t.Errorf("not ready %+v", notReady)
	}

	for _, f := range s.Findings {
		if strings.Contains(f.Name, "paused-db") {
			t.Errorf("a paused resource is a finding: %+v", f)
		}
	}
}

func TestCheckupCrossplaneAbsent(t *testing.T) {
	s := checkupCrossplane(context.Background(), nil, checkupInput{now: fixtureNow, groups: map[string]string{"cilium.io": "v2"}})
	if s.Status != checkAbsent || s.Findings == nil {
		t.Fatalf("absent %+v", s)
	}
}

// A forbidden listing is the section's error; what the other lists show still counts.
func TestCheckupCrossplaneForbidden(t *testing.T) {
	f, k, groups := crossplaneAPI(t)
	f.answerWith("GET /apis/pkg.crossplane.io/v1/providers", 403, `{"kind":"Status","reason":"Forbidden","message":"listing denied"}`)

	s := checkupCrossplane(context.Background(), k, checkupInput{now: fixtureNow, groups: groups})
	if !strings.Contains(s.Error, "/apis/pkg.crossplane.io/v1/providers") || s.Status != healthCritical {
		t.Fatalf("section %+v", s)
	}

	if got := findingKinds(s); got[0] != "crossplaneSyncFailed:critical:Bucket/broken-bucket" {
		t.Fatalf("findings %v", got)
	}
}

func TestCrossplaneObjectFindings(t *testing.T) {
	now := fixtureNow
	ref := xpObjectRef{Kind: "XDatabase", Namespace: "apps", Name: "app-db"}
	long := strings.Repeat("quota exceeded for account 000000000000; ", 20)

	for _, tc := range []struct {
		name string
		x    xpObject
		want string
	}{
		{"sync failed", xpObject{xpObjectRef: ref, Reasons: []string{xpReasonSyncFailed}, Synced: xpCondition{Status: "False", Reason: "ReconcileError", Message: long}}, findCrossplaneSyncFailed},
		{"paused", xpObject{xpObjectRef: ref, Paused: true, Reasons: []string{xpReasonSyncFailed, xpReasonPaused}}, ""},
		{"deleting", xpObject{xpObjectRef: ref, Reasons: []string{xpReasonNotReady, xpReasonDeleting}}, ""},
		{"not ready", xpObject{xpObjectRef: ref, Reasons: []string{xpReasonNotReady}, LastTransition: now.Add(-20 * time.Minute).UnixMilli()}, findCrossplaneNotReady},
		{"healthy", xpObject{xpObjectRef: ref, Reasons: []string{}}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := xpObjectFindings(tc.x, "database-standard", now)
			if tc.want == "" {
				if len(got) != 0 {
					t.Fatalf("findings %+v", got)
				}

				return
			}

			if len(got) != 1 || got[0].Kind != tc.want || got[0].Name != "XDatabase/app-db" || got[0].Namespace != "apps" || got[0].Extra != "database-standard" {
				t.Fatalf("findings %+v", got)
			}

			if len(got[0].Message) > xpMessageLimit+len("…") {
				t.Errorf("message not clipped: %d bytes", len(got[0].Message))
			}

			if tc.want == findCrossplaneNotReady && got[0].Count != 20 {
				t.Errorf("minutes %d", got[0].Count)
			}
		})
	}
}
