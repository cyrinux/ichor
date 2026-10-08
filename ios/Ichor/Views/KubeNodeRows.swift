import SwiftUI
import IchorCore

/// A node of a cluster without Talos, on the Kubernetes home and its nodes screen: the row,
/// with the cordon and the drain on a tap (`KubeNodeActionRow`), and the cordon's confirmation
/// and outcome (`kubeCordonDialogs`), which both screens share.

/// `KubeNodeRow` behind a menu: the cordon (asking `cordoning` for a confirmation) and the
/// drain (the maintenance screen). No node screen: it reads Talos.
struct KubeNodeActionRow: View {
    let node: KubeNodeInfo
    @Binding var path: [Route]
    @Binding var cordoning: KubeNodeInfo?

    var body: some View {
        Menu {
            KubeNodeMenu(node: node, path: $path, cordoning: $cordoning)
        } label: {
            KubeNodeRow(node: node)
        }
        .tint(.primary)
    }
}

/// The cordon (or uncordon) and the drain of `node`: the menu's items.
struct KubeNodeMenu: View {
    let node: KubeNodeInfo
    @Binding var path: [Route]
    @Binding var cordoning: KubeNodeInfo?

    var body: some View {
        Button(node.cordoned ? String(localized: "Uncordon") : String(localized: "Cordon"), systemImage: "nosign") {
            cordoning = node
        }
        Button("Drain…", systemImage: "rectangle.portrait.and.arrow.right") {
            path.append(.drain(node: node.name, hostname: node.name))
        }
    }
}

/// A node as Kubernetes sees it: ready or not, cordoned, roles, address, kubelet, the
/// pressure conditions that are on, and where the cloud put it (autoscaler pool, machine
/// type, spot).
struct KubeNodeRow: View {
    let node: KubeNodeInfo

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack(spacing: 8) {
                Circle().fill(node.status.color).frame(width: 8, height: 8).accessibilityHidden(true)
                Text(verbatim: node.name).font(.body.weight(.medium)).lineLimit(1)
                Spacer()
                Text(statusLabel).font(.caption).foregroundStyle(node.status.color)
            }
            Text(verbatim: details).font(.caption).foregroundStyle(.secondary)
            if let provenance { Text(verbatim: provenance).font(.caption).foregroundStyle(.secondary) }
            if !node.pressure.isEmpty {
                Text(verbatim: node.pressure.joined(separator: ", ")).font(.caption).foregroundStyle(.statusWarn)
            }
        }
        .accessibilityElement(children: .combine)
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

    /// Pool, machine type, spot or on-demand: where the cloud put the node, when it says.
    private var provenance: String? {
        var parts: [String] = []
        if let pool = poolLabel { parts.append(pool) }
        if let type = node.instanceType, !type.isEmpty { parts.append(type) }
        if let capacity = capacityLabel { parts.append(capacity) }
        return parts.isEmpty ? nil : parts.joined(separator: " · ")
    }

    /// "Karpenter pool general", "GKE node pool default-pool"…; the bare name for a kind this does not know.
    private var poolLabel: String? {
        guard let pool = node.pool, !pool.isEmpty else { return nil }
        switch node.poolKind {
        case "karpenter": return String(localized: "Karpenter pool \(pool)")
        case "eks": return String(localized: "EKS node group \(pool)")
        case "gke-class": return String(localized: "GKE compute class \(pool)")
        case "gke": return String(localized: "GKE node pool \(pool)")
        case "aks": return String(localized: "AKS agent pool \(pool)")
        default: return pool
        }
    }

    private var capacityLabel: String? {
        switch node.capacity {
        case "spot": return String(localized: "Spot")
        case "on-demand": return String(localized: "On-demand")
        case "reserved": return String(localized: "Reserved")
        default: return nil
        }
    }
}

extension View {
    /// The cordon's confirmation for `cordoning` (nil: none), then `kubectl cordon` /
    /// `uncordon` after the app lock when it is on, its outcome in an alert, and `reload`.
    func kubeCordonDialogs(cordoning: Binding<KubeNodeInfo?>, reload: @escaping () async -> Void) -> some View {
        modifier(KubeCordonDialogs(cordoning: cordoning, reload: reload))
    }
}

private struct KubeCordonDialogs: ViewModifier {
    @Binding var cordoning: KubeNodeInfo?
    let reload: () async -> Void

    @Environment(AppModel.self) private var model
    @State private var message: String?

    func body(content: Content) -> some View {
        content
            .confirmationDialog(String(localized: "Kubernetes scheduling on \(cordoning?.name ?? "")"),
                                isPresented: Binding(get: { cordoning != nil }, set: { if !$0 { cordoning = nil } }),
                                titleVisibility: .visible, presenting: cordoning) { node in
                if node.cordoned {
                    Button("Uncordon") { Task { await cordon(node, on: false) } }
                } else {
                    Button("Cordon", role: .destructive) { Task { await cordon(node, on: true) } }
                }
                Button("Cancel", role: .cancel) {}
            } message: { _ in
                Text("A cordoned node gets no new pods; the pods it runs stay. Uncordon it to schedule pods on it again.")
            }
            .alert(message ?? "", isPresented: Binding(get: { message != nil }, set: { if !$0 { message = nil } })) {
                Button("OK", role: .cancel) {}
            }
    }

    private func cordon(_ node: KubeNodeInfo, on: Bool) async {
        guard let client = model.client else { return }
        let title = on ? String(localized: "Cordon \(node.name)") : String(localized: "Uncordon \(node.name)")
        if model.lock.enabled, let failure = await Authenticator.authenticate(reason: title) {
            message = failure
            return
        }
        do {
            try await client.kubeNodeCordon(kubeNode: node.name, on: on)
            message = on ? String(localized: "\(node.name) is cordoned: no new pods are scheduled on it.")
                : String(localized: "\(node.name) is uncordoned: pods can be scheduled on it again.")
        } catch {
            message = error.localizedDescription
        }
        await reload()
    }
}
