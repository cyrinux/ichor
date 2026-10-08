import SwiftUI
import IchorCore

/// The home of a cluster added from a kubeconfig, in place of the Talos overview: the API
/// server and who the app is to it, its nodes as Kubernetes sees them (KubeNodes), and the
/// Kubernetes screens. No Talos section or action: the cluster has no Talos API; a node can
/// still be cordoned and drained through Kubernetes.
struct KubeHomeView: View {
    /// The navigation path, shared with the Talos overview.
    @Binding var path: [Route]

    @Environment(AppModel.self) private var model
    @State private var state: LoadState<KubeNodesOverview> = .loading
    /// The node whose cordon to change, after a confirmation.
    @State private var cordoning: KubeNodeInfo?

    var body: some View {
        LoadStateView(state: state, retry: load) { overview in
            List {
                if let ctx = model.activeSummary, ctx.certNotAfter > 0, daysUntil(ctx.certNotAfter) <= certWarnDays {
                    Section { KubeExpiryBanner(notAfter: ctx.certNotAfter) }
                }
                summarySection(overview)
                if let target = model.activeSignInTarget { KubeSignInSection(target: target) }
                nodesSection(overview)
                Section {
                    NavigationLink(value: Route.workloads) {
                        Label("Kubernetes workloads", systemImage: "square.stack.3d.up")
                    }
                    NavigationLink { KubeBrowserView() } label: {
                        Label("Resources", systemImage: "square.grid.3x3")
                    }
                    NavigationLink { HelmReleasesView() } label: {
                        Label("Helm releases", systemImage: "shippingbox")
                    }
                    NavigationLink(value: Route.argoCD(downNodes: [])) {
                        Label { Text(verbatim: "Argo CD") } icon: { Image(systemName: "arrow.triangle.branch") }
                    }
                    NavigationLink(value: Route.flux(downNodes: [])) {
                        Label { Text(verbatim: "Flux") } icon: { Image(systemName: "arrow.triangle.2.circlepath") }
                    }
                    NavigationLink(value: Route.dataServices(hints: "", downNodes: [])) {
                        Label("Data services", systemImage: "externaldrive.connected.to.line.below")
                    }
                    NavigationLink(value: Route.metrics) {
                        Label("Metrics", systemImage: "chart.xyaxis.line")
                    }
                } header: {
                    Text("Kubernetes")
                }
            }
            .refreshable { await load() }
            .themedBackground()
        }
        .navigationTitle(model.activeLabel)
        .kubeCordonDialogs(cordoning: $cordoning, reload: load)
        .toolbarTitleMenu {
            if (model.summary?.contexts.count ?? 0) > 1 {
                Menu {
                    ForEach(model.summary?.contexts ?? []) { context in
                        Button { model.activeContext = context.name } label: {
                            if context.name == model.activeContext {
                                Label(model.labels.of(context), systemImage: "checkmark")
                            } else {
                                Text(model.labels.of(context))
                            }
                        }
                    }
                } label: {
                    Label("Switch cluster", systemImage: "arrow.left.arrow.right")
                }
            }
            Button { path.append(.clusters) } label: { Label("Manage clusters…", systemImage: "square.stack.3d.up") }
            ShareLinkButton(target: .screen(.cluster))
        }
        .safeAreaInset(edge: .top, spacing: 0) {
            if (model.summary?.contexts.count ?? 0) > 1 {
                ClusterBar { path.append(.clusters) }
            }
        }
        .toolbar {
            if model.privacyMask {
                ToolbarItem(placement: .topBarLeading) {
                    Image(systemName: "eye.slash")
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                        .accessibilityLabel(Text("Screenshot mode"))
                }
            }
            ToolbarItemGroup(placement: .primaryAction) {
                Button { path.append(.workloads) } label: {
                    Label("Kubernetes workloads", systemImage: "square.stack.3d.up")
                }
                Button { path.append(.settings) } label: { Label("Settings", systemImage: "gearshape") }
            }
        }
        // Reloads with the screenshot mode too, dropping what was loaded with the old names.
        .task(id: loadID) { await load() }
        // Another cluster: never its name over the previous one's nodes.
        .onChange(of: loadID) { state = .loading }
        // A new network (VPN connected, back on Wi-Fi) is the likely fix: try again at once.
        .task {
            for await _ in NetworkChanges.stream() {
                if case .failed = state { await load() }
            }
        }
    }

