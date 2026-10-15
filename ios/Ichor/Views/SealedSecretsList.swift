import SwiftUI
import IchorCore

/// Sealed Secrets: the controller, the sealing keys (newest first), then every SealedSecret
/// problems first with its scope and whether it unsealed. A row opens the SealedSecret's summary.
/// Names and status only: nothing here decodes a value.
struct SealedSecretsList: View {
    let status: SealedSecretsStatus
    let refresh: () async -> Void

    var body: some View {
        List {
            if !status.error.isEmpty { Section { ErrorLine(error: status.error) } }
            Section {
                ControllerRow(controller: status.controller, reasons: status.reasons)
            }
            if !status.keys.isEmpty {
                Section("Sealing keys") {
                    ForEach(status.keys) { key in KeyRow(key: key) }
                }
            }
            if status.truncated {
                Section {
                    Text(String(localized: "Showing the first \(status.items.count) of \(status.secretCount) SealedSecrets."))
                        .font(.caption).foregroundStyle(.secondary)
                }
            }
            Section {
                if status.items.isEmpty {
                    Text("No SealedSecrets.").foregroundStyle(.secondary)
                }
                ForEach(status.items) { item in SealedSecretRow(item: item) }
            }
        }
        .refreshable { await refresh() }
        .themedBackground()
    }
}

/// The controller's replicas, then the section's own reasons (controller down, keys).
private struct ControllerRow: View {
    let controller: SealedSecretsController?
    let reasons: [SealedSecretsReason]

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack(alignment: .top, spacing: 12) {
                HealthDot(health: controller?.health ?? .warning).padding(.top, 5)
                if let controller {
                    Text(verbatim: controller.label).font(.subheadline.monospaced()).lineLimit(1)
                } else {
                    Text("No controller found").font(.subheadline)
                }
                Spacer()
                if let controller {
                    StatusPill(label: controller.ready ? String(localized: "ready") : String(localized: "not ready"), color: controller.ready ? .green : .red)
                }
            }
            VStack(alignment: .leading, spacing: 2) {
                if let controller {
                    LabeledContent("Replicas", value: "\(controller.readyReplicas)/\(controller.replicas)")
                }
                ForEach(reasons, id: \.self) { reason in
                    Text(verbatim: sealedReasonText(reason)).font(.caption)
                        .foregroundStyle(reason.critical ? Color.red : attentionColor)
                }
            }
            .font(.caption)
        }
    }
}

private struct KeyRow: View {
    let key: SealingKey

    var body: some View {
        HStack(alignment: .top, spacing: 12) {
            VStack(alignment: .leading, spacing: 2) {
                Text(verbatim: key.name).font(.caption.monospaced()).lineLimit(1)
                if key.createdAt > 0 {
                    Text(String(localized: "created \(relativeTime(key.createdAt))")).font(.caption).foregroundStyle(.secondary)
                }
            }
            Spacer()
            if key.active {
                StatusPill(label: String(localized: "active"), color: .green)
            } else if key.compromised {
                StatusPill(label: String(localized: "compromised"), color: .red)
            } else if !key.state.isEmpty {
                StatusPill(label: key.state, color: .secondary)
            }
        }
    }
}

private struct SealedSecretRow: View {
    let item: SealedSecretItem

    var body: some View {
        NavigationLink {
            KubeObjectView(
                resource: KubeAPIResource(group: "bitnami.com", version: "v1alpha1", resource: "sealedsecrets", kind: "SealedSecret"),
                namespace: item.namespace,
                name: item.name,
            )
        } label: {
            VStack(alignment: .leading, spacing: 2) {
                HStack(alignment: .top, spacing: 12) {
                    HealthDot(health: item.health).padding(.top, 5)
                    Text(verbatim: item.label).font(.subheadline.monospaced()).lineLimit(1)
                    Spacer()
                    if !item.scope.isEmpty {
                        Text(verbatim: item.scope).font(.caption.monospaced()).foregroundStyle(.secondary)
                    }
                }
                Text(verbatim: line).font(.caption).foregroundStyle(.secondary).lineLimit(1)
                if !item.synced, let condition = [item.reason, item.message].filter({ !$0.isEmpty }).joined(separator: ": ").nonEmpty {
                    Text(verbatim: condition).font(.caption)
                        .foregroundStyle(item.health.needsAttention ? item.health.color : .secondary)
                        .lineLimit(3)
                }
                ForEach(item.reasons, id: \.self) { reason in
                    Text(verbatim: sealedReasonText(reason)).font(.caption).foregroundStyle(item.health.color)
                }
            }
        }
    }

    private var line: String {
        [String(localized: "\(item.keyCount) keys"),
         item.type.nonEmpty,
         item.immutable ? String(localized: "immutable") : nil,
         item.synced ? String(localized: "unsealed") : nil].compactMap { $0 }.joined(separator: " · ")
    }
}

private func sealedReasonText(_ reason: SealedSecretsReason) -> String {
    switch reason {
    case .notSynced: String(localized: "failed to unseal: its Secret is missing or outdated")
    case .unmanaged: String(localized: "a Secret with this name exists that it does not own")
    case .stale: String(localized: "a newer version is not observed yet")
    case .controllerDown: String(localized: "the controller is not running: nothing gets unsealed")
    case .noKeys: String(localized: "no active sealing key")
    case .keyCompromised: String(localized: "a sealing key is marked compromised")
    case .keyStale: String(localized: "the newest sealing key is over 30 days old: key renewal may be stuck")
    }
}
