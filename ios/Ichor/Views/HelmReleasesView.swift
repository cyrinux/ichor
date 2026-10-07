import SwiftUI
import UniformTypeIdentifiers
import IchorCore

/// Helm releases, read-only, as `helm list` shows them: the latest revision of each, failed and
/// pending ones first, in the Kubernetes screen's namespace scope. Read from the release
/// Secrets by the Go core (no Helm in the app); a release opens its values, notes, manifest
/// and history.
struct HelmReleasesView: View {
    @Environment(AppModel.self) private var model
    @State private var scope = KubeBrowserScope()
    @State private var state: LoadState<[HelmReleaseSummary]> = .loading
    @State private var query = ""

    /// What decides the list: the scope, and the cluster, API address and screenshot mode.
    private struct Trigger: Hashable {
        let scope: KubeScope
        let ready: Bool
        let source: String
    }

    var body: some View {
        let control = scope.control(model: model)
        VStack(spacing: 0) {
            KubeScopeBar(control: control, loaded: loadedNamespaces)
                .background(.bar)
            Divider()
            if !control.ready {
                ContentUnavailableView {
                    Label("Namespace", systemImage: "square.dashed")
                } description: {
                    Text("Type the namespace to list above.")
                }
                .frame(maxHeight: .infinity)
            } else {
                LoadStateView(state: state, retry: { await load(control.scope) }) { releases in
                    list(releases, showNamespace: control.scope.namespace == nil, scope: control.scope)
                }
            }
        }
        .searchable(text: $query, prompt: Text("Name, namespace or chart"))
        .autocorrectionDisabled()
        .textInputAutocapitalization(.never)
        .navigationTitle(Text("Helm releases"))
        .navigationBarTitleDisplayMode(.inline)
        .task(id: kubeNamespacesKey(model)) { await scope.loadNamespaces(model: model) }
        .task(id: Trigger(scope: control.scope, ready: control.ready, source: kubeNamespacesKey(model))) {
            if control.ready { await load(control.scope) }
        }
    }

    private func list(_ releases: [HelmReleaseSummary], showNamespace: Bool, scope: KubeScope) -> some View {
        let shown = filterHelmReleases(sortHelmReleases(releases), query: query)
        return List {
            ForEach(shown) { release in
                NavigationLink { HelmReleaseView(namespace: release.namespace, name: release.name) } label: {
                    HelmReleaseRow(release: release, showNamespace: showNamespace)
                }
            }
        }
        .emptyOverlay(shown.isEmpty, query: query) {
            ContentUnavailableView("No Helm releases", systemImage: "shippingbox")
        }
        .refreshable { await load(scope) }
        .themedBackground()
    }

    private var loadedNamespaces: [String] {
        guard case .loaded(let releases, _, _) = state else { return [] }
        return Array(Set(releases.map(\.namespace))).sorted()
    }

    private func load(_ scope: KubeScope) async {
        guard let client = model.client else { return }
        let key = kubeNamespacesKey(model)
        let fetched: LoadState<[HelmReleaseSummary]> = await .from { try await client.helmReleases(namespace: scope.namespace).releases }
        guard key == kubeNamespacesKey(model), !Task.isCancelled else { return }
        state = state.refreshed(with: fetched)
    }
}

/// A release: name, status pill, chart and app version, revision and when it was last deployed.
private struct HelmReleaseRow: View {
    let release: HelmReleaseSummary
    let showNamespace: Bool

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack(alignment: .firstTextBaseline, spacing: 6) {
                Text(verbatim: release.name)
                    .font(.callout.monospaced().weight(.medium))
                    .lineLimit(1)
                    .truncationMode(.middle)
                Spacer(minLength: 4)
                HelmStatusPill(status: release.status)
            }
            if showNamespace {
                Text(verbatim: release.namespace).font(.caption).foregroundStyle(.secondary)
            }
            Text(verbatim: details).font(.caption).foregroundStyle(.secondary).lineLimit(2)
        }
        .accessibilityElement(children: .combine)
    }

    private var details: String {
        var parts = [release.chartLabel]
        if !release.appVersion.isEmpty { parts.append(String(localized: "app \(release.appVersion)")) }
        parts.append(String(localized: "revision \(release.revision)"))
        if release.updated > 0 { parts.append(relativeTime(release.updated * 1000)) }
        return parts.filter { !$0.isEmpty }.joined(separator: " · ")
    }
}

/// "deployed" in green, "failed" in red, "pending-…" in amber.
private struct HelmStatusPill: View {
    let status: String

    var body: some View {
        StatusPill(label: status.isEmpty ? String(localized: "unknown") : status,
                   color: kubeStatusTone(status).color ?? .secondary)
    }
}

/// One release: summary, history, and its values, notes and manifest as text.
struct HelmReleaseView: View {
    let namespace: String
    let name: String

    // Explicit: the private @State makes the memberwise init private.
    init(namespace: String, name: String) {
        self.namespace = namespace
        self.name = name
    }

    private enum Tab: Hashable { case summary, values, notes, manifest }

    @Environment(AppModel.self) private var model
    @State private var state: LoadState<HelmReleaseDetail> = .loading
    @State private var tab = Tab.summary
    @State private var query = ""
    @State private var message: String?
    /// The revision whose rollback sheet is open.
    @State private var rollingBack: HelmRevision?
    /// Set by the sheet on success, announced once it has gone.
    @State private var rolledBackTo: Int?

