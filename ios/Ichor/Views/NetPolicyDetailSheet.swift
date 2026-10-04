import SwiftUI
import IchorCore

/// One network policy: who it applies to and the pods it selects now, then per direction
/// whether it isolates them and each rule, with its peers as chips, its ports and L7 filters.
struct NetPolicyDetailSheet: View {
    let policy: NetPolicy

    @Environment(\.dismiss) private var dismiss

    var body: some View {
        NavigationStack {
            List {
                Section { header }
                Section("Applies to") {
                    Text(verbatim: policy.subjectScope.text).font(.callout.monospaced()).textSelection(.enabled)
                    if !policy.subjectNamespace.isEmpty {
                        LabeledContent("Namespace") { Text(verbatim: policy.subjectNamespace).font(.callout.monospaced()) }
                    }
                }
                if !policy.nodes {
                    podsSection
                }
                NetDirectionSection(policy: policy, direction: .ingress)
                NetDirectionSection(policy: policy, direction: .egress)
                if policy.created > 0 {
                    Section {
                        LabeledContent("Created") {
                            Text(Date(epochMillis: policy.created), format: .relative(presentation: .named))
                        }
                    }
                }
            }
            .themedBackground()
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .confirmationAction) { Button("Done") { dismiss() } }
            }
        }
        .presentationDetents([.medium, .large])
        .presentationDragIndicator(.visible)
    }

    private var header: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(spacing: 6) {
                NetPolicyKindBadge(kind: policy.kind)
                Text(verbatim: policy.kindName).font(.caption).foregroundStyle(.secondary)
            }
            Text(verbatim: policy.name).font(.headline.monospaced()).textSelection(.enabled)
            if policy.clusterWide {
                Text("Cluster-wide").font(.subheadline).foregroundStyle(.secondary)
            } else {
                Text(verbatim: policy.namespace).font(.subheadline.monospaced()).foregroundStyle(.secondary)
            }
            if !policy.description.isEmpty {
                Text(verbatim: policy.description).font(.callout)
            }
        }
    }

    private var podsSection: some View {
        Section {
            if policy.pods.isEmpty {
                Text("No pod matches it now.").note()
            }
            ForEach(policy.pods, id: \.self) { pod in
                Text(verbatim: pod).font(.caption.monospaced()).lineLimit(1).truncationMode(.middle)
            }
            if policy.podCount > policy.pods.count {
                Text("and \(policy.podCount - policy.pods.count) more").font(.caption).foregroundStyle(.secondary)
            }
        } header: {
            Text("Selected pods (\(policy.podCount))")
        }
    }
}

/// Ingress or egress: isolated or not, then the rules.
private struct NetDirectionSection: View {
    let policy: NetPolicy
    let direction: NetDirection

    var body: some View {
        let rules = policy.rules(direction)
        Section {
            effect
            ForEach(rules.indices, id: \.self) { index in
                NetRuleRow(rule: rules[index], direction: direction, clusterWide: policy.clusterWide)
            }
        } header: {
            Label(direction.label, systemImage: direction.symbol)
        }
    }

    @ViewBuilder private var effect: some View {
        switch policy.effect(direction) {
        case .isolated:
            Label("Isolated: only the rules below are allowed", systemImage: "lock.fill")
                .font(.callout)
                .foregroundStyle(.green)
        case .denyAll:
            Label("Deny all: no traffic is allowed", systemImage: "nosign")
                .font(.callout)
                .foregroundStyle(.red)
        case .rulesOnly:
            Label("Not isolated by this policy: its rules add to other policies", systemImage: "lock.open")
                .font(.callout)
                .foregroundStyle(.secondary)
        case .open:
            Label("Not restricted", systemImage: "lock.open")
                .font(.callout)
                .foregroundStyle(.secondary)
        }
    }
}

/// Allow/Deny, "From"/"To" peers, the ports, the L7 filters.
private struct NetRuleRow: View {
    let rule: NetRule
    let direction: NetDirection
    let clusterWide: Bool

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            InfoChip(text: rule.deny ? String(localized: "Deny") : String(localized: "Allow"), color: rule.deny ? .red : .green)
            Text(direction == .ingress ? LocalizedStringKey("From") : LocalizedStringKey("To")).font(.caption).foregroundStyle(.secondary)
            ChipFlow {
                if rule.peers.isEmpty {
                    InfoChip(text: String(localized: "Anyone"))
                }
                ForEach(rule.peers.indices, id: \.self) { index in
                    NetPeerChip(peer: rule.peers[index], clusterWide: clusterWide)
                }
            }
            Text("On").font(.caption).foregroundStyle(.secondary)
            ChipFlow {
                if rule.ports.isEmpty {
                    InfoChip(text: String(localized: "Any port"))
                }
                ForEach(rule.ports.indices, id: \.self) { index in
                    InfoChip(text: rule.ports[index].text, monospaced: true)
                }
            }
            ForEach(rule.l7, id: \.self) { line in
                Label { Text(verbatim: line).font(.caption.monospaced()) } icon: { Image(systemName: "text.magnifyingglass") }
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
        }
        .padding(.vertical, 2)
    }
}
