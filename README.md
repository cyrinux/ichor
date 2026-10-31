# Ichor for Kubernetes and Talos Linux

[![Android CI](https://github.com/cyrinux/ichor/actions/workflows/android.yml/badge.svg?branch=main)](https://github.com/cyrinux/ichor/actions/workflows/android.yml)
[![iOS CI](https://github.com/cyrinux/ichor/actions/workflows/ios.yml/badge.svg?branch=main)](https://github.com/cyrinux/ichor/actions/workflows/ios.yml)

**Any Kubernetes cluster from its kubeconfig, and the whole Talos layer, from your phone.**
Ichor is a free, open-source Android and iOS app to monitor and operate Kubernetes clusters:
workloads, pods, logs, Argo CD and Flux, on EKS, GKE, AKS, DigitalOcean, Rancher, Omni or your
own (see [Kubernetes clusters without Talos](docs/clusters.md#kubernetes-clusters-without-talos)). On
[Talos Linux](https://www.talos.dev) clusters it goes down to the nodes: node health, etcd,
KubeSpan, upgrades, machine config and live graphs in your pocket, and a notification the
moment a node goes down.

Website: <https://cyrinux.github.io/ichor/>

In the app, **Settings → About → Ichor website** opens the project site.
**About → Integrations → Platform → Talos Linux** opens the Talos website.

Want to explore without a cluster? Tap **Try demo** on the import screen.
The built-in demo works offline and shows five sample nodes, live metrics, services,
logs, events, etcd, networking, storage, hardware and resources. The overview marks
it as demo data; cluster changes and credential exports are unavailable. It can
sit alongside your real clusters and be removed from **Manage clusters**.
The first time the Talos demo opens, a short tour points at the cluster switcher, the nodes,
a node that needs a look, the action bar and Settings; **Settings → About → Replay the demo
tour** shows it again.

> Ichor is an independent community project, not affiliated with or endorsed by Sidero Labs.
> Talos is a trademark of Sidero Labs, Inc.

## What it does

- **Talos nodes:** status, services, resources, logs, events, etcd, live graphs, machine config
  with try mode, hardware, storage, KubeSpan (a red banner when a tunnel is down, also said on the home summary and in Cluster health), packet capture and a debug shell. Reboot, shut down
  or wake a node, upgrade Talos node by node or the whole cluster, upgrade Kubernetes, add,
  replace, reset or wipe a node, recover etcd, rotate the Talos CA. Alerts when a node goes down, with quiet hours, a per-cluster
  minimum severity, a digest, and critical alerts that repeat until acknowledged.
- **Any Kubernetes cluster:** workloads with rollout history and undo for Deployments,
  StatefulSets and DaemonSets (the pod template diff first) and a Deployment's pause, pods, logs and shells, a browser of every object with
  summary, YAML, scale and delete, Jobs, Services and load balancing, storage, autoscaling,
  policies, virtual machines, capacity, live events and Alertmanager (acknowledge or resolve the
  PagerDuty incident from the alert), with RBAC checked before you tap. Clusters come from a kubeconfig or a cloud account: EKS, GKE, AKS, DigitalOcean,
  Rancher, Omni, OIDC. The `?` on the add screen says what each way in needs: a cloud
  account, a kubeconfig or a Talos identity for the phone.
- **GitOps:** Argo CD and Flux apps with the diff a sync would apply, Argo Rollouts, the files
  an app deploys at the revision the cluster runs, edits reviewed and opened as pull requests,
  a cluster repository for Talos machine config patches, offered a dependency-update bot
  configuration when it has none, and the images and charts with a newer version, one tap from
  the pull request that bumps them (or select several and propose them as one pull request).
- **Operators:** Longhorn, CloudNativePG, Garage, Rook Ceph, Velero, cert-manager, External
  Secrets, Sealed Secrets, ExternalDNS, Crossplane, Tailscale, Cluster API, Kyverno,
  Gatekeeper, KubeVirt, CAST AI and more, each with its findings in the cluster checkup, and
  [plugins](docs/plugins.md) for any CRD of your own.
- **Troubleshooting:** cluster checkup, configuration drift, incident recorder, node pressure,
  bottleneck metrics, network tests between nodes, node network tools, image vulnerability
  scans, thirty days of history and an optional AI diagnosis.
- **Operating safely:** read-only by default, typed confirmations, dry runs, an encrypted audit
  log, runbooks with parameters and a confirmation per destructive step, a security key or biometric lock,
  a panic-app response (lock, or delete the saved configs; Android),
  configs sealed in the phone's security chip, backups that move between Android and iPhone.

The full list, screen by screen, is in [docs/features.md](docs/features.md); the
[website](https://cyrinux.github.io/ichor/) shows the highlights of each release.

The Talos API layer is the official Go client (`siderolabs/talos/pkg/machinery`, same code as
`talosctl`) compiled with gomobile, so talosconfig parsing, Ed25519 mTLS and endpoint→node
proxying behave exactly like `talosctl`.

[<img src="https://raw.githubusercontent.com/ImranR98/Obtainium/main/assets/graphics/badge_obtainium.png" alt="Get it on Obtainium" height="54">](https://apps.obtainium.imranr.dev/redirect?r=obtainium://app/%7B%22id%22%3A%22name.levis.ichor.foss%22%2C%22url%22%3A%22https%3A%2F%2Fgithub.com%2Fcyrinux%2Fichor%22%2C%22author%22%3A%22cyrinux%22%2C%22name%22%3A%22Ichor%22%2C%22additionalSettings%22%3A%22%7B%5C%22apkFilterRegEx%5C%22%3A%20%5C%22%5E%28%3F%21.%2Aunsigned%29.%2A%5C%5C%5C%5C.apk%24%5C%22%7D%22%7D)

**Install on Android:**

- **[Obtainium](https://obtainium.imranr.dev)** installs and updates the app straight from this
  repository's GitHub releases. The button above pre-fills it, with a filter that skips
  unsigned APKs.
- **Manually:** download `ichor-v<version>-<abi>.apk` from the
  [latest release](https://github.com/cyrinux/ichor/releases/latest); `arm64-v8a`
  fits almost all phones.

**F-Droid:** not listed yet. The planned route is [IzzyOnDroid](https://apt.izzysoft.de/fdroid/),
an F-Droid repository that ships the signed release APKs. Release APKs are rebuilt from their
tag and compared in CI (`just verify-release`, see
[Reproducible builds](CONTRIBUTING.md#reproducible-builds)). The release also carries
`ichor-wear-v<version>.apk`, the Wear OS tile: it only works with the Google Play phone build
(same package and signing key), so it is not meant for F-Droid.

## Documentation

| Read | For |
|---|---|
| [Features](docs/features.md) | Everything the app does, screen by screen |
| [Roles](docs/roles.md) | Which talosconfig role each feature needs |
| [Adding clusters](docs/clusters.md) | Talosconfig for the phone, kubeconfig and cloud clusters, Omni, several clusters, share links, backup and restore |
| [Kubernetes screens](docs/kubernetes.md) | Checkup, CronJobs, capacity, policies, autoscaling, virtual machines, Crossplane, Tailscale, Cluster API, image scans, Argo CD icons |
| [Network](docs/network.md) | Node network tools, packet capture, network test |
| [Safety](docs/safety.md) | What protects the cluster and the phone |
| [etcd](docs/etcd.md) | Snapshots, recovery, the NOSPACE fix |
| [Cluster insights](docs/insights.md) | Drift, incident recorder, bottleneck metrics |
| [AI diagnosis](docs/ai.md) | Optional, off by default |
| [Plugins](docs/plugins.md) | The plugin file format and how to write one from a CRD |
| [Runbooks](docs/runbooks.md) | The runbook format and its actions |
| [Build](docs/build.md) | Repository layout, Android, iOS and CI builds |
| [Contributing](CONTRIBUTING.md) | Checks, reproducible builds, conventions |

## Updates

The Android app updates itself from this repository's GitHub releases (Settings → About → Updates):

- **When it checks:** once a day, or when you tap "Check now".
- **What it downloads:** the signed APK for the phone's ABI, after you confirm.
- **Verification before installing:**
  1. the SHA-256 checksum GitHub publishes for the file; a release without one is refused;
  2. the package name;
  3. that it is signed with exactly the installed app's key, or with a key rotated from it
     (APK signature scheme v3 lineage, Android 9+).
- **Installing:** Android shows its own confirmation, and asks once to allow
  "Install unknown apps" for Ichor.

Updates only work between builds signed with the same key, which means CI release builds with
the `ICHOR_KEYSTORE*` secrets set. A **debug** build, signed with your machine's debug key,
cannot be replaced by a release. To switch once:

1. uninstall the debug build;
2. install `ichor-vX.Y.Z-<abi>.apk` from a release;
3. re-import the talosconfig.

There is no self-update on iOS, where sideloaded apps are reinstalled with Sideloadly or AltStore.

## Languages

The app is available in English, French, Spanish, Ukrainian, German, Italian, Simplified Chinese, Japanese, Brazilian Portuguese and Russian.

- **Android:** pick a language in Settings → Appearance → Language. On Android 13+, the
  system's per-app language settings work too.
- **iOS:** iOS sets the app's language in the Settings app; the app's Language row opens it.

On Android, Settings → Appearance → UI style picks how the app is drawn: Material 3 or Material 3 Expressive.

When you add or change a UI string, translate it in every language:

| Platform | English source | Translations |
|---|---|---|
| Android | `app/src/main/res/values/strings.xml` | `values-{fr,es,uk,de,it,zh-rCN,ja,pt-rBR,ru}/strings.xml` |
| iOS | `ios/**/Localizable.xcstrings` | the same catalogs (String Catalogs) |

Then run:

```sh
just i18n-check   # also part of `just check` and the iOS CI build
```

It fails on a missing translation, when placeholders (`%1$s`, `%@`, `%lld`) differ from
English, or when a plural lacks one of its language's categories (Russian needs `few` and
`many`, Chinese only `other`). It also fails on an Android key that no code or resource reads:
delete it in every language's file.

## Support the project

Ichor is free, with no ads or tracking. To help keep it going:

- [GitHub Sponsors](https://github.com/sponsors/cyrinux)
- Bitcoin: `bc1qc0dhqrgw6z08du94rkfequk8n5r3lgcr5lnxtl`
- Ethereum: `0xb32676301F9c4abD35Eb2e4c7C8cdA754BA29804`

The app's About section and the [website](https://cyrinux.github.io/ichor/features.html#support) show them as QR codes.

The Google Play version can't show those links (Play's payment rules). Instead,
**Settings → About → Fund features** lets you back the features you want next, with Google
Play purchases. The list is [`docs/roadmap.json`](docs/roadmap.json); the maintainer side is
in [fastlane/PLAY_CONSOLE.md](fastlane/PLAY_CONSOLE.md#feature-funding-in-app-products).

## License

Apache License 2.0; see [LICENSE](LICENSE) and [NOTICE](NOTICE) for third-party components.
The Ichor name, logo and icon are not covered by the license; see [TRADEMARKS.md](TRADEMARKS.md).
To contribute, see [CONTRIBUTING.md](CONTRIBUTING.md).
