# Adding clusters

Part of the [Ichor documentation](../README.md#documentation).

## Creating a talosconfig for the phone

Generate a dedicated identity instead of copying your admin `~/.talos/config`. If the phone is
lost, its certificate only grants what you chose, and it expires on its own.

`talosctl config new` needs exactly one node to sign the certificate: use a control-plane node.

```sh
CP=10.0.0.2   # any control-plane node

# Monitoring only (recommended default):
talosctl -n $CP config new talosconfig-phone --roles os:reader --crt-ttl 8760h

# Monitoring + reboot/shutdown:
talosctl -n $CP config new talosconfig-phone --roles os:operator --crt-ttl 8760h

# Everything, including the health check and kubeconfig export (treat the phone like a laptop):
talosctl -n $CP config new talosconfig-phone --roles os:admin --crt-ttl 8760h
```

Check what was generated. The app queries the context's `nodes`, falling back to its
`endpoints` when the list is empty, so make sure every node is listed:

```sh
talosctl --talosconfig talosconfig-phone config info
talosctl --talosconfig talosconfig-phone config node 10.0.0.2 10.0.0.3 ...   # all nodes
talosctl --talosconfig talosconfig-phone -n $CP version                                # sanity check
```

The `talosconfig*` filenames are gitignored in this repository.

**Renewal:** the certificate lasts `--crt-ttl` (default one year). Settings shows the expiry
date. When background alerts are on, the app notifies daily from 14 days before expiry. To
renew, generate a new file and import it (Settings → Clusters & backup → Add a cluster): a cluster already
imported, i.e. the same context name and CA, is updated in place.

**Revoking:** a Talos client certificate cannot be revoked. To cut off every talosconfig issued
so far (a lost phone, a leaked file), rotate the cluster CA from the phone (Settings → Clusters
& backup → Rotate the cluster CA…, see [Safety](safety.md)), then issue new talosconfigs for the
devices that keep access.

## Importing it

Pick one of these:

- **File:** `adb push talosconfig-phone /sdcard/Download/`, then choose it in the app.
- **Paste:** paste the YAML.
- **QR code:** run `qrencode -r talosconfig-phone -o talosconfig.png` and scan it. This works
  for configs up to about 2.9 KB. A larger one (a kubeconfig with an embedded certificate)
  usually fits compressed; the app expands it:

  ```sh
  gzip -9 < talosconfig-phone | qrencode -8 -t ansiutf8
  ```

  Both forms work, as does the older `ichor-config:` text form (base64url of gzip).

  A config too large even compressed (several clusters, long certificates) goes over several
  codes, each `ichor:<i>/<n>:<chunk>` (up to 16 codes of 1,200 characters, version 25 at ECC
  level L). Scan them in any order: the app shows "2 of 3" and imports the config once all
  are in.

  ```sh
  just qr-parts talosconfig-phone            # one code after another in the terminal
  just qr-parts talosconfig-phone -o parts   # parts/part-1-of-3.png, ...
  ```

Delete `talosconfig.png` (or the `parts` PNGs) and the copy in `Download/` afterwards; they contain the private key.
The app stores the config AES-GCM encrypted, excluded from backups: an Android Keystore key (in
the StrongBox chip when the phone has one) wraps a random data key that encrypts the config, so
it opens in milliseconds however many clusters it holds. A busy chip is tried again for a few
seconds before the key goes to the TEE instead; Settings → Security then says so once, and
its "Move to the secure chip" button tries the chip again and, when it answers, encrypts the
stored configs again under a new chip key in place, so a refusal or a crash at any step leaves
them readable under the old one.

## Several clusters

Every context of a talosconfig is a cluster in the app, and importing another talosconfig
adds its contexts to the ones already there (like `talosctl config merge`: a context whose
name is taken by another cluster is added as `name-1`). To switch cluster:

- **Android:** swipe the overview's top bar left or right, or tap its title for the list.
- **iOS:** swipe the row of dots under the overview's title, or tap it for the list.

On Android, screens with tabs (node, Kubernetes, data services, Flux, network, KubeSpan,
insights, flows) also switch tab with a sideways swipe on their content.

The list is also where a cluster is removed (its credentials are deleted from the device) and
where its color is chosen. Each cluster gets a color of its own, and the app's palette (light,
dark and true black alike) is generated from the color of the cluster on screen, so it is
always clear which cluster a reboot is about to hit. Background alerts and the widget follow
the cluster on screen. On both homes, the summary card's name band and stat icon tiles carry the
cluster's color, while health, metrics and bars keep their neutral surface and status colors.

