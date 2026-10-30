# Features

Part of the [Ichor documentation](../README.md#documentation).

- **Viewing:** node status, services, resources, logs, etcd and the cluster health check.
- **Actions:** reboot or shut down a node, and wake a powered-off one with Wake-on-LAN (set a
  MAC address and, optionally, a broadcast address and port per node, from its action sheet;
  the phone sends the magic packet itself, so it has to reach the node's network). The MACs of
  each node's network cards are recorded on the phone while it is up, so a node that goes down
  before it was set up can still be woken.
- **Cluster CA:** rotate the Talos API CA (Settings → Clusters & backup → Rotate the cluster
  CA…), like `talosctl rotate-ca --kubernetes=false`: a plan of every node first, then five steps
  in which every node trusts both CAs until all of them trust the new one, and the phone's own
  talosconfig is replaced on the way. Every talosconfig issued before stops working at the end,
  so it is the way to cut off a lost phone or a leaked config. See [Safety](safety.md).
- **Kubernetes:** list Deployments, StatefulSets and DaemonSets with their rollout state and
  restart one with a rolling update (`kubectl rollout restart`), then follow it live with its
  old and new pods (`kubectl rollout status`); list pods with their
  `kubectl get pods` status (a pod that cannot pull its image says why: not found, pull limit,
  pull secret, certificate, network, and what to do) and delete one so its controller starts a new one, or only those of
  a node or of a workload (`--field-selector spec.nodeName=`, the workload's selector), or clean up a
  namespace's dead pods in one tap from the Pods tab (Completed, Evicted, CrashLoopBackOff, ImagePullBackOff…, as k9s's `z`
  picks them, with a preview to uncheck any first; the sweep runs in the background with a progress
  notification or Live Activity, Stop, and a result when it ends); list CronJobs
  with their schedule, next run and recent runs, and run one now (`kubectl create job --from`,
  with an icon and a title of your choice, see [CronJobs](kubernetes.md#cronjobs)); measure the
  network between two nodes (TCP throughput and latency, see [Network test](network.md#network-test));
  open an app in the browser from its sheet, at the hosts of the Ingresses, Gateway API
  HTTPRoutes, GRPCRoutes and TLSRoutes and Traefik IngressRoutes whose Services select its pods (kgateway included: its
  routing objects are the standard ones, and the network map's Gateway boxes name the
  controller serving them — kgateway, Traefik, Cilium, Envoy Gateway, Istio, NGINX Gateway
  Fabric); browse any resource, CRDs included, with the
  columns `kubectl get` prints (on iPhone and iPad, long-press an object to copy its name, share
  its link or delete it), read and edit its YAML (a dry-run diff first; a change made
  meanwhile is never overwritten), apply pasted manifests like `kubectl apply --server-side`
  (each object previewed as a diff first; a field another manager owns is refused, never taken
  over), list Helm releases and roll one back to an earlier revision
  (a dry-run plan first; a release Flux manages is suspended first; optionally waiting for its
  workloads to be ready, replacing objects instead of patching them, and keeping only the last N
  revisions; the chart's pre- and post-rollback hooks run in Helm's order with their delete
  policies), follow a pod's log and
  port-forward to it on the phone (127.0.0.1 only); see what gives LoadBalancer Services their
  address (MetalLB and Cilium pools with their free addresses, MetalLB advertisements and BGP
  peers, Cilium L2 announcement policies and the node announcing each Service, kube-vip ranges
  and leases) and why one has none; or export a kubeconfig. Reach the API through another address (a port forward, a load
  balancer, a public name) from the cluster's row in Manage clusters, on this device only (not through Omni, nor for a Talos cluster whose Kubernetes access goes through a kubeconfig cluster: that one's address is used). On a Talos
  cluster the Kubernetes screen's ⋯ menu reads in three sections (Explore, Health, Installed here) and also leads to
  Capacity, Data services and, when the cluster runs them, the cert-manager and CAST AI lists (any
  of them can be moved to the bar).
- **Argo CD:** when the cluster runs it, list its Applications with their health and sync state,
  follow a sync wave by wave, and sync (with prune, dry run, selected resources…), refresh,
  terminate a sync, pause or resume auto-sync, or roll back to an earlier deployment. A sync,
  a Flux reconcile or a rollout restart started from the phone is followed in the background,
  with its progress and Terminate or Stop in the notification (Android) or Live Activity (iOS). Ichor
  patches the Application resources with the admin kubeconfig, like `argocd --core`, so no
  Argo CD token is needed. **Show diff** tells what a sync would change before running it,
  like the Diff tab of the Argo CD UI: Ichor reads the comparison the application controller
  already made (the live and the predicted state of each object, in Argo CD's Redis, through a
  port-forward: nothing runs in the cluster, nothing is written) and renders it object by
  object, Secret values hidden, also in the sync sheet for the resources about to be synced;
  the revision, each history entry and the running sync link to
  their commit on GitHub, GitLab, Gitea, Forgejo, Codeberg or Bitbucket. An app an ApplicationSet or a parent app manages keeps its spec:
  only sync, refresh and terminate are offered there, since its owner would revert the rest.
  Each app also has a network view, like Argo CD's own but down to the nodes: host → Gateway →
  Ingress/HTTPRoute/GRPCRoute/TLSRoute/IngressRoute → Service → pods → node, every box coloured by health, traffic flowing along
  the healthy paths, and the likely root cause named (a node not ready, a crash-looping pod, a
  Service without ready pods).
  An app the icon catalog does not know (a personal project) can bring its own logo, see
  [Argo CD app icons](kubernetes.md#argo-cd-app-icons).
- **Flux:** when the cluster runs Flux v2, list its Kustomizations and HelmReleases with their
  readiness, applied and attempted revision, source, what a Kustomization applied and a
  release's Helm history, and their Git, OCI and Helm sources; reconcile (with its source
  first if wanted), suspend or resume any of them, and force or reset a HelmRelease that gave up.
  The detail shows a HelmRelease's drift as Flux reports it (with **Correct drift**), the
  dependsOn chain as a tree with what waits on it, and the object's events. Like Argo CD,
  Ichor writes the annotations and `spec.suspend` the `flux` CLI does, with the admin
  kubeconfig: nothing to install in the cluster.
- **Argo Rollouts:** see each canary or blue-green rollout at its step, with its weight, pause
  reason and analysis, and promote (one step or fully), abort, retry or pause it with the same
  patches as the `kubectl argo rollouts` plugin.
- **Git hosts:** Settings → Git hosts keeps one personal access token per Git host (GitHub,
  GitLab, Gitea or Forgejo), to read and later change the repositories Argo CD and Flux deploy
  from. Grant only what is needed (GitHub fine-grained: Contents and Pull requests, read and
  write, on those repositories); **Check** says who the token signs in as. The token stays on
  the phone, encrypted (Android Keystore, iOS Keychain), is never shown again, and is only sent
  to its own host over https. When Argo CD or Flux reach the same server by another name (an SSH
  or in-cluster address such as `gitea-http.gitea.svc:3000`), list it under **Also reached as**:
  those repositories then use the host's token, requests still go only to the saved host, and
  their repository, commit and file links open on the saved host instead of the internal name.
- **Source:** an Argo CD or Flux app's detail lists its Git sources; each opens the folder the
  app deploys from, at the revision the cluster runs, with read-only files (YAML highlighted on
  Android). Flux apps need no token (the files come from the source-controller's artifact);
  Argo CD apps use the Git host token, and without one the screen says which host to add.
  **Open source file** on a changed resource of the Argo CD or Flux diff finds the manifest
  that declares it (kustomize `namePrefix`/`nameSuffix` taken into account) and opens it at its
  `metadata.name` line; a Helm-rendered app says why there is no file to open.
- **Edit and open a pull request:** Edit on a source file opens the YAML editor, Review shows
  the diff, and **Open pull request** commits it on a new branch and opens a pull or merge
  request (GitHub, GitLab, Gitea). The tracked branch is never pushed to: Argo CD or Flux
  applies the change once someone merges it. A file changed upstream in the meantime is
  refused, with Reload to start from the new version. Review also checks the build on the
  phone, `kustomize build` before the push: the edited file is laid over the repository at
  that commit (downloaded once, as for Find in repository), the nearest kustomization (a Flux
  app's path, with its settings) is built before and after, and the review lists the rendered
  objects that change, each with its diff, Secret values masked, or **Build fails** with the
  error and the file and line it names. A file in no kustomization is checked as YAML; a Helm
  chart is not rendered on the phone, and a remote base is not fetched, which the check says.
  The pull request can be opened either way. The phone remembers each request it
  opened (50 per cluster, kept in the backup): **Proposals** on the Argo CD or Flux app (and
  at the cluster repository's root) shows whether each is open, merged or closed on the forge
  and, once merged, whether the app runs that commit; a merged one not running yet offers
  **Reconcile now**, which syncs or reconciles with the usual follow. With alerts on, the
  background check tells you when one of them is merged or closed ("Your pull request #12 was
  merged"), and the notification opens the app it changed.
- **Stage several files and propose them together:** in the cluster repository, Review also
  offers **Stage**: the edit waits, the file gets a dot in the browser, and **Staged changes**
  lists the files (lines added and removed), opens one again or removes it, then opens one pull
  request for all of them (up to 20). GitLab and Gitea take it as one commit, GitHub as one
  commit per file on the same branch; if one fails, the branch is deleted. Staged files are kept
  until the app is closed, not saved.
- **Quick edits:** on a source file, **Quick edit** changes a container's image tag (with the
  running tag as a suggestion) or the replicas, through a kustomization `images:` or `replicas:`
  entry when the file has one. Only that line changes, comments and quoting included, and the
  edit goes straight to the review and pull request. For routine bumps, Flux image automation or
  Argo CD Image Updater can open these changes for you.
- **Keep live changes in Git:** when an object was changed by hand in the cluster (`kubectl
  scale`, `set image`, `edit`), its card on the Argo CD or Flux diff lists the values that
  differ and offers **Keep live values in Git**: the manifest is found as for Open source file,
  the live values are written only where that file sets the value Git would apply (one set by
  an overlay, a patch or a substitution is named and left), then reviewed and proposed as a
  pull request. Never for a Secret. An object's page links to the diff of the Argo CD app or
  Flux Kustomization applying it (**Compare with Git**), as do an Argo CD app's sync and
  comparison errors and each resource its last sync failed on.
- **Cluster repository:** the repository that holds a cluster's own configuration (its Talos
  patches, its Kubernetes tree, its `.sops.yaml`) is a setting of that cluster: **Cluster
  repository** in its menu of the cluster list stores the address, a branch (blank for the
  default one) and the Talos and Kubernetes directories, and **Check** asks the forge for the
  default branch and whether the token can push. Only those four fields are stored (and backed
  up); the token is the Git host's, from Settings → Git hosts, and is only used to read the
  repository and open pull requests on it. **Repository** on the cluster home then lists the
  repository from its root at the branch head (`main @ 1a2b3c4`); any text file opens
  read-only, and Edit, Review and **Open pull request** work as for an app's source: edits are
  pull or merge requests on a new branch, never commits to the branch itself. Nothing is
  cloned: each directory and file is one call to the forge's API. The demo cluster has a sample
  repository built in.
- **Updates available:** **Updates…** in the cluster menu lists the images the cluster's
  workloads run and the charts its Flux, Argo CD and Helm releases install, each with its newer
  **patch**, **minor** and **major** version from its registry, Docker Hub, the Helm index or the
  OCI tags. Private registries are asked with the pull secrets the workloads already use; the
  first visit on a cluster asks before any registry is. The policy: a candidate keeps the tag's
  shape (prefix such as `v`, suffix such as `-alpine`, the number of version parts), and a
  prerelease is offered only when the running tag is one. An image or chart that Flux image
  automation, Argo CD Image Updater, a semver range or a Renovate or Dependabot configuration of
  the cluster repository already bumps shows that badge and no version. `latest`, digest-pinned,
  rate-limited or unreachable ones are listed under **Not comparable** with the reason. Tapping
  a version opens the file that sets it (the workload's manifest, its kustomization `images:`
  entry, or the HelmRelease or Application) with the quick edit prefilled, then the usual review
  and pull request, titled "Update podinfo to 6.7.1". An image a chart renders opens the chart's
  first values file. The app never runs a bot, never merges, and checks nothing in the
  background: the last answer is kept until the app is closed, and the GitOps card shows its
  count. **Select** ticks several rows (each with its smallest step, a chip picks another) and
  **Propose N updates** stages them as one pull request: each file is read once, two bumps of
  one file make one file with two changed lines, and the description is a table of each bump
  (name, from, to, patch/minor/major, its registry or chart repository). A row rendered by a
  chart, missing from Git or in another repository is left out with the reason, and the pull
  request opens for the rest.
- **Dependency updates:** the Cluster repository sheet of the active cluster tells whether the
  repository already has a dependency-update bot (a Renovate configuration, a workflow running
  it, or Dependabot's). When it has none, **Propose a configuration…** shows a `renovate.json`
  drafted for the files it holds (the Flux, Argo CD and Kubernetes manifests under its
  Kubernetes directory, the Talos and Kubernetes versions of `talconfig.yaml`, Talos installer
  images) with why each part is there, then opens it as a pull request; installing the bot on
  the forge stays yours, and the pull request says how. No schedule, no automerge.
- **Try a Talos patch from Git:** on a Talos cluster, a Talos patch file opened in the cluster
  repository or an app's source (machine/cluster sections, or config documents such as
  `HostnameConfig`) shows **Try on a node…**: pick the node (the one the patch sets the hostname
  of comes first) and its machine config opens with the patch applied, on the usual review,
  try (it reverts by itself unless kept), apply, staged and reboot. A patch that only sets
  values can be taken to other nodes from the review; one that sets a hostname, with lists (Talos replaces some
  wholesale and appends to others, which the diff shows), `$patch` directives, JSON patch
  operations or a new document is tried node by node. A whole machine config, a patch setting a
  secret or holding a talenv `${...}` cannot be tried and says why. The file in Git is not
  changed: merge it on the forge once the try was kept.
- **SOPS files:** encrypted files are shown, never edited or decrypted on the phone. A
  `*.sops.yaml` or `*.enc.yaml` file has a lock in the listing; its viewer reads the SOPS
  metadata only: the keys it is encrypted for (age, PGP, cloud KMS), when it was last
  encrypted, how many values are, and any value its own rules say should be encrypted but is in
  clear (a half-encrypted file). The ciphertext shows as `ENC[…]`, and Edit and Quick edit give
  way to editing it with sops on a computer. When an edit would put a Kubernetes Secret or a
  Talos secret in clear text at a path the repository's `.sops.yaml` (the one at its root, read
  at the branch the request goes to) encrypts, the review says which rule and keys, and asks
  once more before the pull request.
- **Find in repository:** the search in a source browser (an Argo CD or Flux app's source, or
  the cluster repository) lists every line of the whole repository that holds the text, by
  file, and opens the file at that line. It downloads the repository once per commit as one
  archive (up to 48 MiB; a bigger one is refused, browse it file by file) and keeps it on the
  phone, encrypted, for the next searches (the last 5 commits, 200 MiB at most). A Flux source
  is searched in its artifact, with no token. Files over 1 MiB and binaries are skipped, a SOPS
  file is searched by its keys only, and Git LFS files and submodules are not in an archive.
- **Talos nodes: Git compared with the node:** **Compare with Git** on a node's machine config
  lists the files of the cluster repository (under its Talos directory) that configure that
  node, with the rule that matched each: its own patch (the file that sets its hostname, or is
  named after it), the shared patches, talhelper's `talconfig.yaml` and the patches it lists in
  its order (global, then the role's, then the node's, inline ones included), and the secrets
  files, greyed (never read into a comparison). **Compare** applies the files checked onto the
  node's live config with Talos's own patcher, as `talosctl gen config --config-patch` or
  `talhelper genconfig` would: **Same as Git**, or the diff (secrets redacted) with whether
  applying it needs a reboot, the node checking it with a dry run (os:admin). **Edit from this
  diff** opens the editor with Git's version as the draft, to review, try or apply it. A patch
  holding a `${...}` substitution or setting a secret is skipped with the reason; talconfig's
  node fields (installDisk, ipAddress…) are not compared, only its patches.
- **A kept change is proposed back to the node's file:** after a tried change is kept, or a
  change applied, **Propose in Git** lists the values it changed and writes them into the node's
  file of the cluster repository as the smallest patch: a changed value in place (its comments
  and quoting kept), a new one under its parent, a changed list as a whole. A value one of the
  shared patches sets is left out with that file named; a node without a file of its own gets
  `<Talos directory>/nodes/<hostname>.yaml`. The usual review and **Open pull request** follow.
  From the Git comparison, **Propose the node's values in Git** does the same for what the node
  runs and Git does not have. Secrets are never written; a talhelper node without a patch file
  of its own, or a file with `${...}` substitutions, is edited by hand.
- **Activity log:** every change Ichor made to your clusters (reboots, scaling, syncs,
  rollbacks…) is kept 90 days on the phone, encrypted, with its outcome: ok, partial (a pod sweep where
  some pods could not be deleted) or failed. Optionally, per cluster (Clusters → the cluster's
  menu → Record actions in the cluster, off by default), each Kubernetes object the app changes there also gets a
  Kubernetes Event, reason `IchorAction`, so the cluster's own history shows what the phone
  did (`kubectl get events`, the Argo CD timeline); node reboots and Talos changes have no
  object to attach it to and send nothing.
- **Export diagnostics:** a redacted log of the app's own events and the system's crash records,
  as a zip you review and share yourself; nothing is sent automatically (Settings → Support).
  On Android 11 and later it holds the system's records of how the app last stopped (an ANR,
  a crash, a low-memory kill) with their trace; on iOS, the crash, hang and exception reports
  MetricKit hands over at a later launch, with their top frames. The log keeps the current
  launch only, unless "Keep a diagnostics log on this phone" (iOS: "on this device") is on.
- **Incident summary:** overview menu → Share incident summary… turns the last 6 h, 24 h or 7 days
  of the cluster's history on this phone and the actions taken from it into a Markdown page
  (what is wrong now, timeline, actions, polling gaps), names hidden by default, to share or copy.
- **Network policies:** every NetworkPolicy and, with Cilium, CiliumNetworkPolicy and
  CiliumClusterwideNetworkPolicy, with Calico its NetworkPolicy and GlobalNetworkPolicy
  (selectors in Calico's own language, Allow/Deny/Pass/Log rules, read from its CRDs or its
  API server), in plain words: the pods each one selects, whether they are isolated in each
  direction, and what each rule lets in or out (pods, namespaces, CIDRs, entities, FQDNs,
  ports, HTTP/DNS rules), with each namespace's share of isolated pods.
- **Policies (Kyverno):** every Kyverno policy with its mode (Enforce or Audit), whether it is
  ready, how many resources pass or fail it and which ones, and whether the engine's
  controllers and admission webhook are up. See [Policies](kubernetes.md#policies).
- **Virtual machines (KubeVirt / Harvester):** every VM with its status, node, address, guest
  OS and any disk import or live migration in flight, failing ones first; start, stop (with or
  without the grace period), restart, pause and migrate one from its sheet, or open its
  serial console in the app's terminal. See [Virtual machines](kubernetes.md#virtual-machines).
- **API server health and pressure:** the Kubernetes API server's readyz and livez checks, and
  what loads it right now: Ichor reads its `/metrics` twice a few seconds apart and shows the
  request rate, 5xx and 429 answers, which clients send the requests (API Priority and Fairness
  flow schemas: nodes, controllers, service accounts…) and how full each priority level is, the
  busiest verbs and resources with their mean latency, the requests waiting in a queue with the
  user that sent them, open watches and the resources with the most objects in etcd. A verdict
  sums it up: healthy, busy, throttling or unhealthy.
  **Who loads it** goes further with the Kubernetes audit log Talos keeps on every control plane
  (read through the Talos API, os:admin): over the last 5 to 60 minutes it names the client at
  fault, service account and program, and says what it does wrong and what to change: lists in a
  loop instead of watching, watches that keep restarting, refused (403) or failing calls, an object
  rewritten every few seconds, creates of objects that exist, slow requests, throttling. When many
  clients share a symptom (5xx, slowness, watches cut at the same interval) it blames the API
  server, etcd or what sits between them instead, and it tells when a control plane's API server
  stopped logging.
- **Cluster checkup:** what no other screen shows, on one screen (Kubernetes → Checkup): pods
  that crash, cannot pull their image, stay Pending or were killed for their memory, failed Jobs,
  the Warning events of the last hour, volumes nearly full (the kubelets' own figures, no
  Prometheus needed), claims not bound or lost, deprecated API versions something still calls
  before the next Kubernetes upgrade, admission webhooks whose Service has no ready endpoint,
  admission policies: an engine that is down, a policy that is not ready, a workload whose pods a policy denies,
  what pods request against what each node offers (and whether one node's pods would still fit
  elsewhere), quotas at their limit, nodes left cordoned or under pressure, LoadBalancer Services
  without an address (and why), empty MetalLB or Cilium pools, MetalLB pools nothing advertises,
  Cilium pools no L2 announcement policy announces (when BGP is off),
  a refused MetalLB configuration and BGP sessions down, namespaces, pods and claims stuck in Terminating,
  certificate requests nobody approved, web address certificates (not cert-manager's) expired,
  expiring or naming other hosts, External Secrets that do not sync, Flux Kustomizations whose
  SOPS decryption fails, Helm releases
  failed or stuck, and Crossplane: managed resources that cannot sync, unhealthy providers, and
  the findings each plugin the cluster runs declares, under the plugin's name. Every finding says what to change. Read-only; the critical ones can also
  notify in the background (Settings → Alerts). A pod's log sheet and a workload's sheet show
  the object's Kubernetes events, like the end of `kubectl describe`. See
  [Cluster checkup](kubernetes.md#cluster-checkup).
- **Alertmanager:** the cluster's alerts grouped by name, worst first, and its silences. Swipe an
  alert to silence it (the form opens with matchers that mute only that alert; nothing is sent
  before **Silence**), swipe a silence to expire it (iPhone and iPad).
- **Node taints and labels:** a node's menu → **Taints and labels…** lists them, with the ones
  Kubernetes, Talos or the cloud set locked unless shown. Review shows the change, the API
  server's dry-run verdict and the pods a new `NoExecute` taint would evict; a taint change asks
  for the node's hostname. A node changed meanwhile is refused with Reload; the change is in the
  activity log.
- **Live flows (Cilium + Hubble, or Calico 3.30+ + Whisker):** follow the cluster's traffic like
  Hubble UI, or only what is dropped, for a namespace or a pod. With Cilium, Ichor runs
  `hubble observe --follow` in each cilium-agent pod (the CLI ships in the agent image, so nothing
  is installed and Hubble Relay is not needed); with Calico it streams Goldmane's flows through the
  whisker Service (via the API server proxy, nothing to expose), 15-second aggregates with the
  policy trace that decided them. Either way it groups repeated drops, explains the drop reason and
  names the policy behind it: the deny rule that matched, or, for the usual default-deny, the
  policies that isolate the pod. History is what the agents still hold in their flow buffer, or
  what Goldmane still keeps.
- **Node shell without Talos:** on a cluster added from a kubeconfig, a node's menu can open a
  root shell on the node, like `kubectl debug node/`: a privileged pod in the node's namespaces,
  then `nsenter`. Off by default (Settings → Kubernetes → Node debug shell), behind the app lock,
  recorded in the activity log, and the pod is deleted when the shell ends.
- **Tablets and foldables (Android):** on a tablet or an unfolded foldable, the overview shows
  its cards in two columns under the cluster summary, and the nodes list and the Argo CD and
  Flux apps open their node or app side by side with the list. Kubernetes nodes open beside
  their list too, with cordon, drain, taints and labels and the debug shell at the top of the
  node. A rail on the left lists your clusters and the cluster's sections (overview, nodes,
  alerts, Kubernetes, Argo CD, Flux, data services, activity, search, settings) beside the
  screen, as the iPad sidebar does; a narrower window keeps the phone navigation. On a phone the app stays
  in portrait, except logs, shells, charts, YAML, diffs, source files and maps, which rotate to
  landscape and use the width.
- **iPad:** the overview shows its cards in two columns on a full or wide window, and the nodes
  list and the Argo CD / Flux apps open their node or app side by side with the list (Kubernetes
  nodes too, with the node menu in the detail's toolbar); a narrow
  Split View or Stage Manager window keeps the iPhone layout. At full or wide width a sidebar
  lists your clusters and the current cluster's sections (Alerts, Kubernetes, Argo CD, Flux,
  data services, activity, search, settings) beside the screen open in the detail column.
  On an iPhone the app stays in portrait, except logs, shells, charts, YAML, diffs and the
  KubeSpan map, which rotate to landscape.
- **Background:** alerts, a home-screen widget and a fleet screen with every cluster's last check, worst first. Alerts are off until you turn them on: after the first real cluster is added, the home offers them once and asks for the notification permission there. The findings of the plugins the cluster runs
  (a kube-green schedule with no wake-up time, an ExternalSecret that does not sync…) notify
  like the data services' problems, in the app's language, and clear when they go away
  ([docs/plugins.md](plugins.md#findings)). Alert rules (Settings → Monitoring → Rules) set
  quiet hours (a window in the phone's zone or another, critical alerts let through if you
  want), a minimum severity (all, warnings, critical) and a digest (one notification per
  cluster and check, "3 findings on staging-eu", instead of one per alert), by default and
  per cluster. They are applied when each check runs, by the Go core: an alert kept quiet
  shows "muted · quiet hours until 07:00" or "muted · below the cluster's minimum severity"
  in the cluster's "Since you last looked" card, and is not posted again once the window ends.
  The rules travel in backups.
- **Widgets:** the home-screen widget, and on Wear OS a tile and a complication with the health
  of the cluster on screen on the phone (ready count in its colour, "N down", etcd alarms; the
  check's age once it is over an hour old). The phone sends them its last background check
  over the Wear OS Data Layer: counts and the cluster's name only (masked in screenshot mode), no
  talosconfig or kubeconfig on the watch. Google Play build only: the open-source APKs carry no
  Google library, so they send nothing to a watch. On Android, a Quick Settings tile shows the
  same: the cluster on screen and "All nodes ready" or "2 of 3 nodes ready" from the last check;
  a tap opens the app, a long press its checkup (Settings → Monitoring adds it, Android 13+).
- **Data services:** the storage, database and backup operators the cluster runs (Longhorn, Garage,
  CloudNativePG, Dragonfly, MariaDB, Percona, Velero, Rook Ceph, CSI VolumeSnapshots, backup
  CronJobs) on one tabbed screen, each with its health and what needs a look. The other
  integrations open from their family of the Kubernetes home, with a dot when one needs a look:
  ExternalDNS under Networking, cert-manager, Sealed Secrets and External Secrets under Security,
  CAST AI under Workloads (on a Talos cluster they stay tabs of Data services). ExternalDNS shows the
  hostnames it manages, where each comes from, and the controller's settings; its screen's
  "Check names" button resolves every hostname from the phone and compares it with the record's
  targets, so a name that points elsewhere, or that no controller owns, stands out. Sealed Secrets shows the controller, the
  sealing keys and whether each SealedSecret unsealed, by name only. External Secrets shows
  whether each ExternalSecret syncs, from which store, how often and when it last refreshed,
  flags one that stopped refreshing, and whether its SecretStore or ClusterSecretStore can reach
  its backend; its target Secret is a link, by name only. When the garage-operator
  manages Garage, its clusters, buckets (size, quota, website), keys and node declarations show
  next to the live Garage state. Each Garage cluster's layout opens from its card: the roles
  (zone, capacity or gateway, tags) with how much of each capacity the layout uses, the nodes
  without a role, the version history with the nodes a draining version still waits on (and,
  typed confirmation given, the skip past nodes that will not come back, with or without the
  metadata only they held), and,
  through Garage's own staging, a node added to the layout or its role changed or removed,
  previewed (partitions moved, usable capacity) and applied by typing the new version number,
  or reverted; the zone redundancy can be staged the same way. A layout the garage-operator
  declares is shown read-only. Integrations are declarative plugins; see
  [docs/plugins.md](plugins.md). On Android and iOS, each plugin the cluster runs gets its tile
  in the Data services card and its own tab, drawn from the tree the Go core renders; a plugin
  of another kind (workloads, networking, policies…) is a tool in that family of the Kubernetes
  home. kube-green is one: Workloads → kube-green says which namespaces sleep on a schedule, when
  they wake up, and whether they are asleep right now, and a schedule can be moved from the phone
  (days, sleep and wake-up times, time zone). Workloads → Dataflow lists the Google Cloud
  Dataflow jobs declared through Config Connector (DataflowJob and DataflowFlexTemplateJob):
  each job's state, batch or streaming, its job id and region, and a reconcile that failed
  (a missing dependency, an update refused); jobs launched outside Config Connector are not
  shown. Workloads → Numaflow shows each pipeline and mono vertex (running, paused, failed and
  why), its vertices' pods and its inter-step buffer service, and pauses or resumes a pipeline.
  Workloads → Temporal shows the Temporal clusters the Temporal operator runs (ready or not, and
  which services are down), their namespaces and schedules, and pauses or unpauses a schedule;
  Workloads → Temporal Worker Controller shows each worker rollout (current and target build,
  ramp, a stalled rollout or a connection to Temporal that fails). Plugin actions run from their
  buttons.
- **Plugins of your own:** Settings → Plugins lists the built-in plugins and the ones you
  imported. Import a plugin file (one `just probe plugin-from-crd` drafted from a CRD, or
  `/ichor-crd-plugin` wrote, and `just probe plugin-lint` checked,
  for an operator Ichor does not know) by pasting it, picking it in Files, from an https URL
  (1 MiB at most), or by scanning a QR code (an `ichor://plugin?url=…` link, or the file itself
  over one or several codes made with `just qr-parts`). It is validated first, with the line of each problem; then a consent sheet
  shows what it reads (a Secret whose values it shows in clear is flagged in red) and what it can
  change, with which verbs, before you enable it. It renders like a built-in, with your own
  Kubernetes access, through the resources it declares only; switch it off or remove it there.
  The file stays on the phone, encrypted, and travels with the app backup, on or off as it was.
  Format: [docs/plugins.md](plugins.md#user-plugins).
  For a CRD the app does not show yet, the
  [Plugin request](https://github.com/cyrinux/ichor/issues/new?template=crd-request.yml) issue
  form takes its public manifest link; the
  [website's form](https://cyrinux.github.io/ichor/features.html#plugin-request) and the app's Request an
  integration open it prefilled.
- **Several clusters:** switch from the header, give each one a color and a name of your own, and
  open any of them straight from the app icon (long press: a shortcut / quick action per cluster,
  with "Search clusters" and "Run a checkup" on Android). The cluster menu (tap the title; on
  iOS also ⋯ → Cluster) is grouped: switch or manage the clusters, operate this one (upgrades,
  add a node, runbooks), then share it (incident summary, link).
- **All clusters:** every cluster's last background check on one list, worst first (down or
  critical, degraded, not checked lately, fine): its ready nodes, the critical and warning
  counts, the one thing that is wrong and how old the check is. Opening it calls no cluster;
  "Check every cluster now" runs one. A tap switches to that cluster. From the cluster title
  menu, the top of the cluster list, the tablet rail and the iPad sidebar; the demo shows a
  made-up fleet, and screenshot mode shows the masked names and no alert or app name.
- **Search clusters:** every cluster's last-known nodes, pods, workloads, CronJobs and GitOps
  apps, and every screen of the cluster on screen: type "upgrade", "etcd", "alert set" or a
  node's name and "hard" to open it. With nothing typed it lists the nodes, workloads and
  GitOps apps opened lately (20, on this phone only, never in screenshot mode) and the recent
  searches.
- **Protection:** fingerprint/PIN lock, or a tap of a security key (YubiKey or any FIDO2 key,
  over NFC or USB), which can also seal the stored configs so that nothing reads them without it.
- **AI diagnosis:** optional, off by default: ask Claude or an OpenAI model what is wrong and
  how to fix it, a model downloaded to the phone that answers without sending anything, or hand
  the question to an assistant app (see [AI diagnosis](ai.md#ai-diagnosis-optional)).
