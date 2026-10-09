# D12. Image vulnerability scan

Status: **implemented** (Go, Android, iOS). Size M (Go M, Android S, iOS S).

Scan an app's images for known vulnerabilities on demand, read the report on the phone and
export it in a standard format (SARIF, CycloneDX) to hand to GitHub code scanning, Defect
Dojo, Dependency-Track or a colleague.

## Choices

- **Where:** Trivy runs as a Job in the cluster: the cluster downloads the vulnerability
  database (~80 MB) and the image layers, not the phone over mobile data. The Job runs in one
  `ichor-imagescan` namespace (restricted pod security), created on the first scan and kept,
  with `backoffLimit: 0`, `activeDeadlineSeconds` (40 min) and `ttlSecondsAfterFinished`
  (10 min): the app deletes it once it read the log, and when the phone killed the app
  mid-scan Kubernetes deletes it, its pod and its credentials by itself. Pulling layers to the phone and matching them against OSV.dev would mean
  rewriting Syft in gomobile.
- **Scanner:** Trivy, the de facto open-source standard (OS and language packages in one
  run, the operator below). Grype is the only close alternative, a pure CVE scanner.
- **What:** the images as they run, by digest: the pods' `status.containerStatuses[].imageID`
  (`repo@sha256:…`), not the tag, which may have moved since the pull.
- **Supply chain:** Trivy itself was compromised in March 2026 (v0.69.4–0.69.6 images and
  `latest`, CVE-2026-33634). The image is pinned by digest, the pod gets no service account
  token, and registry credentials only reach it when the pods use `imagePullSecrets`: only
  the registries scanned, copied into a Secret owned by the Job, so garbage-collected with
  it (at the latest 50 min after the scan started).
- **Trivy Operator:** when it runs in the cluster, its `VulnerabilityReport`s are read
  directly: no pod, instant, same report model.

## Go core (done)

- `StartImageScan(config, context, server, pods, options, listener)` (os:admin): reads the pods
  (`[{namespace,pod}]`, as `KubeAppWorkloads`), collects their images by digest and their
  pull secrets (merged into one Docker `config.json` secret owned by the Job), runs one Trivy
  Job (restricted pod security, read-only root, `/work` emptyDir for the DB and layers) whose
  pod downloads the DB once then scans each image in turn. The pod log is followed as it is written: framed by
  nonce markers, it gives the progress (database, image i of n) and each image's Trivy JSON.
  A follow cut short is resumed; once the pod ended, the whole log is read once more.
  Checked against a real Trivy 0.75.0 run of the script (alpine 3.18.0, node-exporter
  v1.5.0, a missing image); SARIF and CycloneDX output validate against their schemas.
  Options: `dbRepository`, `javaDBRepository` (mirrors for air-gapped clusters).
- `ImageScanOperatorReports(config, context, server, pods)`: the operator's reports for the
  same images (read by the pods' controller labels, matched by digest), `available:false`
  when the CRD is absent.
- `ImageScanExport(report, format)`: `sarif` (2.1.0), `cyclonedx` (1.6 JSON, components with
  purls + vulnerabilities), `csv`, `html` (self-contained, printable to PDF), `json`.
- Report model: per image ref, digest, OS, pods, error, severity summary (+ fixable, OS
  package count for the "bump the base image" hint), vulnerabilities sorted by severity,
  fixable first, score.
- Demo mode: a canned report.
- Talos system images: `TalosSystemImages(config, context, node)` (os:admin, reads the
  machine config) lists the installer, kubelet and, on a control plane node, etcd,
  kube-apiserver, controller-manager, scheduler, CoreDNS and kube-proxy images, with the
  digest the node pulled (system or CRI containerd namespace), then the system namespace's
  other images. `StartImageScan` takes them as `options.images` (refs with no pod, validated
  against a strict ref pattern, pulled without credentials) with `pods=[]`. Probe:
  `system-images NODE`, `image-scan REF...`.

## Android (done)

- App sheet → "Vulnerabilities" section (Kubernetes apps, admin kubeconfig): the Trivy
  Operator's report when it has one (`ImageScanOperatorReports`, read when the sheet opens),
  else a hint naming the `ichor-imagescan` namespace; "Scan images" / "Scan again".
- The scan is kept app-wide by `ImageScanRepository` (one at a time), so it goes on when the
  sheet or the Apps screen closes: phase, image i of n, Stop.
- Report sheet: severity pills, "fixable only" (on) and severity filters, one block per image
  (OS, digest, pods, error, base image hint), findings grouped by package; a finding opens in
  place (description, CVSS, advisory link).
- Export menu: HTML, SARIF, CycloneDX, CSV, JSON, written by the core and shared as a file
  (`cache/scans`, one at a time, removed when the report closes).

## iOS (done)

Same as Android: `AppScanSection` in the app sheet, the scan kept app-wide by
`ImageScanJob.shared`, `ImageScanReportView` pushed from the sheet with its filters and
expandable findings, and an Export menu (core-written file in `tmp/scans`, kept an hour,
shown in the system share sheet). Models and their logic in `IchorCore/ImageScan.swift`.

## Later

- Talos system images in the apps (Node → Images: "Scan system images"); the Go core is done.
- Cache reports per digest on the phone; "new since last scan".
- Grype as a second opinion (same pod pattern, `grype -o json`).
