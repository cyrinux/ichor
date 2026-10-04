import SwiftUI
import IchorCore

/// An action asked for one Longhorn volume or node, with the label shown in its messages.
struct LonghornRequest: Identifiable {
    let namespace: String
    let name: String
    let label: String
    let action: LonghornAction
    /// The replica count for .replicas.
    var value = 0

    /// One action at a time per volume or node.
    var id: String { Self.key(namespace: namespace, name: name, node: action.onNode) }

    static func key(namespace: String, name: String, node: Bool) -> String {
        "\(node ? "node" : "volume")|\(namespace)/\(name)"
    }

    init(volume: LonghornVolume, action: LonghornAction, value: Int = 0) {
        namespace = volume.namespace
        name = volume.name
        label = volume.label
        self.action = action
        self.value = value
    }

    init(node: LonghornNode, action: LonghornAction) {
        namespace = node.namespace
        name = node.name
        label = node.name
        self.action = action
    }

    /// What the alert says once Longhorn took the change.
    var doneMessage: String {
        switch action {
        case .backup: String(localized: "Backup of \(label) started")
        case .trim: String(localized: "Filesystem trim of \(label) started")
        case .replicas: String(localized: "Replicas of \(label) set to \(value)")
        case .schedulingOn: String(localized: "\(label) accepts new replicas again")
        case .schedulingOff: String(localized: "\(label) takes no new replicas")
        case .evict: String(localized: "Moving the replicas off \(label)")
        case .cancelEviction: String(localized: "Eviction of \(label) cancelled")
        }
    }
}

extension LonghornAction {
    var onNode: Bool {
        switch self {
        case .backup, .trim, .replicas: false
        case .schedulingOn, .schedulingOff, .evict, .cancelEviction: true
        }
    }
}

/// An action's menu label; the ones that ask before running end with an ellipsis.
struct LonghornActionLabel: View {
    let action: LonghornAction

    var body: some View {
        switch action {
        case .backup: Label("Back up now", systemImage: "icloud.and.arrow.up")
        case .trim: Label("Trim filesystem", systemImage: "scissors")
        case .replicas: Label("Replica count…", systemImage: "square.stack.3d.up")
        case .schedulingOn: Label("Turn scheduling on", systemImage: "play.circle")
        case .schedulingOff: Label("Turn scheduling off", systemImage: "pause.circle")
        case .evict: Label("Evict replicas…", systemImage: "arrow.right.circle")
        case .cancelEviction: Label("Cancel eviction", systemImage: "xmark.circle")
        }
    }
}

/// The actions of a row, for its context menu and swipe actions. Eviction shows as destructive in
/// the menu only: a destructive swipe action makes the row slide away as if deleted.
struct LonghornActionButtons: View {
    let actions: [LonghornAction]
    var inMenu = false
    let onAction: (LonghornAction) -> Void

    var body: some View {
        ForEach(actions, id: \.self) { action in
            Button(role: inMenu && action == .evict ? .destructive : nil) { onAction(action) } label: { LonghornActionLabel(action: action) }
                .tint(color(for: action))
        }
    }

    private func color(for action: LonghornAction) -> Color {
        switch action {
        case .backup: .blue
        case .trim: .indigo
        case .replicas: .teal
        case .schedulingOn, .cancelEviction: .green
        case .schedulingOff: .orange
        case .evict: .red
        }
    }
}

/// Picks a volume's new replica count, from one to a replica per node (or the current count when
/// it is higher), and applies it.
struct LonghornReplicaSheet: View {
    let volume: LonghornVolume
    let nodes: Int
    let onApply: (Int) -> Void

    @Environment(\.dismiss) private var dismiss
    @State private var count: Int

    init(volume: LonghornVolume, nodes: Int, onApply: @escaping (Int) -> Void) {
        self.volume = volume
        self.nodes = nodes
        self.onApply = onApply
        _count = State(initialValue: volume.defaultReplicas(nodes: nodes))
    }

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    Stepper(value: $count, in: LonghornReplicas.min...volume.maxReplicas(nodes: nodes)) {
                        LabeledContent("Replicas", value: count.formatted())
                    }
                } header: {
                    Text(verbatim: volume.label).textCase(nil)
                } footer: {
                    VStack(alignment: .leading, spacing: 6) {
                        Text("Replicas now: \(volume.replicasDesired) wanted, \(volume.replicasHealthy) healthy.")
                        if count > volume.replicasDesired {
                            Text("Longhorn builds the new replicas from a healthy one: disk and network IO while it copies.")
                        } else if count < volume.replicasDesired {
                            Text("Longhorn removes the extra replicas: fewer copies survive a node or disk failure.")
                        }
                        if count == 1 {
                            Text("With a single replica, losing its node or disk loses the data.").foregroundStyle(attentionColor)
                        }
                    }
                }
            }
            .navigationTitle(Text("Replica count"))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() } }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Apply") {
                        onApply(count)
                        dismiss()
                    }
                    .disabled(count == volume.replicasDesired)
                }
            }
        }
        .presentationDetents([.medium])
    }
}
