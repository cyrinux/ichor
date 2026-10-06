import SwiftUI
import IchorCore

/// Every Dragonfly instance (problems first) with its pods and their master/replica role.
struct DragonflyList: View {
    let status: DragonflyStatus
    let refresh: () async -> Void

    var body: some View {
        List {
            if !status.error.isEmpty { Section { ErrorLine(error: status.error) } }
            Section {
                if status.instances.isEmpty {
                    Text("No Dragonfly instances.").foregroundStyle(.secondary)
                }
                ForEach(status.instances) { InstanceRow(instance: $0) }
            }
        }
        .refreshable { await refresh() }
        .themedBackground()
    }
}

private struct InstanceRow: View {
    let instance: DragonflyInstance

    var body: some View {
        VStack(alignment: .leading, spacing: 3) {
            HStack(spacing: 12) {
                HealthDot(health: instance.health)
                Text(verbatim: instance.label).font(.subheadline.monospaced()).lineLimit(1)
            }
            Text(verbatim: summary)
                .font(.caption)
                .foregroundStyle(instance.health.needsAttention ? instance.health.color : .secondary)
            ForEach(instance.pods) { pod in
                HStack(spacing: 6) {
                    HealthDot(health: pod.ready ? .ok : .critical, size: 8)
                    Text(verbatim: pod.line).font(.caption.monospaced()).foregroundStyle(pod.ready ? .primary : Color.red)
                }
            }
        }
        .accessibilityElement(children: .combine)
    }

    private var summary: String {
        ([String(localized: "\(instance.readyPods)/\(instance.replicas) ready")] + instance.reasons.map(reasonText)).joined(separator: " · ")
    }

    private func reasonText(_ reason: DragonflyReason) -> String {
        switch reason {
        case .noReady: String(localized: "no pod ready")
        case .noMaster: String(localized: "no master")
        case .masters: String(localized: "several masters")
        case .pods: String(localized: "replicas missing")
        // The operator's phase is its own word (a rolling update, replication being set up).
        case .notReady: String(localized: "operator: \(instance.phase)")
        }
    }
}
