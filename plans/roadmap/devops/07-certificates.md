# D7. cert-manager renew and TLS expiry of routes

Status: **partial** (read-only). Size S.

## What exists today

- `kube_certmanager.go`: Certificates and Issuers with health (`expired`, `expiring` < 14 d,
  `renewalOverdue`, `notReady`, issuer problems), inside `KubeDataServices`; Android
  `CertificatesTab.kt`, iOS `CertificatesList.swift`; background alerts (`monitor/DataIssues.kt`, `Monitor.swift`).
- Routes: `kube_routes_ingress.go` uses `spec.tls` only to choose https.

## Plan

1. **Renew** (like `cmctl renew`): `KubeRenewCertificate(cfg, ctx, server, ns, name)` adds the
   condition `{type: Issuing, status: "True", reason: "ManuallyTriggered", message: "Certificate re-issuance manually triggered (ichor)"}`
   via a merge patch on the `status` subresource (`…/certificates/<n>/status`). Button on a
   Certificate row ("Renew now"), confirm, then poll until `Ready` again or show the failing
   CertificateRequest/Order message (already read for issuer errors). Also "Renew all expiring".
2. **TLS of routes not managed by cert-manager**: for each Ingress `spec.tls[].secretName` and
   Gateway listener `certificateRefs`, read the Secret's `tls.crt` and parse x509 `NotAfter`,
   SANs, issuer (only the leaf; key never read). Shown on the app's web addresses (expiry badge)
   and merged into the Certificates tab as "Route certificates"; alerts reuse `certHealth` thresholds.
   Secrets are read only when the route lists one; the cert-manager-owned ones are skipped
   (annotation `cert-manager.io/certificate-name`).

Go tests: status patch body, x509 parsing with generated certs. Both apps: button + section.
