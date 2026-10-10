import SwiftUI
import IchorCore

/// What the Prometheus of the Metrics screen says about itself: the scrape targets that are
/// down, grouped by scrape pool, its rule groups troubled first, and the prometheus-operator's
/// Prometheus and Alertmanager objects (the Operator tab only where the operator runs). Rows
/// open the pod, Service, monitor or PrometheusRule in the Kubernetes browser. `source` nil
/// (from the checkup): the Metrics screen's saved source, else the first one found.
struct PromMonitoringView: View {
    enum Tab: Hashable { case targets, rules, operatorStatus }

    @Environment(AppModel.self) private var model
    let source: PromSource?

    @State private var tab = Tab.targets
    @State private var shown: PromSource?
    @State private var searching = true
    @State private var targets: LoadState<PromTargets> = .loading
    @State private var rules: LoadState<PromRules> = .loading
    @State private var status: PromOperatorStatus?
    /// The cluster `shown` and the answers are of.
    @State private var loadedFor = ""

    private var fingerprint: String { model.activeSummary?.fingerprint ?? "" }

    var body: some View {
        VStack(spacing: 0) {
            Picker(selection: $tab) {
                Text(verbatim: PromMonitoringText.targets).tag(Tab.targets)
                Text(verbatim: PromMonitoringText.rules).tag(Tab.rules)
                if status?.installed == true { Text("Operator").tag(Tab.operatorStatus) }
            } label: {
                Text("Monitoring")
            }
            .pickerStyle(.segmented)
            .padding(.horizontal)
            .padding(.vertical, 8)
            content
        }
        .navigationTitle("Monitoring")
        .navigationBarTitleDisplayMode(.inline)
        .task(id: fingerprint) { await load() }
        .themedBackground()
    }

    @ViewBuilder private var content: some View {
        if tab == .operatorStatus, let status, status.installed {
            PromOperatorList(status: status).refreshable { await load() }
        } else if searching && shown == nil {
            SkeletonView()
        } else if let shown {
            switch tab {
            case .targets, .operatorStatus:
                LoadStateView(state: targets, retry: load) { targets in
                    PromTargetsList(targets: targets, source: shown).refreshable { await load() }
                }
            case .rules:
                LoadStateView(state: rules, retry: load) { rules in
                    PromRulesList(rules: rules).refreshable { await load() }
                }
            }
        } else {
            ContentUnavailableView {
                Label("No Prometheus, Mimir, Thanos or VictoriaMetrics found in the cluster", systemImage: "chart.xyaxis.line")
            } description: {
                Text("Pick a Service by hand, or set the URL of a server the phone can reach.")
            } actions: {
                NavigationLink("Set up a source") { MetricsView() }
            }
        }
    }

    /// The three reads at once; an answer for a cluster since switched away from is dropped.
    private func load() async {
        guard let client = model.client else { return }
        let key = fingerprint
        if loadedFor != key {
            shown = nil
            status = nil
            searching = true
            targets = .loading
            rules = .loading
            loadedFor = key
        }
        let operatorRead = Task { try await client.promOperatorStatus() }
        var picked = shown ?? source ?? MetricsStore.read(key).source
        if picked == nil { picked = try? await client.promDiscover().first }
        guard key == fingerprint else { return }
        shown = picked
        searching = false
        if let picked {
            let targetsRead = Task { try await client.promTargets(picked) }
            let rulesRead = Task { try await client.promRules(picked) }
            let loadedTargets: LoadState<PromTargets> = await .from { try await targetsRead.value }
            let loadedRules: LoadState<PromRules> = await .from { try await rulesRead.value }
            guard key == fingerprint else { return }
            targets = targets.refreshed(with: loadedTargets)
            rules = rules.refreshed(with: loadedRules)
        }
        let loadedStatus = try? await operatorRead.value
        guard key == fingerprint else { return }
        // A failed read keeps the tab it had; one that says "not installed" removes it.
        if let loadedStatus { status = loadedStatus }
        if status?.installed != true, tab == .operatorStatus { tab = .targets }
    }
}

/// A row that opens `link` in the Kubernetes browser, or just shows `label` when there is none.
struct KubeObjectLinkRow<Label: View>: View {
    let link: KubeObjectLink?
    @ViewBuilder let label: () -> Label

    var body: some View {
        if let link {
            NavigationLink {
                KubeObjectView(resource: link.resource, namespace: link.namespace, name: link.name)
            } label: {
                label()
            }
        } else {
            label()
        }
    }
}

/// "ServiceMonitor demo/web": the kind and the object, as a link row.
struct KubeObjectLinkLabel: View {
    let link: KubeObjectLink

    var body: some View {
        Label {
            Text(verbatim: "\(link.resource.kind) \(link.namespace)/\(link.name)")
                .font(.callout.monospaced())
                .lineLimit(1)
                .truncationMode(.middle)
        } icon: {
            Image(systemName: "doc.text.magnifyingglass")
        }
    }
}

/// How long ago `millis` (unix ms) was, nil when never.
func promAgo(_ millis: Int64, now: Date = Date()) -> String? {
    guard millis > 0 else { return nil }
    return localizedDuration(max(0, Int64(now.timeIntervalSince1970) - millis / 1000))
}
