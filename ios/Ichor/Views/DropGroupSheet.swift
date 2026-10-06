import SwiftUI
import UIKit
import IchorCore

/// Flows dropped alike: both peers, the reason in words, the policies that denied them or put
/// the pod in default-deny (each opens its rules), what to change, and a sample flow to copy.
struct DropGroupSheet: View {
    let group: HubbleDropGroup

    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    /// Read on the first policy tapped, then kept for the others.
    @State private var policies: NetPolicyReport?
    @State private var loading: NetPolicyRef?
    @State private var opened: NetPolicy?
    @State private var message: String?
    @State private var copied = 0

    var body: some View {
        NavigationStack {
            List {
                Section { header }
                Section("Source") { HubblePeerDetails(peer: group.source) }
                Section("Destination") { HubblePeerDetails(peer: group.destination) }
                if !group.deniedBy.isEmpty {
                    policySection(group.deniedBy, title: Text("Denied by"),
                                  footer: Text("Explicit deny rules: they win over any allow rule."))
                }
                if !group.isolating.isEmpty {
                    policySection(group.isolating, title: Text("Isolated by"),
                                  footer: Text("These policies put the pod in default-deny: only what they allow passes."))
                }
                if let hint = group.hint {
                    Section {
                        Label { Text(verbatim: hint.text) } icon: { Image(systemName: "lightbulb").foregroundStyle(.yellow) }
                            .font(.callout)
                    }
                }
                Section {
                    Button {
                        UIPasteboard.general.string = group.sample.json
                        copied += 1
                    } label: {
                        Label("Copy sample flow as JSON", systemImage: "doc.on.doc")
                    }
                }
            }
            .themedBackground()
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .confirmationAction) { Button("Done") { dismiss() } }
            }
            .sheet(item: $opened) { NetPolicyDetailSheet(policy: $0) }
            .messageAlert($message)
            .sensoryFeedback(.success, trigger: copied)
        }
        .presentationDetents([.medium, .large])
        .presentationDragIndicator(.visible)
    }

    private var header: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text(verbatim: "\(group.source.label) → \(group.destination.label)\(group.endpointSuffix)")
                .font(.headline.monospaced())
                .textSelection(.enabled)
            HStack(spacing: 6) {
                StatusPill(label: group.verdict.label, color: group.verdict.color)
                if let direction = group.direction {
                    InfoChip(text: direction.label)
                }
                InfoChip(text: "×\(group.count)", monospaced: true)
            }
            Text(verbatim: group.reason.text).font(.callout)
            Text("Last seen \(relativeTime(group.lastSeen))")
                .font(.caption)
                .foregroundStyle(.secondary)
            if !group.nodes.isEmpty {
                Text(verbatim: group.nodes.joined(separator: ", ")).font(.caption.monospaced()).foregroundStyle(.secondary)
            }
        }
    }

    private func policySection(_ refs: [NetPolicyRef], title: Text, footer: Text) -> some View {
        Section {
            ForEach(refs, id: \.self) { ref in
                Button { Task { await open(ref) } } label: {
                    HStack(spacing: 6) {
                        NetPolicyKindBadge(kind: NetPolicyKind(wire: ref.kind))
                        Text(verbatim: ref.path).font(.callout.monospaced()).foregroundStyle(.primary).lineLimit(1)
                        Spacer()
                        if loading == ref { ProgressView() } else { Image(systemName: "chevron.right").foregroundStyle(.tertiary) }
                    }
                }
                .disabled(loading != nil)
            }
        } header: {
            title
        } footer: {
            footer
        }
    }

    /// Reads the policies once, then opens the one named.
    private func open(_ ref: NetPolicyRef) async {
        guard let client = model.client, loading == nil else { return }
        loading = ref
        defer { loading = nil }
        do {
            let report: NetPolicyReport
            if let policies {
                report = policies
            } else {
                report = try await client.networkPolicies()
                policies = report
            }
            if let policy = report.policy(ref) {
                opened = policy
            } else {
                message = String(localized: "\(ref.path) is no longer in the cluster")
            }
        } catch {
            message = error.localizedDescription
        }
    }
}

/// What a flow says about one side: namespace, pod, workload, IP, DNS names, identity.
private struct HubblePeerDetails: View {
    let peer: HubblePeer

    var body: some View {
        row("Namespace", peer.namespace)
        row("Pod", peer.pod)
        row("Workload", peer.workload)
        row("IP address", peer.ip)
        row("DNS names", peer.names.joined(separator: ", "))
        row("Reserved identity", peer.reserved)
        if peer.identity > 0 {
            row("Security identity", String(peer.identity))
        }
    }

    @ViewBuilder
    private func row(_ title: LocalizedStringKey, _ value: String) -> some View {
        if !value.isEmpty {
            LabeledContent(title) {
                Text(verbatim: value).font(.callout.monospaced()).textSelection(.enabled).multilineTextAlignment(.trailing)
            }
        }
    }
}
