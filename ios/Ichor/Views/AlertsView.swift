import SwiftUI
import IchorCore

/// The cluster's Alertmanager on screen: its alerts grouped by alertname, the worst first, with
/// filters (states, severities, search), and its silences, which can be expired. The source is
/// found in the cluster on first open (the first candidate) or set by hand, like the metrics
/// source but kept apart. Reads again every minute while on screen, and on a pull.
struct AlertsView: View {
    /// The navigation path: an alert's pod, namespace or node opens over this screen.
    @Binding var path: [Route]

    // Explicit: the private @State properties could make the memberwise init private.
    init(path: Binding<[Route]>) {
        _path = path
    }

    enum Tab: Hashable { case alerts, silences }

    /// What the lists show: a change reloads them.
    private struct Shown: Equatable { let source: PromSource?; let filter: AMStateFilter; let withExpired: Bool }

    @Environment(AppModel.self) private var model
    @State private var tab = Tab.alerts
    @State private var source: PromSource?
    @State private var resolved = false
    @State private var state: LoadState<AMAlerts> = .loading
    @State private var silences: LoadState<[AMSilence]> = .loading
    @State private var filter = AMStateFilter()
    @State private var severities = Set(AMSeverity.allCases)
    @State private var query = ""
    @State private var withExpired = false
    /// The cluster's Talos nodes, for an alert's node link (none on a cluster without Talos).
    @State private var nodes: [NodeOverview] = []
    @State private var selected: AMAlert?
    /// Where a link of the alert sheet goes, once the sheet is gone.
    @State private var pendingRoute: Route?
    @State private var expiring: AMSilence?
    @State private var busy: Set<String> = []
    @State private var message: String?
    @State private var succeeded = 0
    @State private var sourceOpen = false
    @State private var discovered: [PromSource]?
    @State private var discovering = false
    @State private var discoveryError: String?

    private var fingerprint: String { model.activeSummary?.fingerprint ?? "" }

