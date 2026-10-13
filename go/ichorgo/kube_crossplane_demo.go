package ichorgo

import (
	"fmt"
	"slices"
	"time"
)

// demoCrossplane mixes legacy cluster-scoped and v2 namespaced resources. Every provider
// and external identifier is invented; no connection details are part of the report.
func demoCrossplane(now time.Time) crossplaneStatus {
	out := emptyCrossplane()
	out.Installed = true
	out.Version = xpVersion
	condition := func(status, reason, message string, ago time.Duration) xpCondition {
		return xpCondition{Status: status, Reason: reason, Message: message, LastTransition: now.Add(-ago).UnixMilli()}
	}
	for i, name := range []string{"provider-storage", "function-compose", "provider-broken"} {
		resource, kind := "providers", "Provider"
		if i == 1 {
			resource, kind = "functions", "Function"
		}
		p := xpPackage{xpObjectRef: xpReference(groupCrossplanePkg, xpVersion, resource, kind), Package: "registry.example.org/packages/" + name + ":v1", CurrentRevision: name + "-revision", CurrentIdentifier: "registry.example.org/packages/" + name + ":v1", Installed: condition("True", "ActivePackageRevision", "", time.Hour), Healthy: condition("True", "HealthyPackageRevision", "", time.Hour), Health: healthOK, Reasons: []string{}}
		p.Name = name
		if i == 2 {
			p.Installed = condition("False", "PullFailed", "cannot pull package", time.Hour)
			p.Health = healthCritical
			p.Reasons = []string{xpReasonProviderUnhealthy}
		}
		out.Packages = append(out.Packages, p)
		out.Counts.add(p.Health)
	}
	for i, kind := range []string{"XBucket", "XDatabase"} {
		scope, plural := "LegacyCluster", "xbuckets"
		if i == 1 {
			scope, plural = "Namespaced", "xdatabases"
		}
		d := xpDefinition{xpObjectRef: xpReference(groupCrossplaneAPIExt, xpVersion, "compositeresourcedefinitions", "CompositeResourceDefinition"), CompositeGroup: "platform.example.org", CompositeKind: kind, Plural: plural, Scope: scope, Versions: []xpDefinitionVersion{{Name: "v1beta1", Served: true, Referenceable: true}}, Established: condition("True", "WatchingCompositeResource", "", time.Hour), Offered: condition("True", "WatchingCompositeResourceClaim", "", time.Hour), Compositions: 1, CompositionNames: []string{"database-standard"}, Composites: 1, Health: healthOK, Reasons: []string{}}
		d.Name = plural + ".platform.example.org"
		if i == 0 {
			d.ClaimKind = "BucketClaim"
			d.ClaimPlural = "bucketclaims"
			d.Compositions = 2
			d.CompositionNames = []string{"bucket-small", "bucket-standard"}
		}
		// Still show the namespaced XR already created while its definition recovers.
		if i == 1 {
			d.Established = condition("False", "ReconcileError", "waiting for the generated CRD", time.Hour)
			d.Health = healthWarning
			d.Reasons = []string{xpReasonNotEstablished}
		}
		out.Definitions = append(out.Definitions, d)
		out.Counts.add(d.Health)
	}
	makeObject := func(group, resource, kind, name, namespace string) xpObject {
		x := mapCrossplaneObject(xpResourceObject{}, xpReference(group, "v1beta1", resource, kind), now)
		x.Name = name
		x.Namespace = namespace
		x.Ready = condition("True", "Available", "", time.Hour)
		x.Synced = condition("True", "ReconcileSuccess", "", time.Hour)
		return x
	}
	x := makeObject("platform.example.org", "xbuckets", "XBucket", "assets", "")
	x.Composition = "bucket-standard"
	x.Resources = 1
	x.Claim = "assets-claim"
	out.Composites = append(out.Composites, x)
	x = makeObject("platform.example.org", "xdatabases", "XDatabase", "app-db", "apps")
	x.Composition = "database-standard"
	x.Resources = 2
	out.Composites = append(out.Composites, x)
	x = makeObject("platform.example.org", "bucketclaims", "BucketClaim", "assets-claim", "apps")
	x.Composite = "assets"
	out.Claims = append(out.Claims, x)
	for i := range 9 {
		group, resource, kind, namespace := "storage.example.org", "buckets", "Bucket", ""
		if i >= 4 {
			group, resource, kind, namespace = "sql.example.org", "databases", "Database", "apps"
		}
		o := xpResourceObject{}
		o.Metadata.Name = fmt.Sprintf("resource-%d", i+1)
		o.Metadata.Namespace = namespace
		o.Metadata.Annotations = map[string]string{"crossplane.io/external-name": fmt.Sprintf("external-%d", i+1)}
		o.Metadata.Labels = map[string]string{"crossplane.io/composite": "assets"}
		o.Spec.ProviderConfigRef.Name = "example-auth"
		o.Status.Conditions = kubeConditions{{Type: "Ready", Status: "True", Reason: "Available"}, {Type: "Synced", Status: "True", Reason: "ReconcileSuccess"}}
		switch i {
		case 0:
			o.Status.Conditions[1] = kubeCondition{Type: "Synced", Status: "False", Reason: "ReconcileError", Message: "AccessDenied: User is not authorized to create this bucket", LastTransitionTime: now.Add(-time.Hour).Format(time.RFC3339)}
		case 1:
			o.Status.Conditions[0] = kubeCondition{Type: "Ready", Status: "False", Reason: "Creating", LastTransitionTime: now.Add(-2 * time.Minute).Format(time.RFC3339)}
		case 2:
			o.Status.Conditions[0] = kubeCondition{Type: "Ready", Status: "False", Reason: "Unavailable", LastTransitionTime: now.Add(-time.Hour).Format(time.RFC3339)}
		case 3:
			o.Metadata.Annotations["crossplane.io/paused"] = "true"
			o.Status.Conditions[1] = kubeCondition{Type: "Synced", Status: "False", Reason: "ReconcilePaused", LastTransitionTime: now.Add(-time.Hour).Format(time.RFC3339)}
		case 4:
			o.Status.Conditions[0] = kubeCondition{Type: "Ready", Status: "False", Reason: "Deleting", LastTransitionTime: now.Add(-time.Minute).Format(time.RFC3339)}
		}
		m := mapCrossplaneObject(o, xpReference(group, "v1beta1", resource, kind), now)
		out.Managed = append(out.Managed, m)
		out.Counts.add(m.Health)
	}
	out.ManagedTotal = len(out.Managed)
	for _, x := range out.Composites {
		out.Counts.add(x.Health)
	}
	for _, x := range out.Claims {
		out.Counts.add(x.Health)
	}
	slices.SortFunc(out.Packages, byHealthThenKey(func(x xpPackage) (string, string) { return x.Health, x.key() }))
	slices.SortFunc(out.Definitions, byHealthThenKey(func(x xpDefinition) (string, string) { return x.Health, x.key() }))
	sortCrossplaneObjects(out.Composites)
	sortCrossplaneObjects(out.Claims)
	sortCrossplaneObjects(out.Managed)
	return out
}
