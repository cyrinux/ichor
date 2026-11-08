# iOS views, by feature

The SwiftUI views of the app, one folder per feature. Each folder names the Android
`app/src/main/java/name/levis/ichor/ui/` packages it mirrors, so the iOS twin of an Android
screen is found by folder. A new screen goes in the folder of its Android twin; a view used by
several folders goes in `Components/`. xcodegen picks up every subfolder (`ios/project.yml`,
`path: Ichor`); swift-format lints them recursively.

| Folder | Mirrors (Android `ui/…`) | Holds |
|---|---|---|
| `Overview/` | overview, tour, checkup, health, insights, diagnosis, history, search, apihealth, incident | the cluster homes (Talos and Kubernetes), checkup, health, insights, AI diagnosis, search, demo tour |
| `Clusters/` | importconfig, kubeauth, (fleet) | add a cluster, cloud discovery and sign-in, the cluster list and fleet, lock screen, VPN gate |
| `Node/` | node, nodes, hardware, live, logs, metrics, network, nettools, capture, resources | a Talos node's screens: services, logs, graphs, hardware, disks, network tools, packet capture, power |
| `Talos/` | machineconfig, upgrade, maintenance, lifecycle, rotateca, etcd, kubespan, join, issueconfig | machine config editor and try mode, upgrades, maintenance, etcd, KubeSpan, join a node, boot media |
| `Kube/` | kubebrowser, kubenodes, kubeevents, workloads, autoscaling, netpol, policies, loadbalancing, events | the Kubernetes object browser, workloads, pods, nodes, events, autoscaling, policies, services |
| `Apps/` | apps | the GitOps app list and an app's detail (workloads, routes, scans) |
| `Argo/` | argocd, argorollouts | Argo CD apps, sync, windows, network graph, Rollouts |
| `Flux/` | flux | Flux apps, sources, diffs and the dependency tree |
| `Helm/` | (helm in workloads) | Helm releases and rollback |
| `GitOps/` | gitops, diff | the source browser and editor, quick edits, proposals, staged changes, SOPS, the cluster repository |
| `DataServices/` | dataservices, storage, vms, tailscale, crossplane, integrations | operators: Longhorn, CNPG, Garage, Velero, VMs, Prometheus, CAST AI, cert-manager, snapshots, storage |
| `Images/` | images, imagescan | the image inventory and vulnerability scans |
| `Updates/` | updates | newer versions of images and charts |
| `Alerts/` | alerts | the Alerts screen and its detail |
| `Runbooks/` | runbooks | runbooks: list, plan, run, import |
| `Plugins/` | plugins | CRD plugins: cards, actions, import, integration requests |
| `Settings/` | settings, support, backup, changelog, activity | Settings pages and sections, Git hosts, security keys, audit log, support bundle, licenses |
| `Components/` | components, theme | shared rows, cards, styles, sheets and helpers used by several folders |
