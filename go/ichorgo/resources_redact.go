package ichorgo

import (
	"context"

	"github.com/cosi-project/runtime/pkg/resource"
	"google.golang.org/grpc/metadata"
)

// Talos 1.15 serves a sensitive resource to os:reader and os:operator with its secret
// values replaced, when the call accepts it; before 1.15 (and without the header) the same
// read is PermissionDenied. These mirror upstream v1.15 (pkg/machinery/redactable and
// pkg/machinery/constants), which the machinery this client builds with does not have yet.
const (
	// redactModeHeader is the gRPC metadata key of redactable.WithMode.
	redactModeHeader = "x-talos-dev-redact"
	// redactModeAccept is redactable.ModeAccept: a redacted form is fine when the caller's
	// role may not read the full one. Admins still get the full resource.
	redactModeAccept = "accept"
	// annotationRedacted marks a resource whose spec came back redacted
	// (constants.AnnotationRedacted, value constants.AnnotationRedactedValue).
	annotationRedacted      = "talos.dev/redacted"
	annotationRedactedValue = "true"
	// redactedPlaceholder replaces each secret value (constants.Redacted).
	redactedPlaceholder = "REDACTED"
)

// acceptRedacted asks the node for the redacted form of sensitive resources the role may
// not read in full, like `talosctl get` does on every read. Older nodes ignore the header,
// so it is always sent. Types that have no redacted form stay os:admin only.
func acceptRedacted(ctx context.Context) context.Context {
	md, _ := metadata.FromOutgoingContext(ctx)
	md = md.Copy()
	md.Set(redactModeHeader, redactModeAccept)

	return metadata.NewOutgoingContext(ctx, md)
}

// isRedacted tells whether the node replaced r's secret values (metadata is never redacted).
func isRedacted(r resource.Resource) bool {
	v, ok := r.Metadata().Annotations().Get(annotationRedacted)

	return ok && v == annotationRedactedValue
}
