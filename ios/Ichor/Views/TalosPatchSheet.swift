import SwiftUI
import IchorCore

/// A repository file that is a Talos patch: "Try on a node…" when it can be tried, else why not
/// (a secret, a whole config, a talenv placeholder).
struct TalosPatchBar: View {
    let info: TalosPatchInfo
    let onTry: () -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            Label("Talos patch", systemImage: "testtube.2")
                .font(.subheadline.weight(.semibold))
                .foregroundStyle(.primary)
            if info.tryable {
                Text("Try it on a node before it is merged: it reverts by itself unless you keep it.")
                Button("Try on a node…", action: onTry)
                    .buttonStyle(.bordered)
            } else {
                Text("Cannot be tried: \(info.note)")
            }
        }
        .font(.footnote)
        .foregroundStyle(.secondary)
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(.horizontal)
        .padding(.vertical, 8)
        .background(.bar)
    }
}

/// The node to try the patch `info` on: the cluster's reachable nodes, the one the patch sets
/// the hostname of picked first, with a warning when another one is picked. The review then
/// offers other nodes too when the patch only sets values.
struct TalosPatchNodeSheet: View {
    let info: TalosPatchInfo
    let onPick: (NodeOverview) -> Void

    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    @State private var state: LoadState<[NodeOverview]> = .loading
    @State private var picked: String?

    var body: some View {
        NavigationStack {
            LoadStateView(state: state, retry: load) { nodes in list(nodes) }
                .navigationTitle("Try on which node?")
                .navigationBarTitleDisplayMode(.inline)
                .toolbar {
                    ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() } }
                }
        }
        .task { await load() }
    }

    private func list(_ nodes: [NodeOverview]) -> some View {
        let meant = info.node(in: nodes)
        return List {
            Section {
                if nodes.isEmpty {
                    Text("No node of this cluster answers.").foregroundStyle(.secondary)
                }
                ForEach(nodes) { node in
                    Button { picked = node.node } label: { row(node) }
                        .foregroundStyle(.primary)
                        .accessibilityAddTraits(picked == node.node ? .isSelected : [])
                }
            } footer: {
                VStack(alignment: .leading, spacing: 6) {
                    if !info.hostnames.isEmpty, let picked, picked != meant?.node {
                        Text("This patch is for \(info.hostnames.joined(separator: ", ")): another node would get its settings.")
                            .foregroundStyle(.statusWarn)
                    }
                    Text("The review shows what changes on that node. When the patch only sets values, it can be taken to other nodes from there.")
                }
            }
            Section {
                Button("Review on this node") {
                    if let node = nodes.first(where: { $0.node == picked }) { onPick(node) }
                }
                .disabled(picked == nil)
            }
        }
        .themedBackground()
    }

    private func row(_ node: NodeOverview) -> some View {
        HStack {
            VStack(alignment: .leading, spacing: 2) {
                Text(verbatim: node.hostname.isEmpty ? node.node : node.hostname)
                Text(verbatim: "\(node.node) · \(node.role)").font(.caption).foregroundStyle(.secondary)
            }
            Spacer()
            if picked == node.node {
                Image(systemName: "checkmark").foregroundStyle(.tint).accessibilityHidden(true)
            }
        }
        .contentShape(Rectangle())
    }

    private func load() async {
        guard let client = model.client else { return }
        state = await .from { try await client.overview().nodes.filter(\.reachable) }
        if case .loaded(let nodes, _, _) = state, picked == nil {
            picked = (info.node(in: nodes) ?? (nodes.count == 1 ? nodes.first : nil))?.node
        }
    }
}

/// Where "Try on a node…" leads: the machine config screen of `node`, with the patch.
struct PatchTarget: Hashable, Identifiable {
    let node: String
    let hostname: String
    let patch: PatchRequest

    var id: String { node + "\u{0}" + patch.from }
}