    private func summarySection(_ overview: KubeNodesOverview) -> some View {
        Section {
            if let ctx = model.activeSummary {
                LabeledContent("API server", value: ctx.endpoints.first ?? "")
                LabeledContent("Version", value: overview.serverVersion.isEmpty ? String(localized: "unknown") : overview.serverVersion)
                if !overview.forbidden {
                    LabeledContent("Nodes ready", value: "\(overview.readyCount)/\(overview.nodes.count)")
                }
                LabeledContent("Sign-in", value: ctx.localizedAuthLabel)
                if let user = ctx.user, !user.isEmpty { LabeledContent("Signed in as", value: user) }
                if let namespace = ctx.namespace, !namespace.isEmpty { LabeledContent("Namespace", value: namespace) }
                if ctx.certNotAfter > 0 { LabeledContent("Expires", value: localizedCertExpiry(ctx.certNotAfter)) }
            }
        } header: {
            Text("Cluster")
        }
    }

    /// The nodes grouped by status, the worst first; past nodeDenseThreshold a dot per node
    /// (DenseKubeNodes) and the header opens the Kubernetes nodes screen: a section holding
    /// hundreds of rows defeats the home.
    private func nodesSection(_ overview: KubeNodesOverview) -> some View {
        let dense = isDenseCluster(overview.nodes.count)
        return Section {
            if overview.forbidden {
                Text("These credentials may not list the nodes. The other screens show what they may read.")
                    .font(.callout).foregroundStyle(.secondary)
            } else if dense {
                DenseKubeNodes(nodes: overview.nodes, path: $path, cordoning: $cordoning)
            } else {
                ForEach(overview.nodes.byStatus.flatMap(\.nodes)) { node in
                    KubeNodeActionRow(node: node, path: $path, cordoning: $cordoning)
                }
            }
        } header: {
            HStack {
                Text("Nodes")
                Spacer()
                if !overview.forbidden { Text(verbatim: "\(overview.nodes.count)") }
                if dense {
                    Button { path.append(.kubeNodes(filter: nil, nodes: overview.nodes)) } label: {
                        Image(systemName: "chevron.right")
                    }
                    .accessibilityLabel(Text("Show all nodes"))
                }
            }
        }
    }

    /// What the loaded nodes belong to: the context and the screenshot mode generation.
    private var loadID: String { "\(model.activeContext)#\(model.dataGeneration)" }

    private func load() async {
        guard let client = model.client else { return }
        // The call is not cancelled with its task: a slow load of the previous context can
        // end after the new one's, and must not replace it.
        let id = loadID
        let fetched: LoadState<KubeNodesOverview> = await .from { try await client.kubeNodes() }
        guard id == loadID else { return }
        state = state.refreshed(with: fetched)
    }
}

/// The kubeconfig's credentials (client certificate or token) expire within certWarnDays, or
/// have: the app cannot renew them, a new kubeconfig has to be imported.
private struct KubeExpiryBanner: View {
    let notAfter: Int64

    var body: some View {
        let days = daysUntil(notAfter)
        Label {
            Text(days < 0
                 ? String(localized: "The kubeconfig credentials have expired. Import a new kubeconfig.")
                 : String(localized: "The kubeconfig credentials expire \(localizedCertExpiry(notAfter)). Import a new kubeconfig before then."))
        } icon: {
            Image(systemName: "exclamationmark.triangle.fill")
        }
        .foregroundStyle(days < 0 ? Color.red : Color.orange)
    }
}
