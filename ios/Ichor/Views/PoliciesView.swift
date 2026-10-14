import SwiftUI
import IchorCore

/// Kyverno's admission policies (os:admin): whether the engine's controllers and webhook are up,
/// then every policy (not ready first, then Enforce before Audit) with its findings; one opens
/// its rules and the resources it fails.
struct PoliciesView: View {
    @Environment(AppModel.self) private var model
    @State private var state: LoadState<PolicyReport> = .loading
    @State private var namespace: String?
    @State private var query = ""
    @State private var selected: KyvernoPolicy?

    var body: some View {
        LoadStateView(state: state, retry: load) { report in
            if report.installed {
                list(report)
            } else {
                notInstalled(report)
            }
        }
        .task { await load() }
        // A new API address (set on the Kubernetes screen): read again through it.
        .id(model.client?.kubeServer)
        .searchable(text: $query, prompt: Text("Name, category or rule"))
        .navigationTitle("Policies")
        .navigationBarTitleDisplayMode(.inline)
        .sheet(item: $selected) { PolicyDetailSheet(policy: $0) }
    }

    private func list(_ report: PolicyReport) -> some View {
        let namespaces = report.namespaceNames
        let chosen = namespace.flatMap { namespaces.contains($0) ? $0 : nil }
        let rows = report.filtered(namespace: chosen, query: query)
        return List {
            PolicyEngineSection(report: report)
            if !namespaces.isEmpty {
                Section {
                    NamespacePicker(namespaces: namespaces, namespace: $namespace)
                    if let totals = report.namespaces.first(where: { $0.namespace == chosen }) {
                        PolicyCounts(fail: totals.fail, warn: totals.warn, pass: totals.pass)
                    }
                }
            }
            Section {
                ForEach(rows) { policy in
                    Button { selected = policy } label: { PolicyRow(policy: policy, namespace: chosen) }
                }
            } footer: {
                if report.reportsTruncated {
                    Text(PolicyText.reportsTruncated(String(report.reportsRead)))
                }
            }
        }
        .overlay {
            if report.policies.isEmpty {
                ContentUnavailableView("No Kyverno policies", systemImage: "checkmark.shield",
                                       description: Text("Kyverno runs, but no policy is defined."))
            } else if rows.isEmpty && !query.isEmpty {
                ContentUnavailableView.search(text: query)
            }
        }
        .refreshable { await load() }
        .themedBackground()
    }

    /// No Kyverno here: what it would do, and where to read about it; not an error.
    private func notInstalled(_ report: PolicyReport) -> some View {
        ContentUnavailableView {
            Label("Kyverno is not installed on this cluster", systemImage: "checkmark.shield")
        } description: {
            Text("Kyverno admits or refuses workloads by policy and reports what breaks the rules.")
        } actions: {
            Link(destination: URL(string: "https://kyverno.io")!) { Text(verbatim: "kyverno.io") }
            if !report.error.isEmpty {
                ErrorOrNoticeText(message: PolicyText.readError(report.error))
            }
        }
        .themedBackground()
    }

    private func load() async {
        guard let client = model.client else { return }
        state = state.refreshed(with: await .from { try await client.policies() })
    }
}

/// The controllers as ready/desired chips, what the webhook does while Kyverno is down, and the reads that failed.
private struct PolicyEngineSection: View {
    let report: PolicyReport

    var body: some View {
        let engine = report.engine
        Section {
            if !engine.controllers.isEmpty {
                ChipFlow(spacing: 6) {
                    ForEach(engine.controllers) { c in
                        InfoChip(text: "\(c.name) \(c.ready)/\(c.desired)", monospaced: true, color: c.healthy ? .statusOK : .statusBad)
                            .accessibilityLabel(Text(PolicyText.controllerReady(c.name, String(c.ready), String(c.desired))))
                    }
                }
            }
            webhookLine(engine.webhook)
            if !report.reportsError.isEmpty {
                ErrorOrNoticeText(message: PolicyText.reportsUnavailable(report.reportsError))
            }
            if !report.error.isEmpty {
                ErrorOrNoticeText(message: PolicyText.readError(report.error))
            }
        } header: {
            Text("Kyverno engine")
        } footer: {
            Text(verbatim: [engine.namespace, report.version, PolicyText.reportsRead(String(report.reportsRead))]
                .filter { !$0.isEmpty }
                .joined(separator: "  ·  "))
        }
    }

    /// Fail refuses requests while Kyverno is down (an outage risk), Ignore lets them through unchecked.
    @ViewBuilder
    private func webhookLine(_ webhook: PolicyWebhook) -> some View {
        if !webhook.present {
            Text("No admission webhook: policies are not applied to new requests").font(.footnote).foregroundStyle(Color.statusBad)
        } else {
            switch webhook.failurePolicy {
            case .fail: Text("Fail: requests are refused while Kyverno is down").font(.footnote).foregroundStyle(Color.statusWarn)
            case .ignore: Text("Ignore: policies are skipped while Kyverno is down").font(.footnote).foregroundStyle(.secondary)
            case .unknown: EmptyView()
            }
        }
    }
}

/// Name and mode, its scope, not-ready reason and counts (in `namespace` when one is picked).
private struct PolicyRow: View {
    let policy: KyvernoPolicy
    let namespace: String?

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack(spacing: 6) {
                Text(verbatim: policy.name)
                    .font(.callout.monospaced())
                    .lineLimit(1)
                    .truncationMode(.middle)
                    .foregroundStyle(.primary)
                Spacer()
                PolicyActionBadge(action: policy.action)
            }
            policy.scopeText
                .font(.caption)
                .foregroundStyle(.secondary)
                .lineLimit(1)
            if !policy.ready {
                HStack(spacing: 6) {
                    PolicyNotReadyBadge(reason: policy.readyReason)
                    if !policy.readyReason.isEmpty {
                        Text(verbatim: policy.readyReason).font(.caption).foregroundStyle(.secondary).lineLimit(1)
                    }
                }
            }
            let counts = policy.counts(namespace: namespace)
            PolicyCounts(fail: counts.fail, warn: counts.warn, pass: counts.pass)
        }
    }
}
