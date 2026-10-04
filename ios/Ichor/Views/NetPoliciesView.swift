import SwiftUI
import IchorCore

/// The cluster's network policies, Kubernetes and Cilium ones (os:admin, any CNI): how isolated
/// each namespace is, then the policies by namespace, cluster-wide ones last. A policy opens
/// its rules.
struct NetPoliciesView: View {
    @Environment(AppModel.self) private var model
    @State private var state: LoadState<NetPolicyReport> = .loading
    @State private var namespace: String?
    @State private var query = ""
    @State private var selected: NetPolicy?

    var body: some View {
        LoadStateView(state: state, retry: load) { report in
            let namespaces = report.policyNamespaces
            let chosen = namespace.flatMap { namespaces.contains($0) ? $0 : nil }
            let sections = report.sections(namespace: chosen, query: query)
            List {
                NetPolicySummary(report: report, namespace: chosen)
                if !report.error.isEmpty {
                    Section { ErrorOrNoticeText(message: report.error) }
                }
                if !namespaces.isEmpty {
                    Section { NamespacePicker(namespaces: namespaces, namespace: $namespace) }
                }
                ForEach(sections) { section in
                    Section {
                        ForEach(section.policies) { policy in
                            Button { selected = policy } label: { NetPolicyRow(policy: policy) }
                        }
                    } header: {
                        if let namespace = section.namespace {
                            Text(verbatim: namespace)
                        } else {
                            Text("Cluster-wide")
                        }
                    }
                }
            }
            .overlay {
                if report.policies.isEmpty {
                    ContentUnavailableView("No network policies", systemImage: "shield",
                                           description: Text("Without a policy, every pod accepts and sends any traffic."))
                } else if sections.isEmpty && !query.isEmpty {
                    ContentUnavailableView.search(text: query)
                }
            }
            .refreshable { await load() }
            .themedBackground()
        }
        .task { await load() }
        // A new API address (set on the Kubernetes screen): read again through it.
        .id(model.client?.kubeServer)
        .searchable(text: $query, prompt: Text("Name, selector or namespace"))
        .navigationTitle("Network policies")
        .navigationBarTitleDisplayMode(.inline)
        .sheet(item: $selected) { NetPolicyDetailSheet(policy: $0) }
    }

    private func load() async {
        guard let client = model.client else { return }
        state = state.refreshed(with: await .from { try await client.networkPolicies() })
    }
}

/// How many policies of which kinds, and per namespace how many pods each direction isolates.
private struct NetPolicySummary: View {
    let report: NetPolicyReport
    let namespace: String?

    var body: some View {
        Section {
            HStack(spacing: 8) {
                Text("\(report.policies.count) policies").font(.headline)
                Spacer()
                ForEach(report.kindCounts, id: \.kind) { entry in
                    HStack(spacing: 3) {
                        NetPolicyKindBadge(kind: entry.kind)
                        Text(verbatim: "\(entry.count)").font(.caption).monospacedDigit().foregroundStyle(.secondary)
                    }
                }
            }
            ForEach(report.namespaces.filter { namespace == nil || $0.namespace == namespace }) { row in
                NetNamespaceRow(row: row)
            }
        } header: {
            Text("Isolation by namespace")
        } footer: {
            Text("An isolated pod only accepts (ingress) or sends (egress) what a policy allows.")
        }
    }
}

private struct NetNamespaceRow: View {
    let row: NetPolicyNamespace

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack {
                Text(verbatim: row.namespace).font(.callout.monospaced()).lineLimit(1)
                Spacer()
                Text("\(row.pods) pods").font(.caption).foregroundStyle(.secondary).monospacedDigit()
            }
            HStack(spacing: 6) {
                chip(.ingress, isolated: row.ingressIsolated)
                chip(.egress, isolated: row.egressIsolated)
            }
        }
    }

    private func chip(_ direction: NetDirection, isolated: Int) -> some View {
        InfoChip(text: "\(direction.label) \(isolated)/\(row.pods)", color: row.isolation(direction).color)
    }
}

/// Name, kind badge, who it applies to, the directions it isolates and its pod count.
private struct NetPolicyRow: View {
    let policy: NetPolicy

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack(spacing: 6) {
                NetPolicyKindBadge(kind: policy.kind)
                Text(verbatim: policy.name)
                    .font(.callout.monospaced())
                    .lineLimit(1)
                    .truncationMode(.middle)
                    .foregroundStyle(.primary)
            }
            Text(verbatim: policy.subjectScope.text)
                .font(.caption.monospaced())
                .foregroundStyle(.secondary)
                .lineLimit(1)
            HStack(spacing: 6) {
                ForEach(NetDirection.allCases, id: \.self) { direction in
                    if policy.isolates(direction) || !policy.rules(direction).isEmpty {
                        Label(direction.label, systemImage: direction.symbol)
                            .font(.caption)
                            .foregroundStyle(policy.isolates(direction) ? Color.green : Color.secondary)
                    }
                }
                Spacer()
                if !policy.nodes {
                    Text("\(policy.podCount) pods").font(.caption).foregroundStyle(.secondary).monospacedDigit()
                }
            }
        }
    }
}
