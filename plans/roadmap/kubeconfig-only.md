# Study: kubeconfig-only clusters (no Talos)

Status: **study**, checked against the code on 2026-10-06. Nothing is built.

Goal: add a cluster from a kubeconfig alone, with no talosconfig, and use every Kubernetes feature
of the app on it. Target auth methods: ServiceAccount token and client cert, kubectl
`oidc-login` (kubelogin), AWS EKS, GCP GKE. Later: Azure AKS, DigitalOcean, Rancher (§5.5–5.7).

## 1. Where we are

- **A cluster is a talosconfig context.** `ContextSummary` (`config.go:22`), its fingerprint
  (`sha256(name + Talos CA)`, `config.go:139`), the stored list (`ConfigRepository.kt`,
  `SecureConfigStore.swift`) and the import flow (`ImportScreen.kt`, `ImportView.swift`, validated
  by `ParseConfig`) only know talosconfigs.
- **The kubeconfig always comes from Talos.** `openKubeClientForContext`
  (`kube_client_cache.go:121`) opens a Talos session and calls `fetchKubeconfig`
  (`kubeconfig.go:31`, needs `os:admin`). It is kept in memory for 30 min; the only "refresh" is
  to drop the client on 401 and fetch a new kubeconfig from Talos (`callKube`, `kube_client.go:482`).
- **The Kubernetes client is our own** (`kube_client.go`, no client-go). `parseKubeconfig`
  (`kube_config.go:57`) already accepts:
  - `client-certificate-data` + `client-key-data`;
  - a static `token` (ServiceAccount);
  - `certificate-authority-data`, `insecure-skip-tls-verify`, `tls-server-name`, `namespace`.

  It **rejects** `exec` and `auth-provider` (`kube_config.go:149`) and ignores the file-path
  fields (`certificate-authority`, `client-certificate`, `client-key`, `tokenFile`) and
  `proxy-url`.
- **Feature split** (48 Android routes):
  - Kubernetes API only (~15–18 routes): Workloads, pods, cronjobs, actions, diff, Argo CD,
    Flux, data services, routes/apps, netpol/Cilium/Hubble, API health, Prometheus via service
    proxy, pod exec.
  - Talos API only (~25 routes): Overview (the home screen), nodes and node tabs, health, etcd,
    KubeSpan, logs/containers/processes, network, hardware, storage, resources, machineconfig,
    images, capture, upgrade, issue config, machined events, **Audit** (reads apiserver audit
    files through Talos, `kube_audit.go:92`).
  - Mixed: maintenance (cordon/drain is Kubernetes, reboot is Talos), upgrade plan, diagnose,
    netperf (node identity from Talos), `KubeNodeName` (`kube_pods_selected.go:18`).

Conclusion: the Kubernetes half of the app is already isolated behind one seam
(`kubeTarget` → `openKubeClientForContext`). The work is (a) a second kind of cluster, (b) a
credential layer that can mint and refresh tokens in Go, (c) hiding Talos screens and giving
kube-only clusters a home screen.

## 2. Why it is worth it

- ServiceAccount and client-cert kubeconfigs work **today** once there is an import path and a
  storage slot: low cost, real value (k3s, kubeadm, RKE2, managed clusters with a static SA).
- It also helps Talos users: a Talos user with `os:reader` cannot fetch the admin kubeconfig, so
  every Kubernetes feature is dead for them. Letting a Talos cluster use an **imported kubeconfig**
  (their own RBAC identity, maybe OIDC) instead of the Talos fetch fixes that and is safer than
  handing out `os:admin` (see §6, "Hybrid").
- **Positioning: a kubenav competitor.** Kubeconfig clusters are first-class, not a "limited"
  mode: any Kubernetes user can install the app, and Talos clusters get the extra node/etcd/OS
  screens on top. The app already goes further than kubenav on the GitOps and operations side
  (Argo CD, Flux, data services, Cilium/Hubble, API health, diff, drain); §7 lists what kubenav
  has that we lack. The store listing, README and onboarding have to say "Kubernetes, and Talos"
  instead of "Talos".