    var body: some View {
        Group {
            if !resolved {
                SkeletonView()
            } else if source != nil {
                VStack(spacing: 0) {
                    switch tab {
                    case .alerts: alertsList
                    case .silences: silencesList
                    }
                }
                .safeAreaInset(edge: .top) { topBar }
            } else {
                noSource
            }
        }
        .searchable(text: $query, prompt: Text("Alert name, label or summary"))
        .navigationTitle("Alerts")
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItemGroup(placement: .primaryAction) {
                ShareLinkButton(target: .screen(.alerts))
                Button { sourceOpen = true } label: { Image(systemName: "slider.horizontal.3") }
                    .accessibilityLabel(Text("Alertmanager source"))
            }
        }
        .task(id: model.alertsKey) { await resolve() }
        .task(id: Shown(source: source, filter: filter, withExpired: withExpired)) {
            guard source != nil else { return }
            while !Task.isCancelled {
                await reload()
                try? await Task.sleep(for: .seconds(60))
            }
        }
        .sheet(item: $selected, onDismiss: {
            if let pendingRoute { path.append(pendingRoute) }
            pendingRoute = nil
        }) { alert in
            AlertDetailSheet(alert: alert, node: alertNode(alert.labels, in: nodes),
                             canOpenKube: model.allows(.workloads), silence: silence,
                             open: { route in
                                 pendingRoute = route
                                 selected = nil
                             },
                             silenced: {
                                 selected = nil
                                 succeeded += 1
                                 announce(String(localized: "Silenced"))
                                 Task { await reload() }
                             })
        }
        .sheet(isPresented: $sourceOpen) {
            SourceSheet(kind: .alertmanager, current: source, discovered: discovered, discovering: discovering,
                        discoveryError: discoveryError, discover: { await discover(autoSelect: false) }, test: test, save: setSource)
        }
        .confirmationDialog(String(localized: "Expire this silence?"), isPresented: $expiring.isPresent(),
                            titleVisibility: .visible, presenting: expiring) { silence in
            Button("Expire", role: .destructive) { Task { await expire(silence) } }
            Button("Cancel", role: .cancel) {}
        } message: { silence in
            Text(verbatim: silence.matchers.map(\.text).joined(separator: ", ")) + Text(verbatim: "\n\n") +
                Text("The alerts it mutes notify again.")
        }
        .messageAlert($message)
        .sensoryFeedback(.success, trigger: succeeded)
        .themedBackground()
        // Outermost, so the alert sheet reads it too: silence and expire go through the service
        // proxy of the Alertmanager's namespace; a URL source asks nothing (never gated).
        .loadsKubeActionAccess(namespace: proxyNamespace)
    }

    /// The namespace of a source reached through the service proxy, nil for a URL source.
    private var proxyNamespace: String? {
        guard let source, source.mode == PromSource.proxy else { return nil }
        return source.namespace
    }

    // MARK: - Lists

    private var topBar: some View {
        VStack(spacing: 8) {
            Picker(selection: $tab) {
                Text("Alerts").tag(Tab.alerts)
                Text("Silences").tag(Tab.silences)
            } label: {
                EmptyView()
            }
            .pickerStyle(.segmented)
            .padding(.horizontal)
            if tab == .alerts {
                ScrollView(.horizontal, showsIndicators: false) {
                    HStack(spacing: 8) {
                        stateMenu
                        ForEach(AMSeverity.allCases, id: \.self) { severity in
                            FilterChip(label: severity.label, count: severityCount(severity), selected: severities.contains(severity),
                                       dot: severity.color) {
                                withAnimation(.snappy) {
                                    if severities.contains(severity) { severities.remove(severity) } else { severities.insert(severity) }
                                }
                            }
                        }
                    }
                    .padding(.horizontal)
                }
            }
        }
        .padding(.bottom, 6)
        .background(.bar)
    }

    /// Which states Alertmanager is asked for: firing, silenced, inhibited.
    private var stateMenu: some View {
        Menu {
            Toggle("Firing", isOn: $filter.active)
            Toggle("Silenced", isOn: $filter.silenced)
            Toggle("Inhibited", isOn: $filter.inhibited)
        } label: {
            Label("States", systemImage: "line.3.horizontal.decrease.circle")
                .font(.subheadline.weight(.medium))
                .padding(.horizontal, 12)
                .padding(.vertical, 7)
                .background(Color(.tertiarySystemFill), in: Capsule())
                .frame(minHeight: 44)
        }
        .tint(.primary)
    }

    private func severityCount(_ severity: AMSeverity) -> Int {
        guard case .loaded(let alerts, _, _) = state else { return 0 }
        return alerts.alerts.filter { $0.severity == severity && $0.matches(query) }.count
    }

    private var alertsList: some View {
        LoadStateView(state: state, retry: reload) { alerts in
            let groups = alerts.filtered(severities: severities, query: query)
            List {
                if alerts.truncated {
                    Text("Showing the first 1000 of \(alerts.total) alerts.").font(.footnote).foregroundStyle(.secondary)
                }
                ForEach(groups) { group in
                    Section {
                        ForEach(group.alerts) { alert in
                            Button { selected = alert } label: { AlertRow(alert: alert) }
                                .foregroundStyle(.primary)
                        }
                    } header: {
                        HStack(spacing: 6) {
                            Image(systemName: group.severity.symbol).foregroundStyle(group.severity.color).accessibilityHidden(true)
                            Text(verbatim: group.alertname)
                            Spacer()
                            Text(verbatim: "\(group.count)").monospacedDigit()
                        }
                    }
                }
            }
            .emptyOverlay(groups.isEmpty, query: query) {
                ContentUnavailableView("No alerts", systemImage: "bell.slash",
                                       description: Text("Nothing in the states and severities picked."))
            }
            .refreshable { await reload() }
        }
    }

    private var silencesList: some View {
        LoadStateView(state: silences, retry: reload) { list in
            List {
                Section {
                    Toggle("Show expired", isOn: $withExpired)
                    KubeDeniedNote(.alertmanagerExpire)
                }
                ForEach(list.filter { silenceMatches($0) }) { silence in
                    SilenceRow(silence: silence, busy: busy.contains(silence.id))
                        .swipeActions {
                            if silence.expirable {
                                Button("Expire", role: .destructive) { expiring = silence }
                                    .kubeGated(.alertmanagerExpire)
                            }
                        }
                        .contextMenu {
                            if silence.expirable {
                                Button(role: .destructive) { expiring = silence } label: { Label("Expire", systemImage: "bell") }
                                    .kubeGated(.alertmanagerExpire)
                            }
                        }
                }
            }
            .emptyOverlay(list.filter { silenceMatches($0) }.isEmpty, query: query) {
                ContentUnavailableView("No silences", systemImage: "bell",
                                       description: Text("Silence an alert from its details."))
            }
            .refreshable { await reload() }
        }
    }

    private func silenceMatches(_ silence: AMSilence) -> Bool {
        let q = query.trimmingCharacters(in: .whitespaces).lowercased()
        guard !q.isEmpty else { return true }
        return silence.comment.lowercased().contains(q) || silence.matchers.contains { $0.text.lowercased().contains(q) }
    }

    private var noSource: some View {
        Group {
            if discovering {
                ProgressView("Looking for Alertmanager in the cluster…").frame(maxWidth: .infinity, maxHeight: .infinity)
            } else {
                ContentUnavailableView {
                    Label("No Alertmanager found in the cluster", systemImage: "bell.slash")
                } description: {
                    Text(discoveryError ?? String(localized: "Pick a Service by hand, or set the URL of an Alertmanager the phone can reach."))
                } actions: {
                    Button("Set up a source") { sourceOpen = true }
                    Button("Search the cluster again") { Task { await discover(autoSelect: true) } }
                }
            }
        }
    }

    // MARK: - Loading

    private func resolve() async {
        source = AlertmanagerStore.read(fingerprint).source
        // What the home card read, while this screen reads again.
        let known = AlertsStore.shared.alerts(for: model.alertsKey)
        state = known.map { LoadState<AMAlerts>.loaded($0, at: Date()) } ?? .loading
        silences = .loading
        nodes = []
        // The search starts below: no "none found" in between.
        if source == nil { discovering = true }
        resolved = true
        if source == nil { await discover(autoSelect: true) }
        // An alert's node link: only a Talos cluster has node screens.
        if !model.activeIsKube, let client = model.client {
            let key = model.alertsKey
            let found = (try? await client.overview().nodes) ?? []
            if key == model.alertsKey { nodes = found }
        }
    }

    private func discover(autoSelect: Bool) async {
        guard let client = model.client else {
            discovering = false
            return
        }
        discovering = true
        discoveryError = nil
        do {
            let found = try await client.alertmanagerDiscover()
            discovered = found
            if autoSelect, source == nil, let first = found.first { _ = await setSource(first) }
        } catch {
            discoveryError = error.localizedDescription
        }
        discovering = false
    }

    /// Checks `next` (Go) and saves it for the cluster. Nil when saved, else why not.
    private func setSource(_ next: PromSource) async -> String? {
        do {
            let checked = try await TalosClient.normalizePromSource(next)
            try AlertmanagerStore.save(AlertmanagerConfig(source: checked), for: fingerprint)
            AlertsStore.shared.sourceSet(key: model.alertsKey)
            state = .loading
            silences = .loading
            source = checked
            return nil
        } catch {
            return error.localizedDescription
        }
    }

    private func test(_ candidate: PromSource) async -> String? {
        guard let client = model.client else { return nil }
        do {
            let checked = try await TalosClient.normalizePromSource(candidate)
            _ = try await client.alertmanagerSilences(checked, withExpired: false)
            return nil
        } catch {
            return error.localizedDescription
        }
    }

    /// Both lists; an answer for another source, filter or cluster is dropped.
    private func reload() async {
        guard let client = model.client, let source else { return }
        let key = model.alertsKey
        let filter = filter
        let withExpired = withExpired
        let alerts: LoadState<AMAlerts> = await .from { try await client.alertmanagerAlerts(source, filter: filter) }
        let list: LoadState<[AMSilence]> = await .from { try await client.alertmanagerSilences(source, withExpired: withExpired) }
        guard key == model.alertsKey, source == self.source, filter == self.filter, withExpired == self.withExpired else { return }
        state = state.refreshed(with: alerts)
        silences = silences.refreshed(with: list)
        // Every state shown: what the home card counts too.
        if case .loaded(let read, _, _) = alerts, filter == AMStateFilter() { AlertsStore.shared.remember(read, key: key) }
    }

    // MARK: - Actions

    /// Silences `matchers` from the alert sheet; nil when done, else why not.
    private func silence(_ matchers: [AMMatcher], minutes: Int, comment: String) async -> String? {
        guard let client = model.client, let source else { return nil }
        do {
            _ = try await client.alertmanagerSilence(source, matchers: matchers, minutes: minutes, comment: comment)
            return nil
        } catch {
            return error.localizedDescription
        }
    }

    private func expire(_ silence: AMSilence) async {
        guard let client = model.client, let source else { return }
        busy.insert(silence.id)
        defer { busy.remove(silence.id) }
        var failure: String?
        do {
            try await client.alertmanagerExpire(source, id: silence.id)
        } catch {
            failure = error.localizedDescription
        }
        if recordActionOutcome(failure, message: &message, succeeded: &succeeded) { announce(String(localized: "Silence expired")) }
        await reload()
    }
}