    var body: some View {
        VStack(spacing: 0) {
            Picker(selection: $tab) {
                Text("Summary").tag(Tab.summary)
                Text("Values").tag(Tab.values)
                Text("Notes").tag(Tab.notes)
                Text("Manifest").tag(Tab.manifest)
            } label: {
                EmptyView()
            }
            .pickerStyle(.segmented)
            .padding(.horizontal)
            .padding(.vertical, 8)
            LoadStateView(state: state, retry: load) { detail in content(detail) }
        }
        .searchable(text: $query, prompt: Text("Filter lines"))
        .autocorrectionDisabled()
        .textInputAutocapitalization(.never)
        .navigationTitle(Text(verbatim: name))
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            if case .loaded(let detail, _, _) = state, let text = bodyText(of: tab, in: detail), !text.isEmpty {
                ToolbarItem(placement: .primaryAction) {
                    Menu {
                        Button { copy(text) } label: { Label("Copy", systemImage: "doc.on.doc") }
                        ShareLink(item: text) { Label("Share", systemImage: "square.and.arrow.up") }
                    } label: {
                        Image(systemName: "square.and.arrow.up").accessibilityLabel(Text("Share"))
                    }
                }
            }
        }
        .messageAlert($message)
        .sheet(item: $rollingBack, onDismiss: rollbackDismissed) { revision in
            HelmRollbackSheet(namespace: namespace, name: name, revision: revision.revision,
                              rolledBack: { rolledBackTo = revision.revision }, reload: load)
        }
        .task(id: kubeNamespacesKey(model)) { await load() }
    }

    @ViewBuilder private func content(_ detail: HelmReleaseDetail) -> some View {
        switch tab {
        case .summary:
            summary(detail)
        case .values, .notes, .manifest:
            let text = bodyText(of: tab, in: detail) ?? ""
            if text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
                ContentUnavailableView(emptyTitle, systemImage: "doc.text")
            } else {
                ConfigYamlLines(yaml: text, query: query, refresh: load)
            }
        }
    }

    private var emptyTitle: LocalizedStringKey {
        switch tab {
        case .values: "No values set: the chart's defaults apply."
        case .notes: "No notes."
        case .summary, .manifest: "No manifest."
        }
    }

    private func summary(_ detail: HelmReleaseDetail) -> some View {
        let release = detail.summary
        return List {
            Section {
                LabeledContent("Status") { HelmStatusPill(status: release.status) }
                LabeledContent("Namespace") { Text(verbatim: release.namespace).monospaced() }
                LabeledContent("Chart") { Text(verbatim: release.chartLabel).monospaced() }
                if !release.appVersion.isEmpty {
                    LabeledContent("App version") { Text(verbatim: release.appVersion).monospaced() }
                }
                LabeledContent("Revision") { Text(verbatim: "\(release.revision)").monospacedDigit() }
                if release.updated > 0 {
                    LabeledContent("Updated") { Text(verbatim: relativeTime(release.updated * 1000)) }
                }
                if !detail.description.isEmpty {
                    Text(verbatim: detail.description).font(.callout).foregroundStyle(.secondary)
                }
            }
            if !detail.history.isEmpty {
                Section {
                    ForEach(detail.history) { revision in
                        let current = revision.revision == release.revision
                        HelmRevisionRow(revision: revision, current: current,
                                        rollBack: current ? nil : { rollingBack = revision })
                    }
                } header: {
                    Text("History")
                }
            }
            Section {
                Text("Upgrades are done with Helm or your GitOps tool. From the history, the release can be rolled back to an older revision.")
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }
        }
        .refreshable { await load() }
        .themedBackground()
    }

    private func bodyText(of tab: Tab, in detail: HelmReleaseDetail) -> String? {
        switch tab {
        case .summary: return nil
        case .values: return detail.values
        case .notes: return detail.notes
        case .manifest: return detail.manifest
        }
    }

    private func load() async {
        guard let client = model.client else { return }
        let key = kubeNamespacesKey(model)
        let fetched: LoadState<HelmReleaseDetail> = await .from { try await client.helmRelease(namespace: namespace, name: name) }
        guard key == kubeNamespacesKey(model) else { return }
        state = state.refreshed(with: fetched)
    }

    /// After the rollback sheet: says how it ended (the alert waits for the sheet to go) and
    /// reads the release again either way.
    private func rollbackDismissed() {
        if let revision = rolledBackTo {
            message = String(localized: "Rolled back to revision \(String(revision)).")
            rolledBackTo = nil
        }
        Task { await load() }
    }

    /// Device-only pasteboard: values can hold secrets.
    private func copy(_ text: String) {
        UIPasteboard.general.setItems([[UTType.utf8PlainText.identifier: text]], options: [.localOnly: true])
        message = String(localized: "Copied.")
    }
}

/// A revision of the history: number, status, when, and Helm's description; an older one can
/// be rolled back to.
private struct HelmRevisionRow: View {
    let revision: HelmRevision
    let current: Bool
    /// nil for the current revision.
    let rollBack: (() -> Void)?

    var body: some View {
        HStack(spacing: 8) {
            details
            if let rollBack {
                Button("Roll back", action: rollBack)
                    .buttonStyle(.bordered)
                    .controlSize(.small)
            }
        }
    }

    private var details: some View {
        VStack(alignment: .leading, spacing: 3) {
            HStack(spacing: 8) {
                Text(verbatim: "#\(revision.revision)")
                    .font(.callout.monospacedDigit().weight(current ? .semibold : .regular))
                Text(verbatim: revision.status)
                    .font(.caption)
                    .foregroundStyle(revision.tone.color ?? .secondary)
                Spacer()
                if revision.updated > 0 {
                    Text(verbatim: relativeTime(revision.updated * 1000)).font(.caption).foregroundStyle(.secondary)
                }
            }
            if !revision.description.isEmpty {
                Text(verbatim: revision.description).font(.caption).foregroundStyle(.secondary).lineLimit(3)
            }
        }
        .accessibilityElement(children: .combine)
    }
}