- Today the app points users *to* kubenav (kubeconfig export + "Open kubenav",
  `KubeconfigSection.kt:120`, `AndroidManifest.xml:20`). Once kubeconfig clusters ship, that
  export stays useful (desktop kubectl), the kubenav hand-off can go.

## 3. Model and storage

- **Cluster kind.** Add `kind: "talos" | "kube"` to `ContextSummary` (Go, `Models.kt`,
  `Models.swift`). For `kube`: `endpoints` = API server URL, `nodes`/`roles` empty, `certNotAfter`
  from the client cert when there is one, else from the token `exp` when it is a JWT.
- **Fingerprint.** `sha256("kube" + context name + server + cluster CA)` so per-cluster settings
  (colors, names, VPN-only, kube server override) keep working unchanged.
- **Storage.** A second sealed file next to `talosconfig.enc`: `kubeconfig.enc` (Android
  `SecureStore`, iOS `SecureConfigStore` with a second Keychain item). Content: one merged
  kubeconfig (all imported contexts, flattened) plus a sealed **auth state** per user (OIDC refresh
  token, AWS SSO token, cached access token + expiry). Same protection as the talosconfig:
  AndroidKeyStore/StrongBox, Secure Enclave, `WhenUnlockedThisDeviceOnly`.
- **Passing to Go.** Every exported function takes `(configYAML, contextName)`. Keep that
  signature: for a kube cluster the platform passes the kubeconfig YAML. Go tells the two apart
  the same way it tells demo apart (`isDemoContext`, `demo.go:56`): a kubeconfig has
  `kind: Config` / `clusters:`. That avoids touching ~400 exported kube functions.
  - `openKubeClientForContext`: when the YAML is a kubeconfig, call `openKubeClient` directly with
    it (no Talos endpoint fallback: that relies on Talos putting node IPs in the API cert).
  - `openSession`: return a new `errTalosUnavailable` for kube clusters, exactly like
    `errDemoUnavailable` (`client.go:38`), so a missed UI gate fails cleanly.
- **Auth state write-back.** Refresh tokens rotate, so Go must hand new auth state back to the
  platform for sealing. Use a gomobile callback interface (`AuthStore { Save(key, blob) }`)
  registered once at startup, implemented by `ConfigRepository` / `AppModel`. Go never writes to
  disk.

- **No talosconfig at all** must be a valid state: today `NoConfigException`, the
  `ConfigUnreadableScreen` and `forCall()` (`ConfigRepository.kt:211`) assume a talosconfig
  exists. The cluster list becomes "talosconfig contexts + kubeconfig contexts", one list in the
  switcher, sorted together, Talos/Kubernetes badge on each row.

## 4. Import: same paths as a talosconfig

Every way a talosconfig gets in today works for a kubeconfig, through the same screen; the user
does not choose a type, the app detects it.

