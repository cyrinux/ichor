# D11. Image vulnerability scan

Status: **Go core done**. Size M (Go M, Android S, iOS S).

Scan an app's images for known vulnerabilities on demand, read the report on the phone and
export it in a standard format (SARIF, CycloneDX) to hand to GitHub code scanning, Defect
Dojo, Dependency-Track or a colleague.

## Choices

- **Where:** Trivy runs in a short-lived pod in the cluster, like the network test: the
  cluster downloads the vulnerability database (~80 MB) and the image layers, not the phone
  over mobile data. Pulling layers to the phone and matching them against OSV.dev would mean
  rewriting Syft in gomobile.
- **Scanner:** Trivy, the de facto open-source standard (OS and language packages in one
  run, the operator below). Grype is the only close alternative, a pure CVE scanner.
- **What:** the images as they run, by digest: the pods' `status.containerStatuses[].imageID`
  (`repo@sha256:…`), not the tag, which may have moved since the pull.
- **Supply chain:** Trivy itself was compromised in March 2026 (v0.69.4–0.69.6 images and
  `latest`, CVE-2026-33634). The image is pinned by digest, the pod gets no service account
  token, and registry credentials only reach it when the pods use `imagePullSecrets`: only
  the registries scanned, in a namespace deleted after the run. If the phone kills the app
  mid-scan, that namespace (and the copy of the credentials) stays until the next scan or the
  next opening of the scan screen sweeps it, once older than any scan can last (50 min).
- **Trivy Operator:** when it runs in the cluster, its `VulnerabilityReport`s are read
  directly: no pod, instant, same report model.

## Go core (done)

- `StartImageScan(config, context, server, pods, options, listener)` (os:admin): reads the pods
  (`[{namespace,pod}]`, as `KubeAppWorkloads`), collects their images by digest and their
  pull secrets (merged into one Docker `config.json` secret), runs one Trivy pod (restricted
  pod security, read-only root, `/work` emptyDir for the DB and layers) that downloads the DB
  once then scans each image in turn. The pod log is followed as it is written: framed by
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

## Android

1. App sheet → "Scan images" (Kubernetes apps, admin kubeconfig): first
   `ImageScanOperatorReports`; offer "Scan now" (always) and show the operator report if any.
2. Progress screen (phase, image i/n, Cancel) like the network test.
3. Report screen: severity chips with counts, "fixable only" toggle (on), one card per image
   (OS, error, base image hint), vulnerabilities grouped by package, row → sheet with
   description, score, link.
4. Export menu: SARIF, CycloneDX, HTML, CSV, JSON via `FileExport.kt`/`shareFile`.

## iOS

Same screens; export through `ShareLink` with a temporary file.

## Later

- Talos system images (`NodeImages`), which are not pods: scan by ref.
- Cache reports per digest on the phone; "new since last scan".
- Grype as a second opinion (same pod pattern, `grype -o json`).
