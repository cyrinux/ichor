import SwiftUI
import IchorCore

/// The Kubernetes screens that work with these credentials alone, in the families Android
/// groups them in: the Kubernetes home's section, and the Talos overview's (hidden until shown).
/// Native rows: the cluster color only tints the icon tile, never the label, and says nothing
/// about health. The operators' screens show only when the cluster runs them.
struct KubeToolsSections: View {
    /// The systems read with Data services: those outside storage open from their family.
    var services: DataServices?
    /// The active CRD plugins of other categories than Data services.
    var plugins: [PluginDetectItem] = []
    var downNodes: Set<String> = []
    var crossplane = false
    var tailscale = false
    var clusterLifecycle = false
    var argoRollouts = false

    var body: some View {
        Section {
            NavigationLink(value: Route.workloads) {
                toolLabel(KubeHomeAction.workloads)
            }
            NavigationLink { KubeJobsView() } label: {
                toolLabel(Text("Jobs"), systemImage: "checklist")
            }
            NavigationLink { HelmReleasesView() } label: {
                toolLabel(KubeHomeAction.helm)
            }
            NavigationLink { AutoscalingView() } label: {
                toolLabel(Text("Autoscaling"), systemImage: "arrow.up.and.down")
            }
            NavigationLink { VirtualMachinesView() } label: {
                toolLabel(Text("Virtual machines"), systemImage: "desktopcomputer")
            }
            // Only when the cluster serves Argo Rollouts; a refused read (no access) stays quiet.
            if argoRollouts {
                NavigationLink { ArgoRolloutsView() } label: {
                    toolLabel(Text("Argo Rollouts"), systemImage: "arrow.triangle.branch")
                }
            }
            serviceTools(.workloads)
            pluginTools(.workloads)
        } header: {
            Text("Workloads")
        }
        Section {
            NavigationLink { KubeServicesView() } label: {
                toolLabel(Text("Services"), systemImage: "network")
            }
            NavigationLink { NetPoliciesView() } label: {
                toolLabel(KubeHomeAction.networkPolicies)
            }
            NavigationLink { KubeLoadBalancingView() } label: {
                toolLabel(Text(LoadBalancingText.title), systemImage: "point.3.connected.trianglepath.dotted")
            }
            // Only when the cluster runs the Tailscale operator: the read answers "not installed" quickly.
            if tailscale {
                NavigationLink { TailscaleView() } label: {
                    toolLabel(Text("Tailscale"), systemImage: "network.badge.shield.half.filled")
                }
            }
            serviceTools(.networking)
            pluginTools(.networking)
        } header: {
            Text("Networking")
        }
        Section {
            NavigationLink { KubeStorageView() } label: {
                toolLabel(Text("Storage"), systemImage: "externaldrive")
            }
            NavigationLink(value: Route.dataServices(hints: "", downNodes: downNodes)) {
                toolLabel(KubeHomeAction.dataServices)
            }
        } header: {
            Text("Storage")
        }
        // Certificates and secrets: only the systems and plugins that join it, shown when one does.
        if !(services?.detected(.security) ?? []).isEmpty || plugins.contains(where: { $0.toolFamily == .security }) {
            Section {
                serviceTools(.security)
                pluginTools(.security)
            } header: {
                Text("Security")
            }
        }
        Section {
            NavigationLink { KubeLiveEventsView() } label: {
                toolLabel(Text("Kubernetes events"), systemImage: "list.bullet.rectangle")
            }
            NavigationLink(value: Route.metrics) {
                toolLabel(KubeHomeAction.metrics)
            }
            NavigationLink { CheckupView() } label: {
                toolLabel(KubeHomeAction.checkup)
            }
            NavigationLink(value: Route.capacity) {
                toolLabel(KubeHomeAction.capacity)
            }
            NavigationLink { ApiHealthView() } label: {
                toolLabel(KubeHomeAction.apiHealth)
            }
            NavigationLink { PoliciesView() } label: {
                toolLabel(Text("Policies"), systemImage: "checkmark.shield")
            }
            NavigationLink(value: Route.alerts) {
                toolLabel(KubeHomeAction.alerts)
            }
        } header: {
            Text("Observe")
        }
        Section {
            NavigationLink { KubeBrowserView() } label: {
                toolLabel(KubeHomeAction.resources)
            }
            // Only when the cluster runs Crossplane: the read answers "not installed" quickly.
            if crossplane {
                NavigationLink { CrossplaneView() } label: {
                    toolLabel(Text("Crossplane"), systemImage: "square.stack.3d.up")
                }
            }
            // Only on a management cluster running Cluster API or Sidero Metal.
            if clusterLifecycle {
                NavigationLink { ClusterLifecycleView() } label: {
                    toolLabel(Text("Cluster lifecycle"), systemImage: "server.rack")
                }
            }
            ClusterRepoToolLink {
                toolLabel(Text("Repository"), systemImage: "folder.badge.gearshape")
            }
            pluginTools(.browse)
        } header: {
            Text("Browse")
        }
    }

    /// The systems read with Data services that `family` files (ExternalDNS, cert-manager, CAST
    /// AI…): each opens alone, with a dot when it needs a look.
    @ViewBuilder
    private func serviceTools(_ family: ServiceFamily) -> some View {
        if let services {
            ForEach(services.detected(family)) { kind in
                NavigationLink(value: Route.dataServices(hints: "", downNodes: downNodes, kind: kind, solo: true)) {
                    HStack {
                        toolLabel(Text(verbatim: kind.title), systemImage: kind.symbol)
                        if let summary = services.summary(kind), summary.attention > 0 {
                            Spacer()
                            HealthDot(health: summary.health)
                        }
                    }
                }
            }
        }
    }

    /// The active plugins of other categories than Data services joining a family of tools:
    /// each opens its native screen, or its own.
    private func pluginTools(_ family: PluginToolFamily) -> some View {
        ForEach(plugins.filter { $0.toolFamily == family }) { item in
            PluginTileLink(item: item) {
                toolLabel(Text(verbatim: item.name), systemImage: "puzzlepiece.extension")
            }
        }
    }

    private func toolLabel(_ action: KubeHomeAction) -> some View {
        KubeToolLabel(title: action.title, systemImage: action.systemImage)
    }

    private func toolLabel(_ title: Text, systemImage: String) -> some View {
        KubeToolLabel(title: title, systemImage: systemImage)
    }
}

/// A Kubernetes screen's row: the label in the primary color, the icon on a tile tinted with
/// the cluster color.
struct KubeToolLabel: View {
    let title: Text
    let systemImage: String
    @Environment(AppModel.self) private var model

    var body: some View {
        Label {
            title.foregroundStyle(.primary)
        } icon: {
            KubeToolIcon(systemImage: systemImage, accent: model.accent)
        }
    }
}

/// The Talos overview's Kubernetes screens, for roles that may use the Kubernetes API. The
/// operators' screens (Crossplane, Tailscale...) stay out: the overview does not look for them.
struct OverviewKubeTools: View {
    let dataServices: LoadState<DataServices>?
    let plugins: [PluginDetectItem]
    let downNodes: Set<String>
    @Environment(AppModel.self) private var model

    var body: some View {
        if model.allows(.workloads) {
            KubeToolsSections(services: loaded, plugins: plugins, downNodes: downNodes)
        }
    }

    private var loaded: DataServices? {
        if case .loaded(let services, _, _) = dataServices { return services }
        return nil
    }
}