- **File** (`OpenDocument`, `.fileImporter`), **Paste**, **QR** (`QrScanner.kt`,
  `ImportView.swift`): sniff the YAML (talosconfig → today's flow, kubeconfig → kubeconfig flow).
- **"Open with" / share sheet**: register for `application/yaml`, `text/plain` and files named
  `config` / `*.kubeconfig` / `*.yaml`, as backups already arrive (`IncomingBackup.kt`), so
  sending a kubeconfig from mail, a file manager or AirDrop lands in the import preview.
- **Preview**: new Go `ParseKubeconfig(yaml)` returns one row per context, like the talosconfig
  preview: context name, server, CA present, namespace, **auth method** (`cert`, `token`,
  `oidc`, `eks`, `gke`, `unsupported: why`), expiry (client cert `NotAfter`, JWT `exp`).
- The user picks contexts to import (a typical `~/.kube/config` has many). Conflicts by
  fingerprint, same UI as `ImportConflicts`; "replace credentials" for an existing context works
  like `replaceCredentials` (`ConfigRepository.kt:152`), which is how a user rotates an SA token
  or a client cert.
- **QR size**: a QR code holds ~2.9 KB in binary mode. A ServiceAccount kubeconfig fits; one with
  an embedded client cert + key + CA (~5–8 KB of base64 PEM) does not. Accept a compressed form
  (`ichor-kubeconfig:` + base64url(gzip(yaml)), ~2× smaller) and document the desktop one-liner
  next to the existing `qrencode` tip (`QrScanner.kt:46`). Same trick helps large talosconfigs.
- **Export** (`KubeconfigSection`) works for both kinds: Talos clusters export the fetched admin
  kubeconfig as today; kube clusters export their stored kubeconfig (secrets of sign-in methods
  never leave, only the kubeconfig as imported).
- Reject with a fix hint instead of failing later:
  - file paths (`client-certificate`, `certificate-authority`, `tokenFile`): "run
    `kubectl config view --flatten --minify` and import that";
  - `http://` servers (already refused), `proxy-url` (not supported, say so);
  - an exec plugin we do not recognise: show the command and say which ones are supported.

## 4b. Backup and restore

The app backup (`backup.go`, `BackupManager.kt`, `Backup.kt`, Android ⇄ iOS) carries the
talosconfig and per-cluster settings keyed by fingerprint. Kubeconfig clusters go in the same
file:

- `BackupPayload` gains `kubeconfig: String?` (the merged, flattened kubeconfig) and
  `kubeAuth: Map<fingerprint, KubeAuthBackup>` for the long-lived secrets the user typed that are
  not in the kubeconfig: AWS access keys, GCP service account JSON, AWS SSO start URL/region,
  OIDC issuer settings. Per-cluster options (`BackupCluster`: name, color, VPN-only, kube server)
  already key by fingerprint, so they work unchanged once kube fingerprints exist.
- **Session tokens are not backed up** (OIDC refresh tokens, AWS SSO tokens, cached access
  tokens): they are tied to a sign-in on this device, often single-use on rotation, and a
  restored copy racing the original would log both out. After restore, those clusters show "Sign
  in again". Static secrets are backed up: the backup is already Argon2id + AES-GCM with a
  12-char minimum passphrase, the same protection the talosconfig's client keys get.
- **Format bump.** `talosconfig` becomes optional and `validateBackupPayload`
  (`backup.go:214`) accepts "at least one cluster in the talosconfig *or* the kubeconfig", and
  validates the kubeconfig with `ParseKubeconfig`. Write `format: 2` only when the backup holds
  kubeconfig clusters, `1` otherwise: an older app then refuses a v2 file with
  "update the app" (`backup.go:228`) instead of silently restoring without the kube clusters
  (`ignoreUnknownKeys` would drop the field), and v1 backups stay readable by old apps.
- `restore()` (`BackupManager.kt:74`) replaces both stores before the per-cluster settings, same
  "config first, nothing else changes on failure" order. Backup size limit (4 MiB) is plenty.

## 5. Credentials in Go: `tokenSource`

Replace `kubeClient.token string` with a small interface, used on every request
(`kube_client.go:388`) and by pod exec (`kube_exec.go`):

```go
type tokenSource interface {
	Token(ctx context.Context) (string, time.Time, error) // cached until ~1 min before expiry
	Invalidate()                                          // called on 401, then retry once
}
```

Implementations: `staticToken`, `oidcToken`, `eksToken`, `gkeToken`. `callKube` gains "on 401,
invalidate and retry once" before dropping the client. A source that needs a human (refresh token
expired or revoked) returns `errLoginRequired{cluster, method}`; the UI shows a "Sign in again"
banner on that cluster instead of a generic error.

Exec plugins cannot run on a phone, so we **recognise** the well-known ones from their
`command`/`args`/`env` and reimplement them in Go. Anything else stays rejected.

### 5.1 ServiceAccount and client cert: works today