**Search every cluster's nodes, pods, workloads and GitOps apps from the overview** (Android:
"Search clusters" in the title's menu, or the search action of the top bar; iOS: "Search
clusters" in the top bar's ⋯ menu, which can be moved to the bar). Results come from
what each cluster last showed: other clusters than the one on screen need "Keep last known
state" on. Typing never calls a cluster; a result opens its screen on its cluster, like a share
link. The search also opens from the app icon: a long press on Android ("Search clusters"),
a Home Screen quick action on iOS. On iOS the clusters and the Argo CD and Flux apps the phone
knows also show in Spotlight and open Ichor on them; only their names are indexed, and only
while "Keep last known state" is on and neither the app lock nor the screenshot mode is.

**Pin a node, workload, pod, CronJob, Argo CD or Flux app** (the pin button next to its share
link) to a Pinned card at the top of that cluster's overview: up to 12 per cluster, each with its
ready or health state when the overview knows it, opening like its share link; a long press on a
row (Android) or a swipe (iOS) unpins it, and the card moves or hides like the others.

**Runbooks** (the cluster menu's "Runbooks…"): your own ordered lists of the app's actions
(scale, restart, suspend or run a CronJob, Argo CD sync, Flux reconcile, etcd snapshot, node
reboot or maintenance), written once in YAML and imported by paste, file or https link. The
plan shows each step's target, whether it is destructive, the role it needs and whether the
cluster's permissions allow it, before the runbook is kept; nothing runs on import. A run asks
before every destructive step (the node's hostname for a node step), shows its progress and
Stop in the notification (Android) or a Live Activity (iOS), stops at the first failed step
unless you let it go past, and records every step in the Activity log. Runbooks are kept per
cluster, encrypted on the phone. Format and actions: [docs/runbooks.md](runbooks.md).

**Open a screen straight from the home screen or a shortcut:**

- **Android:** "Add to home screen" (next to a screen's share link, and on the checkup and
  alerts screens) pins an icon in the cluster's color that opens that screen of that cluster
  after unlock; it greys out when the cluster is removed.
- **iOS:** "Open in Ichor" in the Shortcuts app takes a share link and opens its screen once
  unlocked, so a shortcut (or Siri, with the phrase you give it) opens a node, an app or etcd.
  "Cluster status" answers "What is the state of <cluster> in Ichor": Siri says what the last
  background check saw (nodes ready, etcd, how long ago, and when it may be out of date) from
  the widget's data, with no network call and without unlocking the app. "Open cluster" and
  "Run a checkup" open that cluster's overview or checkup after unlock, from Shortcuts, Siri or,
  on iOS 18, two Control Center buttons. None of them changes anything on the cluster.

## Kubernetes clusters without Talos

A cluster can also be added from a kubeconfig alone, the same ways as a talosconfig (file,
paste, QR, or "Open with" / the share sheet); the app tells the two apart. The preview lists
every context with how it signs in, and why one cannot be added (a file path to inline with
`kubectl config view --flatten --minify`, a proxy, plain HTTP…). These clusters get a
Kubernetes home (nodes, API server version, who you are signed in as) and every Kubernetes
screen, grouped on that home as Workloads, Networking, Storage, Observe and Browse; the Talos
ones are hidden. What they may do is up to their RBAC.

Each node on that home also says where the cloud put it, read from its labels: the
autoscaler pool it came from (a Karpenter NodePool — open source or EKS Auto Mode — an EKS
managed node group, a GKE custom compute class or node pool, an AKS agent pool), its
machine type (`node.kubernetes.io/instance-type`) and whether it is spot, on-demand or
reserved capacity (`karpenter.sh/capacity-type`, `eks.amazonaws.com/capacityType`,
`cloud.google.com/gke-spot`, `kubernetes.azure.com/scalesetpriority`). Nothing is shown
when no such label is set, as on bare metal.

| Kubeconfig user | What the app does |
|---|---|
| Client certificate, token (ServiceAccount) | Uses it as is |
| `kubectl oidc-login` / kubelogin, `auth-provider: oidc` | Signs in in the browser (kubelogin's own `localhost` redirect, so nothing changes on the identity provider) or with a device code; renews the token with the refresh token |
| `aws eks get-token`, aws-iam-authenticator | IAM Identity Center (device code, like `aws sso login`; the account and role are picked from a list after the sign-in) or access keys; `--role-arn` through AssumeRole. No kubeconfig yet: **Add from a cloud account** → EKS → IAM Identity Center |
| `gke-gcloud-auth-plugin` | A service account key, your Google account in the browser like gcloud (every build), Sign in with Google (Play build and iOS), the user credentials `gcloud auth application-default login` writes (`application_default_credentials.json`, also with `--login-config` for workforce identity), your organisation's own OAuth client (Desktop app type: sign-in in the browser, no key), or a workforce identity login config (browser sign-in at your organisation's identity provider) |
| Azure `kubelogin` (AKS with Entra ID) | Device code or browser, or a service principal |
| `doctl … exec-credential` | A DigitalOcean API token (short-lived cluster credentials from it) |
| `rancher token` | A Rancher API key |

**GKE credentials by QR:** scan a gcloud ADC file from the import screen or tap
**Scan credentials QR code** in GKE discovery. It opens discovery with the credential
filled in; choose projects if needed, then tap **Find clusters**. Google user,
workforce identity session and service account files are supported. Keep the QR
private: it contains a refresh token or private key.

```sh
qrencode -t ansiutf8 < ~/.config/gcloud/application_default_credentials.json
```

**EKS Identity Center by QR:** paste or scan the `~/.aws/config` that `aws configure sso`
wrote, from the import screen or with **Scan ~/.aws/config QR code** in EKS discovery. It
opens discovery on IAM Identity Center with the start URL and its region filled in; tap
**Sign in and find clusters**, and the profile's account and role are already ticked. The first profile that uses Identity Center is taken (an `sso-session` alone
works too, then you pick the account and role). The file holds no secret. An
`~/.aws/credentials` file is refused: type access keys on the EKS discovery screen.

```sh
qrencode -t ansiutf8 < ~/.aws/config
```

**GKE with your Google account, no setup**: pick "Your Google account (like gcloud)" on the
GKE sign-in sheet and tap **Sign in**. Google's sign-in opens in the browser (your
organisation's SSO applies) and comes back by itself; the app then keeps the session as
`gcloud auth application-default login` does, renewing it without the browser until Google ends
it. It uses gcloud's own public OAuth client, so the consent page names Google Cloud SDK, and
Workspace admins who allow gcloud allow it. The refresh token is stored like an imported
`application_default_credentials.json`, so encrypted backups keep the session.

**GKE with workforce identity, no laptop**: pick "Workforce identity (login config)" on the
GKE sign-in sheet, import the login config your administrator made with
`gcloud iam workforce-pools create-login-config`, and tap **Sign in**. Google's workforce page
opens in the browser and sends you on to your organisation's identity provider, as
`gcloud auth login --login-config` does; the session is then stored and renewed like an
imported workforce `application_default_credentials.json`, and the config stays filled in for
the next sign-in.

**GKE with your organisation's OAuth client**: in the Google Cloud console (APIs & Services),
1. set the OAuth consent screen to **Internal** (no Google verification needed),
2. create an OAuth client ID of type **Desktop app**,
3. in Ichor, pick "Your organisation's OAuth client" on the GKE sign-in sheet and paste its ID
   and secret. Sign in then opens your Google account in the browser (company SSO included);
   the app keeps a refresh token and asks again only when Google ends the session.

A **Web application** client works too (the kind kubenav uses): add
`https://cyrinux.github.io/ichor/auth/google/` (or a page of your own doing the same) to its
authorised redirect URIs and enter it as the redirect URL. Google sends the browser to that
page, which hands the sign-in back to the app; if the browser cannot open the app, the page
shows a sign-in code to paste. The code is useless without the key only the app holds (PKCE).

**Add from a cloud account** (on the add screen) lists the clusters of an AWS, Google Cloud,
Azure, DigitalOcean or Rancher account and adds the ones you pick, signed in with the same
credentials. For Google Cloud that is a service account key, the gcloud user credentials of
your own account, your Google account in the browser like gcloud (every build), **Sign in with Google** (Play build, and iOS builds that carry Ichor's Google
client), or your organisation's OAuth client in the browser, with no file to paste: the app then
looks through every project you can see (or the project IDs you enter), and the clusters you add
reuse that sign-in without a second prompt.

**EKS with IAM Identity Center, no kubeconfig**: on **Add from a cloud account**, pick EKS, then
"IAM Identity Center", and enter your access portal's start URL and its region. **Sign in and
find clusters** opens the portal in the browser with the device code filled in, as
`aws configure sso` does; the app then lists the accounts and roles you may assume (one is
taken as is) and you tick those to look in. It reads their clusters in the regions you enter,
or in the Identity Center region and the common regions when you leave them empty, a few at a
time; a region the role may not list (`eks:ListClusters`, `eks:DescribeCluster`) is skipped and
named. The clusters you add sign in with the role they were found with and the same session:
no second browser round.
A Talos cluster can also use one of these kubeconfig clusters for its Kubernetes
screens (cluster menu → **Kubernetes access**): your own identity and RBAC instead of the
admin kubeconfig, which also works with an `os:reader` talosconfig.

Sign-in tokens and entered keys are sealed on the device like the configs. Backups keep the
keys you entered but not the tokens of a sign-in: after a restore, those clusters ask to sign
in again.

## Omni clusters

A cluster managed by [Sidero Omni](https://omni.siderolabs.com/) is reached through Omni, which
proxies the Talos API to its machines and signs every call with a PGP key, as `talosctl` and
`omnictl` do. Add one in any of three ways:

- **Add from Sidero Omni** (on the add screen): the instance URL, then an account email or a
  service account key (`OMNI_SERVICE_ACCOUNT_KEY`, as `omnictl serviceaccount create` prints it).
  Omni lists the clusters the identity may see and the preview adds the ones you pick.
- **Import Omni's talosconfig** of a cluster (downloaded from Omni, or `omnictl talosconfig`),
  like any other talosconfig. It has no certificate: the app asks for the sign-in on first use.
- **Enter details** (the form): the instance URL, the cluster name in Omni and who signs in.

An account signs in by confirming a key in the browser, as `omnictl` does: the key lasts 4 hours,
then the app asks again. A service account key is long-lived and is entered once. One sign-in
serves every cluster of the same identity on the same instance; removing one cluster keeps the
others signed in. Backups keep service account keys but not account sign-ins.

What the app may do is Omni's role for the identity: the Reader role reads the Talos API through
Omni and lists the clusters; Kubernetes goes through Omni's own proxy, signed in with the same
key, and needs an access policy on the cluster (or the Operator role), as `omnictl kubeconfig`
does. Issuing a talosconfig for a teammate is not available for Omni clusters: download one from
Omni instead.

## Share links

**Share link** sends a link to the screen on show, for a teammate whose Ichor holds a
talosconfig for the same cluster: an Argo CD or Flux app, a node (on its tab), a workload, pod
or CronJob, etcd, the health check, the Argo CD and Flux lists, the Kubernetes tabs or the
cluster overview. It is in the top bar, the node and cluster menus, the workload sheet, the
pod's log sheet (link icon), a CronJob's card (Android) or a pod or CronJob's context menu (iOS).

- The link is `https://cyrinux.github.io/ichor/open/#…`. That page hands it to the app as
  `ichor://open?…` (Android also offers to open the https link in Ichor directly). What follows
  `#` stays in the browser: the website never sees it.
- The cluster is named by a hash of its CA certificate, the same on every phone whatever the
  context is called there. The link carries the names the screen shows (namespace, name,
  hostname and address), nothing secret. In screenshot mode it carries the masked names.
- Opening it unlocks the app first, switches to that cluster (keeping the context on screen
  when it is one of it, so its role) and opens the screen. A link only ever navigates: no
  action, not even one waiting for confirmation. A node opens only when it is one of that
  cluster's (else the link stops at the overview). Without that cluster, the app says so.

## Backup and restore

The stored config's key never leaves the phone, so moving to a new phone takes a backup:
**Settings → Clusters & backup → Back up clusters and settings** (with the app lock on, you authenticate
first). Restore it from the same section, or from **Restore a backup** on the first screen of
a fresh install. Backups move between Android and iOS.

- **What it holds:** the talosconfig (every cluster's credentials), the cluster on screen,
  theme, language, screenshot mode, monitoring and its storage thresholds, "Allow screenshots",
  each cluster's name, color, VPN-only setting, Wake-on-LAN targets, pins and repository, your Git hosts,
  the pull requests opened from the phone and the plugins you imported, on or off (a backup that
  holds either needs this version or later to restore; a plugin that no longer validates is
  listed as left out), the AI diagnosis settings, and whether an app lock was on. API keys and Git tokens only
  when you turn on **Include API keys and Git tokens** while backing up (off by default:
  anyone with the file and its passphrase can use them).
- **What it leaves out:** the app lock's secret and security-key data (set the lock again; the
  app offers it after the restore), the audit log, and caches.
- **Encryption:** a passphrase of at least 12 characters. The key comes from Argon2id (64 MiB,
  3 passes) and the file is sealed with AES-256-GCM. Without the passphrase it cannot be opened,
  and the passphrase cannot be recovered.

  Choose **age file** instead of the Ichor format (the default) to get a standard
  [age](https://age-encryption.org) file (`.age`) that opens on a computer without the app,
  either with a passphrase (at least 12 characters) or for one or more age public keys
  (`age1…`, one per line), not both:

  ```sh
  age -d backup.age | jq .                            # passphrase file: age asks for it
  age -d -i ~/.config/age/key.txt backup.age | jq .   # file made for your age public key
  ```

  The phone restores an age file made with a passphrase. A file made only for age public keys
  cannot be restored on the phone: open it on a computer with the matching age key, or back up
  again with a passphrase.
- **Restoring replaces** every cluster and setting on the phone with those of the backup.
