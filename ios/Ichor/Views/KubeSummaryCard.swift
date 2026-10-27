import SwiftUI
import IchorCore

/// The Kubernetes home's summary, as the Talos ClusterSummaryCard: the cluster's name, "9 nodes
/// · 1 not ready · v1.31.2" and the overall state, the API server and who the credentials are
/// on one line, then the nodes ready, the cores and the memory, in use when metrics-server
/// answers (`usage`). Same as Android's KubeSummaryCard; the Checkup and Capacity rows under it
/// are its footer.
struct KubeSummaryCard: View {
    let name: String
    let context: ContextSummary
    let overview: KubeNodesOverview
    let usage: [KubeTopNode]
    /// Who the API server takes the credentials for, when it said.
    let whoAmI: KubeWhoAmI?

    @Environment(AppModel.self) private var model
    @Environment(\.colorScheme) private var colorScheme

    var body: some View {
        let colors = model.summaryColors(for: colorScheme)
        let summary = listed ? overview.clusterSummary(usage: usage) : nil
        VStack(alignment: .leading, spacing: 10) {
            HStack(alignment: .center, spacing: 8) {
                BundledLogo(name: clusterLogo(context))
                    .frame(width: 22, height: 22)
                    .accessibilityHidden(true)
                Text(verbatim: name).font(.headline).lineLimit(1).accessibilityAddTraits(.isHeader)
            }
            .identityArea(colors)
            HStack(alignment: .center, spacing: 8) {
                headline(summary).font(.caption).foregroundStyle(.secondary).monospacedDigit()
                Spacer()
                if let summary { ClusterStatusPill(status: summary.status) }
            }
            if !identity.isEmpty {
                Text(verbatim: identity)
                    .font(.caption.monospaced())
                    .foregroundStyle(.secondary)
                    .lineLimit(1)
                    .truncationMode(.middle)
            }
            Divider()
            HStack(alignment: .top, spacing: 12) {
                StatTile(icon: "cube", value: summary.map { "\($0.ready)/\($0.total)" } ?? unknown, label: String(localized: "Nodes"))
                    .frame(maxWidth: .infinity, alignment: .leading)
                cpu(summary)
                    .frame(maxWidth: .infinity, alignment: .leading)
                memory(summary)
                    .frame(maxWidth: .infinity, alignment: .leading)
            }
            .environment(\.summaryColors, colors)
        }
        .padding(.vertical, 4)
    }

    private var listed: Bool { !overview.forbidden && !overview.nodes.isEmpty }

    /// "9 nodes  ·  1 not ready  ·  v1.31.2"; the version alone while no node is listed.
    private func headline(_ summary: KubeClusterSummary?) -> Text {
        let version = overview.serverVersion.isEmpty ? "Kubernetes" : overview.serverVersion
        var parts: [Text] = []
        if let summary {
            parts.append(Text(String(localized: "\(summary.total) nodes")))
            if summary.notReady > 0 {
                parts.append(Text(String(localized: "\(summary.notReady) not ready")).foregroundStyle(.statusWarn))
            }
        }
        parts.append(Text(verbatim: version))
        return dotJoined(parts)
    }

    /// The API server's host and the user the credentials are (as the API server takes them, else the kubeconfig's).
    private var identity: String {
        let server = context.endpoints.first.map { endpoint in
            let host = endpoint.components(separatedBy: "://").last ?? endpoint
            return host.components(separatedBy: "/").first ?? host
        } ?? ""
        let user = whoAmI.flatMap { $0.isKnown ? $0.user : nil } ?? context.user ?? ""
        return [server, user].filter { !$0.isEmpty }.joined(separator: "  ·  ")
    }

    @ViewBuilder
    private func cpu(_ summary: KubeClusterSummary?) -> some View {
        let cores = summary?.cpuCount ?? 0
        if let used = summary?.cpuFraction {
            VStack(alignment: .leading, spacing: 6) {
                StatTile(icon: "cpu", value: percent(used),
                         label: cores > 0 ? String(localized: "of \(cores) cores") : String(localized: "CPU in use"))
                UsageBar(fraction: used).animation(.easeInOut, value: used)
            }
        } else {
            StatTile(icon: "cpu", value: cores > 0 ? "\(cores)" : unknown, label: String(localized: "CPU cores"))
        }
    }

    @ViewBuilder
    private func memory(_ summary: KubeClusterSummary?) -> some View {
        let total = summary?.memTotal ?? 0
        let used = summary?.memFraction
        VStack(alignment: .leading, spacing: 6) {
            StatTile(icon: "memorychip", value: total > 0 ? formatBytes(total) : unknown,
                     label: used.map { String(localized: "\(percent($0)) used") } ?? String(localized: "Memory"))
            if let used {
                UsageBar(fraction: used).animation(.easeInOut, value: used)
            }
        }
    }
}

/// Unknown capacity (no node said): a dash rather than a misleading 0.
private let unknown = "—"

/// "42 %" as the locale writes it.
private func percent(_ fraction: Double) -> String {
    fraction.formatted(.percent.precision(.fractionLength(0)))
}