- Nothing new in Go apart from the import path and file-path rejection.
- Add a help sheet with the YAML to create a dedicated SA (`view` or a custom ClusterRole) and a
  `kubernetes.io/service-account-token` Secret, since TokenRequest tokens expire (1 h default) and
  cannot be refreshed from a kubeconfig. Recommend read-only + targeted verbs, never
  `cluster-admin`.
- Warn on expiry when the token is a JWT with `exp`, as the app does for Talos certs.

### 5.2 OIDC: kubelogin and legacy `auth-provider: oidc`

Recognised forms:
- `exec` with `command: kubectl` + `args: [oidc-login, get-token, --oidc-issuer-url=…,
  --oidc-client-id=…, --oidc-client-secret=…, --oidc-extra-scope=…, --grant-type=…]` (or
  `command: kubelogin` / `kubectl-oidc_login`).
- `auth-provider: {name: oidc, config: {idp-issuer-url, client-id, client-secret, id-token,
  refresh-token, extra-scopes}}`: use the stored refresh token right away.

Flow:
- Go: discovery (`/.well-known/openid-configuration`), PKCE pair, code exchange, refresh. The
  bearer sent to the API server is the **ID token** (that is what kubelogin sends). Dependency:
  `golang.org/x/oauth2` (small, pure Go); ID token parsing only needs `exp`, the API server checks
  the signature, so no JOSE library.
- Platform: open the authorization URL in Custom Tabs (Android) /
  `ASWebAuthenticationSession` (iOS) and return the code.
