import SwiftUI
import IchorCore

/// Tells how many cluster members the talosconfig misses; tap to pick which to add.
struct DiscoveredNodesBanner: View {
    let count: Int
    let open: () -> Void

    var body: some View {
        Button(action: open) {
            Label {
                VStack(alignment: .leading, spacing: 2) {
                    Text("\(count) nodes of the cluster are not in the talosconfig.")
                    Text("Tap to add them to the talosconfig.").font(.caption).foregroundStyle(.secondary)
                }
            } icon: {
                Image(systemName: "plus.circle")
            }
        }
    }
}

/// Picks which discovered `nodes` to add to the talosconfig (all by default).
struct DiscoveredNodesSheet: View {
    let nodes: [DiscoveredNode]
    let add: ([DiscoveredNode]) async throws -> Void
    let notNow: () -> Void

    @Environment(\.dismiss) private var dismiss
    @State private var selected: Set<String>
    @State private var adding = false
    @State private var error: String?

    init(nodes: [DiscoveredNode], add: @escaping ([DiscoveredNode]) async throws -> Void, notNow: @escaping () -> Void) {
        self.nodes = nodes
        self.add = add
        self.notNow = notNow
        _selected = State(initialValue: Set(nodes.map(\.address)))
    }

    var body: some View {
        NavigationStack {
            List {
                Section {
                    ForEach(nodes) { node in
                        Button { toggle(node) } label: {
                            HStack {
                                Image(systemName: selected.contains(node.address) ? "checkmark.circle.fill" : "circle")
                                    .foregroundStyle(selected.contains(node.address) ? Color.accentColor : .secondary)
                                VStack(alignment: .leading) {
                                    Text(node.hostname.isEmpty ? node.address : node.hostname).foregroundStyle(.primary)
                                    Text(verbatim: "\(node.address)  ·  \(roleText(node.role))")
                                        .font(.caption.monospaced()).foregroundStyle(.secondary)
                                }
                            }
                        }
                    }
                } footer: {
                    Text("Cluster discovery knows these nodes, but the talosconfig does not list them. Add the ones to show and manage here.")
                }
                if let error {
                    Section { Text(error).foregroundStyle(.statusBad) }
                }
            }
            .navigationTitle("Add discovered nodes")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Not now") {
                        notNow()
                        dismiss()
                    }
                    .disabled(adding)
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Add \(selected.count) nodes") { Task { await submit() } }
                        .disabled(selected.isEmpty || adding)
                }
            }
        }
    }

    private func toggle(_ node: DiscoveredNode) {
        if selected.contains(node.address) {
            selected.remove(node.address)
        } else {
            selected.insert(node.address)
        }
    }

    private func submit() async {
        adding = true
        defer { adding = false }
        do {
            try await add(nodes.filter { selected.contains($0.address) })
            dismiss()
        } catch {
            self.error = error.localizedDescription
        }
    }

    private func roleText(_ role: String) -> String {
        role == "controlplane" ? String(localized: "control plane") : role
    }
}
