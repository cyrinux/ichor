import SwiftUI
import IchorCore

/// One Kyverno policy or Gatekeeper constraint: what its mode does, whether it is ready, its rules,
/// then the resources it fails, each of a kind the browser knows opening its object screen.
struct PolicyDetailSheet: View {
    let policy: KyvernoPolicy

    @Environment(\.dismiss) private var dismiss

    var body: some View {
        NavigationStack {
            List {
                Section { header }
                if !policy.ready {
                    Section {
                        HStack(spacing: 6) {
                            PolicyNotReadyBadge(reason: policy.readyReason)
                            if !policy.readyReason.isEmpty {
                                Text(verbatim: policy.readyReason).font(.callout).foregroundStyle(Color.statusBad)
                            }
                        }
                        if !policy.readyMessage.isEmpty {
                            Text(verbatim: policy.readyMessage).font(.footnote).foregroundStyle(.secondary)
                        }
                    }
                }
                // A Gatekeeper constraint has no rules of its own: its template's Rego is the rule.
                if policy.engine == .gatekeeper {
                    Section { PolicyCounts(fail: policy.fail, warn: policy.warn, pass: policy.pass) }
                } else {
                    Section("Rules") {
                        Text(verbatim: policy.rules.text).font(.callout.monospaced())
                        ForEach(policy.ruleNames, id: \.self) { Text(verbatim: $0).font(.caption.monospaced()).foregroundStyle(.secondary) }
                        PolicyCounts(fail: policy.fail, warn: policy.warn, pass: policy.pass)
                    }
                }
                failingSection
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
                Text(verbatim: policy.name).font(.headline.monospaced()).textSelection(.enabled)
                Spacer()
                PolicyActionBadge(action: policy.action)
            }
            policy.scopeText.font(.subheadline).foregroundStyle(.secondary)
            if let meaning = policy.action.meaning {
                meaning.font(.callout)
            }
            if !policy.description.isEmpty {
                Text(verbatim: policy.description).font(.callout)
            }
            if !policy.category.isEmpty {
                LabeledContent("Category") { Text(verbatim: policy.category) }
            }
            if !policy.severity.isEmpty {
                LabeledContent("Severity") { Text(verbatim: policy.severity) }
            }
            if policy.background {
                Text("Also checks the resources already in the cluster").font(.footnote).foregroundStyle(.secondary)
            }
            if policy.auditAt > 0 {
                (Text("Last audit") + Text(verbatim: " ")
                    + Text(Date(timeIntervalSince1970: TimeInterval(policy.auditAt)), format: .relative(presentation: .named)))
                    .font(.footnote).foregroundStyle(.secondary)
            }
        }
    }

    private var failingSection: some View {
        Section {
            if policy.failing.isEmpty && policy.fail == 0 {
                Text("Nothing fails this policy").foregroundStyle(.secondary)
            }
            ForEach(policy.failing) { failure in
                if let resource = failure.resource {
                    NavigationLink {
                        KubeObjectView(resource: resource, namespace: failure.objectNamespace, name: failure.name)
                    } label: {
                        PolicyFailureRow(failure: failure)
                    }
                } else {
                    PolicyFailureRow(failure: failure)
                }
            }
        } header: {
            Text("Failing resources")
        } footer: {
            if policy.fail > policy.failing.count {
                if policy.failingCapped {
                    Text(PolicyText.failingCapped(String(policy.failing.count), String(policy.fail)))
                } else {
                    Text(PolicyText.failingShown(String(policy.failing.count), String(policy.fail)))
                }
            }
        }
    }
}

/// Kind namespace/name, the rule and when, then its message.
private struct PolicyFailureRow: View {
    let failure: PolicyFailure

    var body: some View {
        VStack(alignment: .leading, spacing: 3) {
            Text(verbatim: [failure.kind, [failure.namespace, failure.name].filter { !$0.isEmpty }.joined(separator: "/")]
                .filter { !$0.isEmpty }
                .joined(separator: " "))
                .font(.callout.monospaced())
            HStack(spacing: 6) {
                if !failure.rule.isEmpty {
                    Text(PolicyText.ruleOf(failure.rule))
                }
                if failure.at > 0 {
                    Text(Date(timeIntervalSince1970: TimeInterval(failure.at)), format: .relative(presentation: .named))
                }
            }
            .font(.caption)
            .foregroundStyle(.secondary)
            if !failure.message.isEmpty {
                Text(verbatim: failure.message).font(.caption)
            }
        }
    }
}