- Redirect URI is the hard part. kubelogin registers `http://localhost:8000` (and `:18000`) with
  the IdP; to avoid asking admins to change their IdP, the app listens on the same loopback port
  while the browser is open (works on Android; on iOS the app stays active during
  `ASWebAuthenticationSession`, needs a device test). For IdPs the admin can configure, the
  private-use scheme `name.levis.ichor:/oidc` (RFC 8252 §7.1) is the cleanest native redirect.
  Never a "copy this code back into the app" page (kubenav's approach, §7b). Fallback:
  **device code** grant (RFC 8628, polled until approved,
  kubelogin `--grant-type=device-code`), supported by Keycloak, Dex, Entra ID, Okta, Google: no
  redirect at all, the most robust option on a phone.
- Store the refresh token sealed; the ID token only in memory.
- Covers more than "kubelogin users": GKE with Identity Service, EKS with an associated OIDC
  provider, Rancher/Pinniped-style setups that end in OIDC, AKS later (Azure `kubelogin` is
  Entra ID OAuth with `--server-id` as audience, device code flow).

### 5.3 AWS EKS

Recognised forms: `aws eks get-token --cluster-name X [--region R] [--role-arn A]`
(`command: aws`), `aws-iam-authenticator token -i X [-r A]`; `AWS_PROFILE`/`AWS_REGION` from
`exec.env`.

Token: `"k8s-aws-v1." + base64url(presigned STS GetCallerIdentity URL)`, signed with SigV4,
header `x-k8s-aws-id: <cluster>` included in the signature, `X-Amz-Expires=60`; the API server
accepts it for ~15 min. Pure Go: SigV4 is ~150 lines over `crypto/hmac`, or add
`github.com/aws/aws-sdk-go-v2/aws/signer/v4` + `credentials` (bigger binary; measure with the
gomobile AAR size before choosing). Optional `sts:AssumeRole` for `--role-arn`, same signer.

AWS credentials are the real question, the kubeconfig has none:
- **IAM Identity Center (SSO) device flow** (recommended): user enters start URL + region once,
  Go runs `sso-oidc` RegisterClient/StartDeviceAuthorization/CreateToken, the user approves in
  the browser, then `sso` GetRoleCredentials (account + role picker) gives short-lived keys.
  Store the SSO refresh token sealed. Same UX as `aws sso login`.
- **Static access key** (fallback): access key id + secret typed or pasted, sealed. Discourage it
  in the UI (long-lived IAM keys on a phone), suggest a dedicated IAM user mapped to a read-only
  access entry.
- The IAM principal must be mapped in the cluster (EKS access entries or `aws-auth`), outside
  the app's control: explain the 401/403 clearly.
- Endpoint: EKS endpoints are public or private; private ones need the VPN, the existing
  VPN-only setting applies.

### 5.4 GCP GKE

Recognised form: `command: gke-gcloud-auth-plugin` (and the legacy `auth-provider: gcp`). The
plugin just returns a Google OAuth2 **access token** (scope `cloud-platform` or
`userinfo.email`).

Ways to get one on a phone:
- **Service account JSON key**: JWT bearer grant with `golang.org/x/oauth2/google`
  (`JWTConfigFromJSON`), pure Go, no Google review. Paste or import the JSON, seal it. Same
  caveat as AWS static keys.
- **User sign-in**: OAuth with an Ichor-owned Google client ID. `cloud-platform` is a sensitive
  scope, so the app needs Google's OAuth verification, and Google's policy for native apps
  wants their SDK / custom scheme flows. A blocker for a side project; skip unless asked.
- **GKE Identity Service / Workforce Identity**: ends in OIDC (§5.2), no Google client needed.

Recommendation: service account key first, OIDC for user identity, no Google user sign-in.

### 5.5 Azure AKS (later)

Two kinds of AKS kubeconfig:
- **Local accounts**: client cert in the kubeconfig, works with K1 as is.
- **Entra ID** (the common enterprise setup): `exec` with `command: kubelogin` and
  `args: [get-token, --login, devicecode|interactive|spn|azurecli, --server-id <app id>,
  --client-id <app id>, --tenant-id <tenant>, --environment …]`. Recognise it and mint an Entra
  token with audience `<server-id>/.default` through the K2 OIDC code: device code (works with
  the client id from the kubeconfig) or PKCE with a redirect. `--login spn` → client credentials
  grant with a stored client id + secret (sealed, backed up like other static secrets).
  `--login azurecli` / `workloadidentity` / `msi` have no meaning on a phone: map them to device
  code with the same server id.

Discovery (K7): an Entra token for `https://management.azure.com/.default`, then ARM
`GET /subscriptions/{id}/providers/Microsoft.ContainerService/managedClusters` and
`POST …/listClusterUserCredential`. Clusters with local accounts disabled return the `kubelogin`
kubeconfig, which goes through the recognition above (kubenav gets no credentials there).

### 5.6 DigitalOcean (later)

- The kubeconfig downloaded from the control panel has a **static token valid 7 days**: works
  with K1, with an expiry warning; kubenav never refreshes it.
- `doctl kubernetes cluster kubeconfig save` writes `exec: doctl kubernetes cluster kubeconfig
  exec-credential --version=v1beta1 <cluster id>`. That command calls the DigitalOcean API for
  short-lived cluster credentials (token + `expires_at`). Recognise it: the user gives a
  **scoped DigitalOcean API token** (read access to Kubernetes, nothing else), Go calls the
  credentials endpoint and refreshes before `expires_at`. The cluster id comes from the args.
- Discovery (K7): the same token lists the account's clusters, so no kubeconfig is needed.

### 5.7 Rancher (later)

- Rancher-generated kubeconfigs point at `https://<rancher>/k8s/clusters/<id>` with a static
  Rancher API token (`kubeconfig-u-…`): works with K1; expiry from the token's TTL. Contexts for
  the **authorized cluster endpoint** (direct to the downstream API server) are ordinary contexts.
- When Rancher is set to not generate tokens (`kubeconfig-generate-token=false`) the kubeconfig
  uses `exec: rancher token --server … --user … --auth-provider …`. Recognise it and ask for a
  Rancher **API key** (created by the user under "Account & API Keys", ideally scoped to a
  cluster and with a TTL) rather than storing a password as kubenav does.
- Discovery (K7): with the API key, list `/v3/clusters` and call each cluster's
  `generateKubeconfig` action.
- SSO-only Rancher users (GitHub, Keycloak, Entra behind Rancher): their CLI login flow is
  Rancher-specific; study it when this phase starts, API keys first.

### 5.8 Static-token providers

Linode/Akamai LKE, Scaleway Kapsule, OVHcloud, Civo, self-hosted k3s/kubeadm/RKE2: their
kubeconfigs carry a token or a client cert, so they work with K1 and need no provider code.
Discovery for them is not planned.

## 6. UI

- **Capabilities.** Expose `capabilities` on the cluster (`talos`, `kube`, later `audit`) and
  hide routes by capability in `Navigation.kt` and the iOS equivalents. Mixed screens:
  maintenance shows cordon/drain only; diagnose skips Talos checks; netperf takes node names from
  `/api/v1/nodes`.
- **Home for kube clusters.** Overview is Talos (`overview.go`). Add a Kubernetes overview built
  on what already exists: nodes from `/api/v1/nodes` (Ready, version, roles from labels,
  pressure conditions, allocatable), API health (`kube_apihealth.go`), unhealthy pods, Argo/Flux
  summaries. Probably useful as a section for Talos clusters too.
- **Restricted RBAC.** Kube-only users are often not cluster-admin: every list must survive 403
  (empty + "no permission" chip, not an error screen). Ties in with `large-clusters.md` L6 (typed
  namespace, namespace from the context). Disable action buttons after a `SelfSubjectAccessReview`
  (cheap, one call per action kind) instead of failing on tap.
- **Sign-in UX.** A per-cluster "Signed in as … / expires …/ Sign out" row; "Sign in again"
  banner on `errLoginRequired`; background widgets/alerts polling must not open a browser, they
  report "sign-in needed" instead.
- **Hybrid (later).** For a Talos cluster, a "Kubernetes access: Talos admin kubeconfig (default) /
  my kubeconfig" choice, pointing at an imported kube context. Fixes `os:reader` users and lets
  teams use their OIDC identity for Kubernetes while keeping Talos for nodes.

## 7. Gap with kubenav

kubenav is a generic resource browser (any kind, CRDs included) with YAML view/edit, logs,
exec, port-forward, Helm, events, Prometheus dashboards and cloud imports (AWS, Azure, GCP,
DigitalOcean, Rancher, OIDC). Against the Kubernetes side of this app:

| kubenav has | Here | Gap |
|---|---|---|
| Kubeconfig import, cloud sign-ins | none (this study) | K1–K4, K6–K9 |
| Browse any kind, CRDs (API discovery) | typed lists only (workloads, pods, cronjobs, routes, netpol, operators) | **generic browser**: `/api` + `/apis` discovery, table view via `Accept: application/json;as=Table` (columns come from the server, no per-kind code), namespaced filter, search. M |
| YAML view / edit / apply | read-only views, Argo/Flux diff | YAML view for every object (S); edit + server-side dry-run + apply with a diff before confirm (M) |
| Pod logs | `KubePodLogs` (snapshot, tail, previous) | follow/stream, multi-container, search (S) |
| Exec / terminal | `kube_exec*.go` | none |
| Events | only machined events (Talos) | Kubernetes Events list per object and namespace (S) |
| Port-forward | none | SPDY/websocket port-forward to a local port for the phone's browser (M; useful for dashboards) |
| Helm releases | none | list/history/values from `sh.helm.release.v1` Secrets (gzip+base64 JSON), rollback later (M) |
| Prometheus dashboards | presets + service-proxy queries | parity, nothing to do |
| Desktop app | none | out of scope |

What we have and kubenav does not (the pitch): Argo CD and Flux with sync, freeze and diff, data
services (CNPG, Ceph, Longhorn, Velero…), Cilium/Hubble flows, API health, drain, alerts and
widgets, sealed backups across Android/iOS, and the Talos layer for Talos users.

Minimum to be credible as a kubenav replacement: K1 (import + backup) + generic browser + YAML
view + Kubernetes Events + log follow. The rest can follow.

## 7b. Lessons from kubenav's source

kubenav is MIT (Flutter UI + gomobile Go core, like us). Read on 2026-10-07 (commit `f507428`)
to learn its mechanisms, not to copy code. What it does and how we do it better:

| Topic | kubenav | Ichor |
|---|---|---|
| Cloud "providers" | 9: kubeconfig, manual, AWS keys, AWS SSO, Azure SP, GCP, OIDC, DigitalOcean, Rancher. Each one lists the account's clusters and builds the cluster entry: **good UX, worth matching** | same idea as a later phase (K7): after an AWS/GCP sign-in, list clusters (`eks:ListClusters` + `DescribeCluster`, GKE `container` API) and create the kube clusters without any kubeconfig. Kubeconfig import stays the main path |
| Kubeconfig import | every context, one bad context fails all; `exec` silently dropped (401 later); `auth-provider` parsed then unused; re-import duplicates | per-context preview with auth method and why it is unsupported; recognised `exec` plugins turned into sign-in methods; fingerprint dedupe and "replace credentials" |
| Client | a new clientset per call, kubeconfig rebuilt by string concatenation (a token with a newline injects YAML) | our cached `kubeClient` (`kube_client_cache.go`), typed parsing, no string-built YAML |
| Token refresh | lazy, before each request, **no lock**: parallel screens refresh at once and a rotating refresh token logs the user out | Go `tokenSource` with single-flight per cluster (one refresh in flight, others wait), refresh ~1 min before expiry, 401 → invalidate → retry once, new state written back sealed |
| Expiry UX | "Reauthenticate" only for AWS SSO and OIDC without refresh token; other failures are generic errors; DO/Rancher/Azure tokens just die | every source returns `errLoginRequired` → per-cluster "Sign in again" banner; expiry shown in the cluster list like Talos cert expiry, with the existing expiry alerts |
| OIDC redirect | `https://kubenav.io/auth/oidc.html` shows the code, the user copies it back into the app; `state` typed by the user and not checked; no loopback, so kubelogin clients need an IdP change | PKCE always, `state` + `nonce` checked, the browser comes back to the app by itself: (1) loopback `http://localhost:8000` for kubelogin-registered clients, (2) private-use scheme `name.levis.ichor:/oidc` (RFC 8252 §7.1), (3) device code **with polling** (kubenav makes the user tap Verify) |
| AWS | aws-sdk-go-v2 presign; SSO device flow but no SSO refresh token (re-login every ~8 h) | presign the same way (or a small SigV4); SSO with the `refresh_token` grant of `sso-oidc` so the session lasts the IdP's configured duration; SSO → assume-role chain |
| GCP | the user creates their own Web OAuth client in GCP, no PKCE, no state, code copied from a web page, client secret in URL queries | service account key first; user sign-in via OIDC (GKE Identity Service / Workforce) or a user-provided **installed-app** client with PKCE + private-use redirect |
| Secrets at rest | one JSON blob in flutter_secure_storage with default options (no iOS accessibility class); CRD cache in the keychain too | AndroidKeyStore/StrongBox and Secure Enclave, `WhenUnlockedThisDeviceOnly`, as for talosconfig; caches never in the keystore |
| Logs | debug logs record full OIDC token responses, and the log viewer has a Copy button | tokens never logged; support bundle already masks (`kube_page_privacy_test.go`) |
| Backup / sync | none | sealed cross-platform backup (§4b) |
| Streaming | logs follow, exec and port-forward go through a local Go HTTP/WebSocket server, credentials in request headers | no local server: gomobile callbacks as `kube_exec_stream.go` and `kube_hubble_stream.go` already do. Port-forward binds `127.0.0.1` only, random port, one session per forward, stops with the screen (Android foreground service if kept in background) |
| Resource browser | hardcoded kinds, one widget each; CRDs via CRD list + client-side `additionalPrinterColumns` jsonPath; no watch, refresh button | `/api` + `/apis` discovery, server-side Table (`as=Table`) for every kind incl. CRDs, so no per-kind code and the columns match `kubectl get`; watch for live lists on open screens |
| Edit | YAML → JSON Patch diff, no `resourceVersion` check (overwrites a concurrent change) | server-side apply with `dryRun=All` first, show the diff (we have `kube_diff*.go`), apply with field manager `ichor`; conflict = clear message, never a silent overwrite |
| Helm | Helm SDK (list, history, rollback, uninstall) | read-only first, decoding `sh.helm.release.v1` Secrets ourselves (base64 → gzip → JSON, no Helm SDK in the binary); rollback later and only if worth the size |
| Node shell | privileged busybox `chroot /host` pod | Talos clusters keep the Talos API (no shell needed); for other clusters, an ephemeral debug container on the node with an explicit "privileged pod" warning, off by default |
| Help content | advises a cluster-admin ServiceAccount | least-privilege SA templates (read-only, read + restart, etc.) with copyable YAML |

## 8. Demo, tests, probe

- Demo stays a Talos cluster; add a second demo context of kind `kube` (fixtures already exist
  through `kubeReadJSON`'s demo function) so screenshots and UI tests cover the gated navigation.
- Go unit tests: kubeconfig sniffing, `ParseKubeconfig` preview per auth form, exec recognition
  table (aws, aws-iam-authenticator, gke plugin, kubectl oidc-login, kubelogin, unknown), SigV4
  presign against AWS's published test vectors, OIDC refresh with an `httptest` IdP, 401 →
  invalidate → retry once.
- `go/cmd/probe`: `probe kube --kubeconfig FILE --context X` to test real EKS/GKE/OIDC clusters
  from a laptop before any UI.

## 9. Phases

| # | Phase | Content | Size |
|---|-------|---------|------|
| K1 | Kubeconfig clusters, static auth | cluster kind, sealed storage, no-talosconfig state, import (file, paste, QR + gzip form, "Open with") + preview, backup format 2, Go sniffing seam, `errTalosUnavailable`, capability gating, Kubernetes overview, 403 tolerance, SA help sheet | L |
| KN | kubenav parity | generic browser (discovery + Table), YAML view, Kubernetes Events, log follow; later YAML apply, port-forward, Helm | M → L |
| K2 | `tokenSource` + OIDC | interface, 401 retry, auth-state write-back, kubelogin + `auth-provider: oidc`, device code, loopback PKCE, sign-in UX | M |
| K3 | EKS | exec recognition, SigV4 presign, IAM Identity Center device flow, static keys fallback, AssumeRole | M |
| K4 | GKE | gke plugin recognition, service account key | S |
| K5 | Hybrid Talos + own kubeconfig | per-cluster Kubernetes access choice | S |
| K6 | AKS (§5.5) | Azure kubelogin recognition (devicecode, spn) on top of K2 | S |
| K7 | Cloud discovery | after an AWS / GCP / Azure / DigitalOcean / Rancher sign-in, list the account's clusters and add them without a kubeconfig | M |
| K8 | DigitalOcean (§5.6) | doctl exec recognition, scoped API token, credentials refresh before `expires_at` | S |
| K9 | Rancher (§5.7) | rancher exec recognition, API key, `generateKubeconfig` | S–M |

K1 alone delivers SA and client-cert clusters on both platforms. Release K1 + KN together as the
"any Kubernetes cluster" version; K2 (OIDC) next, since kubenav users with SSO need it. Each
later phase is independent of the others once K2's `tokenSource` exists.

## 10. Open questions

1. Store listing, name and screenshots for "Kubernetes, and Talos" (the Talos trademark notes in
   `TRADEMARKS.md` still apply to the Talos part).
2. Go binary size budget for `x/oauth2` and, if chosen, the AWS SDK signer packages.
3. iOS: does a loopback listener survive during `ASWebAuthenticationSession`? Spike before K2;
   device code is the fallback either way.
4. Should kube clusters be allowed to mutate (scale, delete, sync…) from day one, or start
   read-only like the first Talos releases?
