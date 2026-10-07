import SwiftUI
import IchorCore

/// The home of a cluster added from a kubeconfig, in place of the Talos overview: the API
/// server and who the app is to it, its nodes as Kubernetes sees them (KubeNodes), and the
/// Kubernetes screens. No Talos section or action: the cluster has no Talos API.
struct KubeHomeView: View {
    /// The navigation path, shared with the Talos overview.
    @Binding var path: [Route]

    @Environment(AppModel.self) private var model
    @State private var state: LoadState<KubeNodesOverview> = .loading

    var body: some View {
        LoadStateView(state: state, retry: load) { overview in
            List {
                if let ctx = model.activeSummary, ctx.certNotAfter > 0, daysUntil(ctx.certNotAfter) <= certWarnDays {
                    Section { KubeExpiryBanner(notAfter: ctx.certNotAfter) }
                }
                summarySection(overview)
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

    private func nodesSection(_ overview: KubeNodesOverview) -> some View {
        Section {
            if overview.forbidden {
                Text("These credentials may not list the nodes. The other screens show what they may read.")
                    .font(.callout).foregroundStyle(.secondary)
            } else {
                // Those needing attention first, then in the core's order (control planes first).
                ForEach(overview.nodes.filter(\.needsAttention) + overview.nodes.filter { !$0.needsAttention }) { node in
                    KubeNodeRow(node: node)
                }
            }
        } header: {
            HStack {
                Text("Nodes")
                Spacer()
                if !overview.forbidden { Text(verbatim: "\(overview.nodes.count)") }
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

/// A node as Kubernetes sees it: ready or not, cordoned, roles, address, kubelet and the
/// pressure conditions that are on. No node screen: that one reads Talos.
private struct KubeNodeRow: View {
    let node: KubeNodeInfo

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack(spacing: 8) {
                Circle().fill(statusColor).frame(width: 8, height: 8).accessibilityHidden(true)
                Text(verbatim: node.name).font(.body.weight(.medium)).lineLimit(1)
                Spacer()
                Text(statusLabel).font(.caption).foregroundStyle(statusColor)
            }
            Text(verbatim: details).font(.caption).foregroundStyle(.secondary)
            if !node.pressure.isEmpty {
                Text(verbatim: node.pressure.joined(separator: ", ")).font(.caption).foregroundStyle(.statusWarn)
            }
        }
        .accessibilityElement(children: .combine)
    }

    private var statusColor: Color {
        if !node.ready { return .statusBad }
        return node.cordoned || !node.pressure.isEmpty ? .statusWarn : .statusOK
    }

    private var statusLabel: String {
        if !node.ready { return String(localized: "Not ready") }
        return node.cordoned ? String(localized: "Cordoned") : String(localized: "Ready")
    }

    /// Roles, address, kubelet version, capacity: what is known, in one line.
    private var details: String {
        var parts: [String] = []
        if !node.roles.isEmpty { parts.append(node.roles.joined(separator: ", ")) }
        if let address = node.address { parts.append(address) }
        if let kubelet = node.kubelet, !kubelet.isEmpty { parts.append(kubelet) }
        if node.cpu > 0 { parts.append(String(localized: "\(formatCores(node.cpu)) CPU")) }
        if node.memory > 0 { parts.append(formatBytes(Int64(node.memory))) }
        return parts.joined(separator: " · ")
    }

    private func formatCores(_ cores: Double) -> String {
        cores == cores.rounded() ? String(Int(cores)) : String(format: "%.1f", cores)
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
