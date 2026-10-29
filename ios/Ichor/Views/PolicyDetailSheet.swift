import SwiftUI
import IchorCore

/// One Kyverno policy or Gatekeeper constraint: what its mode does, whether it is ready, its rules,
/// then the resources it fails, each of a kind the browser knows opening its object screen.
struct PolicyDetailSheet: View {
    let policy: KyvernoPolicy
    /// The policy changed (its mode was switched): read the list again.
    var onChanged: () -> Void = {}

    @Environment(\.dismiss) private var dismiss
    @Environment(AppModel.self) private var model
    @State private var denial: KubeAccess?
    @State private var confirmAudit = false
    @State private var confirmEnforce = false
    @State private var switching = false
    @State private var switchError: String?

    // Explicit: the private @State makes the memberwise init private.
    init(policy: KyvernoPolicy, onChanged: @escaping () -> Void = {}) {
        self.policy = policy
        self.onChanged = onChanged
    }

    var body: some View {
        NavigationStack {
            List {
                Section { header }
                if !policy.switchTargets.isEmpty {
                    switchSection
                }
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
                        ForEach(policy.ruleNames, id: \.self) {
                            Text(verbatim: $0).font(.caption.monospaced()).foregroundStyle(.secondary)
                        }
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
        .task(id: policy.id) { await askAccess() }
        .sheet(isPresented: $confirmAudit) {
            HostnameConfirmationSheet(
                title: String(localized: "Switch to Audit"),
                message: switchMessage(
                    String(
                        localized:
                            "Pods that break this policy will be admitted and only reported until you switch it back.")),
                hostname: policy.name,
                actionTitle: String(localized: "Switch to Audit")
            ) {
                confirmAudit = false
                Task { await switchTo(.audit) }
            }
        }
        .confirmationDialog(
            String(localized: "Switch to Enforce"), isPresented: $confirmEnforce, titleVisibility: .visible
        ) {
            Button("Switch to Enforce") { Task { await switchTo(.enforce) } }
        } message: {
            Text(verbatim: switchMessage(String(localized: "Requests that break this policy will be refused again.")))
        }
    }

    /// The Audit/Enforce switch, disabled with the reason when the credentials may not patch it.
    private var switchSection: some View {
        Section {
            ForEach(policy.switchTargets, id: \.self) { target in
                Button {
                    if target == .audit { confirmAudit = true } else { confirmEnforce = true }
                } label: {
                    if target == .audit { Text("Switch to Audit") } else { Text("Switch to Enforce") }
                }
                .disabled(switching || denial != nil)
            }
            if let denial { KubeDeniedLine(text: denial.deniedText) }
            if let switchError { ErrorOrNoticeText(message: switchError) }
        }
    }

    /// The confirmation's body, warning when the policy's rules mix modes (they all take the new
    /// one, and switching back does not restore the mix) and when a GitOps tool would revert it.
    private func switchMessage(_ body: String) -> String {
        let mixed =
            policy.action == .mixed
            ? String(
                localized:
                    "Its rules mix Enforce and Audit: every rule takes this mode, and switching back later does not restore the mix."
            )
            : ""
        return [body, mixed, policy.gitopsTool.map(PolicyText.switchGitOps) ?? ""].filter { !$0.isEmpty }.joined(
            separator: "\n\n")
    }

    private func askAccess() async {
        guard let client = model.client,
            let access = try? await client.kubeCan(
                verb: "patch", group: "kyverno.io", resource: policy.kyvernoResource,
                namespace: policy.namespace, name: policy.name)
        else { return }
        denial = access.isDenied ? access : nil
    }

    private func switchTo(_ action: PolicyAction) async {
        guard let client = model.client else { return }
        switching = true
        switchError = nil
        defer { switching = false }
        do {
            try await client.setPolicyAction(namespace: policy.namespace, name: policy.name, action: action)
            onChanged()
            dismiss()
        } catch {
            switchError = PolicyText.switchFailed(policy.name, error.localizedDescription)
        }
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
                    + Text(
                        Date(timeIntervalSince1970: TimeInterval(policy.auditAt)),
                        format: .relative(presentation: .named)))
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
            Text(
                verbatim: [
                    failure.kind, [failure.namespace, failure.name].filter { !$0.isEmpty }.joined(separator: "/"),
                ]
                .filter { !$0.isEmpty }
                .joined(separator: " ")
            )
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