/// An alert in its group: what it is about, its summary, since when, and its state when it is not
/// plainly firing.
private struct AlertRow: View {
    let alert: AMAlert

    var body: some View {
        VStack(alignment: .leading, spacing: 3) {
            HStack(spacing: 8) {
                Circle().fill(alert.suppressed ? Color.secondary : alert.severity.color).frame(width: 8, height: 8)
                    .accessibilityHidden(true)
                Text(verbatim: alert.subject.isEmpty ? alert.alertname : alert.subject).font(.body.weight(.medium)).lineLimit(1)
                Spacer()
                if alert.state != .active {
                    Text(alert.stateLabel).font(.caption).foregroundStyle(.secondary)
                }
            }
            if !alert.summary.isEmpty {
                Text(verbatim: alert.summary).font(.caption).foregroundStyle(.secondary).lineLimit(2)
            }
            if alert.startsAt > 0 {
                Text("Since \(relativeTime(alert.startsAt))").font(.caption2).foregroundStyle(.secondary)
            }
        }
        .accessibilityElement(children: .combine)
    }
}

/// A silence: its matchers, why, who, and when it ends (or starts, or ended).
private struct SilenceRow: View {
    let silence: AMSilence
    let busy: Bool

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack {
                Text(stateLabel).font(.caption.weight(.semibold)).foregroundStyle(stateColor)
                Spacer()
                if busy { ProgressView() }
                Text(timeLabel).font(.caption).foregroundStyle(.secondary)
            }
            Text(verbatim: silence.matchers.map(\.text).joined(separator: ", "))
                .font(.caption.monospaced())
                .lineLimit(3)
            if !silence.comment.isEmpty { Text(verbatim: silence.comment).font(.callout) }
            if !silence.createdBy.isEmpty {
                Text("By \(silence.createdBy)").font(.caption2).foregroundStyle(.secondary)
            }
        }
        .accessibilityElement(children: .combine)
    }

    private var stateLabel: String {
        switch silence.state {
        case .active: String(localized: "In effect")
        case .pending: String(localized: "Pending")
        case .expired: String(localized: "Expired")
        }
    }

    private var stateColor: Color {
        switch silence.state {
        case .active: .green
        case .pending: .blue
        case .expired: .secondary
        }
    }

    private var timeLabel: String {
        switch silence.state {
        case .active: String(localized: "Ends \(relativeTime(silence.endsAt))")
        case .pending: String(localized: "Starts \(relativeTime(silence.startsAt))")
        case .expired: String(localized: "Ended \(relativeTime(silence.endsAt))")
        }
    }
}
