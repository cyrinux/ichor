import SwiftUI
import IchorCore

struct InsightsView: View {
    @Environment(AppModel.self) private var model
    var body: some View {
        if let cluster = model.activeSummary?.fingerprint {
            InsightsContent(cluster: cluster, storageScope: "\(cluster)-\(model.privacyStorageKey)")
                .id("\(cluster)-\(model.dataGeneration)")
        }
    }
}

private struct InsightsContent: View {
    @Environment(AppModel.self) private var model
    @Environment(\.scenePhase) private var scenePhase
    @State private var session: InsightsSession
    @State private var tab = 0
    @State private var showMetrics = false
    /// App-wide: leaving the screen keeps a running network test.
    private var netPerf: NetPerfJob { .shared }
    init(cluster: String, storageScope: String) {
        _session = State(initialValue: InsightsSession(cluster: cluster, storageScope: storageScope))
    }
    private func time(_ at: Int64) -> String {
        Date(epochMillis: at).formatted(date: .abbreviated, time: .standard)
    }
    private var picker: some View {
        Picker("Cluster insights", selection: $tab) {
            Text("Drift").tag(0)
            Text("Recorder").tag(1)
            Text("Network").tag(2)
        }.pickerStyle(.segmented)
    }
    var body: some View {
        VStack(spacing: 0) {
            picker.padding(.horizontal).padding(.bottom, 8)
            SwipeTabPager(tabs: [0, 1, 2], selection: $tab) { shown in
                if shown == 2 {
                    NetPerfView(session: netPerf)
                } else {
                    List {
                        if let error = session.error { Text(verbatim: error).foregroundStyle(.statusBad) }
                        if shown == 0 { drift } else { recorder }
                    }
                }
            }
        }
        .themedBackground()
        .navigationTitle("Cluster insights")
        .task { if let client = model.client { await session.load(client: client) } }
        .onDisappear { session.stop() }
        .onChange(of: scenePhase) { _, phase in if phase != .active { session.stop() } }
        .onChange(of: session.recording) { _, recording in UIApplication.shared.isIdleTimerDisabled = recording }
        .onDisappear { UIApplication.shared.isIdleTimerDisabled = false }
    }
    @ViewBuilder private var drift: some View {
        Section {
            // Borderless and bordered styles keep the two buttons separate inside one list row.
            HStack(spacing: 12) {
                Button("Refresh") { Task { if let client = model.client { await session.refresh(client: client) } } }
                    .buttonStyle(.borderedProminent)
                    .disabled(session.busy)
                Button("Save current baseline") { session.saveBaseline() }
                    .buttonStyle(.borderless)
                    .disabled(session.snapshot == nil || session.busy)
                Spacer(minLength: 0)
                if session.busy { ProgressView() }
            }
            if let at = session.baselineAt {
                Text("Baseline: \(time(at))").font(.caption)
                Button("Delete baseline", role: .destructive) { Task { await session.deleteBaseline() } }
            } else {
                Text(
                    "Reference: first reachable node of each role. Save a baseline to track changes on the same nodes."
                )
                .font(.caption).foregroundStyle(.secondary)
            }
        } footer: {
            Text(
                "Compare selected effective settings: DNS, NTP, MTU, extensions, Secure Boot, UKI and Talos version. Differences may be intentional; unavailable fields are unknown."
            )
        }
        if let snapshot = session.snapshot {
            Section {
                Text("Sample: \(time(snapshot.at))").font(.caption)
                if session.changes.isEmpty { Text("No differences in comparable fields.") }
            }
            ForEach(session.changes) { change in
                DriftChangeSection(
                    change: change,
                    fixDestination: change.fix.map { (fix: DriftFix) -> AnyView in
                        let known = snapshot.nodes.first { $0.node == change.node }?.hostname ?? ""
                        let hostname = change.nodeName.isEmpty ? known : change.nodeName
                        return AnyView(
                            MachineConfigView(
                                node: change.node, hostname: hostname.isEmpty ? change.node : hostname, fix: fix))
                    })
            }
            ForEach(snapshot.nodes.filter { !$0.errors.isEmpty }) { node in
                Text(
                    verbatim:
                        "\(node.hostname): \(node.errors.keys.sorted().map { "\($0): \(node.errors[$0] ?? "")" }.joined(separator: "; "))"
                )
                .font(.caption).foregroundStyle(.statusBad)
            }
        }
    }
    @ViewBuilder private var recorder: some View {
        Section {
            // Borderless and bordered styles keep the two buttons separate inside one list row.
            HStack(spacing: 12) {
                if session.recording {
                    Button("Stop recording") { session.stop() }
                        .buttonStyle(.borderedProminent)
                    ProgressView()
                } else {
                    Button("Start recording") { if let client = model.client { session.start(client: client) } }
                        .buttonStyle(.borderedProminent)
                }
                Button("Delete", role: .destructive) { session.deleteRecording() }
                    .buttonStyle(.borderless)
                    .disabled(session.recording || session.document == nil)
                Spacer(minLength: 0)
            }
        } header: {
            HStack(spacing: 4) {
                Text("Incident recorder")
                InfoHint(title: Text("Incident recorder"), text: Text(Self.recordNote))
            }
        }
        if let doc = session.document {
            Section {
                ForEach(incidentTimeline(doc.entries.reversed().filter { showMetrics || $0.kind != "metrics" })) {
                    item in
                    IncidentTimelineRow(item: item, startedAt: doc.startedAt, time: time)
                }
            } header: {
                SectionHeader(title: String(localized: "Timeline"), subtitle: recordingLine(doc), pill: timelineMenu)
            }
        } else {
            Text("No saved recording.")
        }
    }
    private static let recordNote: LocalizedStringKey =
        "Record up to 10 minutes while this screen is in the foreground. Health, services, links and metrics are sampled, with live Talos events. No raw logs or credentials. The latest session is encrypted locally; starting replaces it. Keep up to 600 entries. Event timestamps use node clocks; ordering does not prove causation."
    /// The recorded range and the count of discarded entries, on one muted line.
    private func recordingLine(_ doc: IncidentDocument) -> String {
        let range = "\(time(doc.startedAt)) — \(time(doc.updatedAt))"
        guard doc.dropped > 0 else { return range }
        return range + " · " + String(localized: "Older entries discarded: \(doc.dropped)")
    }
    private var timelineMenu: some View {
        Menu {
            Toggle("Show metric samples", isOn: $showMetrics)
        } label: {
            Image(systemName: "line.3.horizontal.decrease.circle")
        }
        .accessibilityLabel(Text("Filter"))
    }
}

/// A timeline row: one entry, or a run of healthy entries that expands to them.
private struct IncidentTimelineRow: View {
    let item: IncidentTimelineItem
    let startedAt: Int64
    let time: (Int64) -> String

    var body: some View {
        switch item {
        case .entry(let entry):
            IncidentEntryRow(entry: entry, startedAt: startedAt, time: time)
        case .healthyRun(let entries):
            DisclosureGroup {
                ForEach(entries) { entry in
                    IncidentEntryRow(entry: entry, startedAt: startedAt, time: time)
                }
            } label: {
                ResourceRow(
                    health: .ok,
                    title: String(localized: "\(entries.count) services healthy"),
                    subtitle: runSubtitle(entries),
                    mono: false
                )
            }
        }
    }

    /// The node and the span of the run (newest first, as the timeline is).
    private func runSubtitle(_ entries: [IncidentEntry]) -> String {
        guard let first = entries.first, let last = entries.last else { return "" }
        let span =
            incidentOffset(at: last.at, startedAt: startedAt) + " – "
            + incidentOffset(at: first.at, startedAt: startedAt)
        return [first.node, span].filter { !$0.isEmpty }.joined(separator: " · ")
    }
}

/// One entry: its dot, the subject and "kind · state · +mm:ss"; it expands to the evidence.
private struct IncidentEntryRow: View {
    let entry: IncidentEntry
    let startedAt: Int64
    let time: (Int64) -> String

    var body: some View {
        DisclosureGroup {
            IncidentEvidenceCard(entry: entry, timestamp: time(entry.at))
        } label: {
            ResourceRow(
                health: entry.timelineHealth,
                title: entry.subject.isEmpty ? entry.node : entry.subject,
                subtitle: subtitle
            )
        }
    }

    private var subtitle: String {
        [incidentKindLabel(entry.kind), incidentState(entry), incidentOffset(at: entry.at, startedAt: startedAt)]
            .filter { !$0.isEmpty }
            .joined(separator: " · ")
    }
}

/// A row's kind, the first part of its subtitle. Keyed: "Service" and "Link" mean other
/// things elsewhere in the app.
private func incidentKindLabel(_ kind: String) -> String {
    switch kind {
    case "status": return String(localized: "insights.kind.status", defaultValue: "Node")
    case "service": return String(localized: "insights.kind.service", defaultValue: "Service")
    case "metrics": return String(localized: "insights.kind.metrics", defaultValue: "Metrics")
    case "link": return String(localized: "insights.kind.link", defaultValue: "Link")
    case "error": return String(localized: "insights.kind.error", defaultValue: "Error")
    case "recovered": return String(localized: "insights.kind.recovered", defaultValue: "Recovered")
    default: return String(localized: "insights.kind.event", defaultValue: "Event")
    }
}

/// The entry's state in a word: the node status, the service state, the new link state.
private func incidentState(_ entry: IncidentEntry) -> String {
    let detail = entry.evidenceDetail ?? [:]
    switch entry.kind {
    case "status":
        let ready = detail["ready"] as? Bool
        let reachable = detail["reachable"] as? Bool
        if reachable == false { return String(localized: "Unreachable") }
        if ready == true && reachable == true { return String(localized: "Ready") }
        if ready == false { return String(localized: "Not ready") }
        return String(localized: "Unknown")
    case "service":
        let state = detail["state"] as? String ?? ""
        return state.isEmpty ? incidentServiceHealth(detail["health"] as? String ?? "") : state
    case "link": return detail["after"] as? String ?? ""
    case "error": return String(localized: "Evidence unavailable")
    default: return ""
    }
}

private func incidentServiceHealth(_ health: String) -> String {
    switch health {
    case "healthy": return String(localized: "Healthy")
    case "unhealthy": return String(localized: "Unhealthy")
    default: return String(localized: "Unknown")
    }
}

private struct IncidentEvidenceCard: View {
    let entry: IncidentEntry
    let timestamp: String
    private var detail: [String: Any] { entry.evidenceDetail ?? [:] }
    private func value(_ key: String) -> String { detail[key] as? String ?? "" }
    private var ready: Bool? { detail["ready"] as? Bool }
    private var reachable: Bool? { detail["reachable"] as? Bool }
    private var warning: Bool {
        entry.severity == "warning" || entry.severity == "error"
            || (entry.kind == "status" && (ready == false || reachable == false))
            || (entry.kind == "service" && value("health") == "unhealthy")
            || (entry.evidenceMetrics.map {
                !$0.errors.isEmpty || $0.network.contains { $0.errors > 0 || $0.drops > 0 }
            } ?? false)
    }
    private var positive: Bool {
        !warning
            && (entry.kind == "recovered" || (entry.kind == "status" && ready == true && reachable == true)
                || (entry.kind == "service" && value("health") == "healthy"))
    }
    private var color: Color { warning ? .red : positive ? .green : .blue }
    private var icon: String {
        if warning { return "exclamationmark.triangle.fill" }
        if positive { return "checkmark.circle.fill" }
        switch entry.kind {
        case "metrics": return "chart.xyaxis.line"
        case "service": return "gearshape.fill"
        case "link": return "network"
        default: return "info.circle.fill"
        }
    }
    private var title: String {
        switch entry.kind {
        case "status": return incidentState(entry)
        case "metrics": return String(localized: "Bottleneck metrics")
        case "service": return String(localized: "Service status")
        case "link": return String(localized: "Network link changed")
        case "error": return String(localized: "Evidence unavailable")
        case "recovered": return String(localized: "Evidence collection recovered")
        default: return String(localized: "Events")
        }
    }
    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text(verbatim: "\(timestamp) · \(entry.node)").font(.caption).foregroundStyle(.secondary)
            Label(title, systemImage: icon).font(.headline).foregroundStyle(color)
            if !entry.subject.isEmpty { Text(verbatim: entry.subject).font(.subheadline.bold()) }
            switch entry.kind {
            case "metrics":
                if let metrics = entry.evidenceMetrics {
                    BottleneckContent(detail: metrics)
                    if let omitted = entry.metricsOmitted, omitted > 0 {
                        Text("Additional device or error entries omitted: \(omitted)").font(.caption)
                    }
                } else {
                    Text("This older sample has incomplete details. Record a new session to see formatted metrics.")
                }
            case "status":
                if !value("stage").isEmpty { Text("Stage: \(value("stage"))") }
                if !value("error").isEmpty { Text(verbatim: value("error")).foregroundStyle(.statusBad) }
                if let conditions = detail["conditions"] as? [[String: Any]] {
                    ForEach(Array(conditions.enumerated()), id: \.offset) { _, condition in
                        Text(verbatim: "\(condition["name"] as? String ?? ""): \(condition["reason"] as? String ?? "")")
                    }
                }
            case "service":
                Text(verbatim: "\(value("state")) · \(health)")
                if !value("message").isEmpty { Text(verbatim: value("message")) }
            case "link": Text("Link: \(value("before")) → \(value("after"))")
            case "recovered": Text("This section can be sampled again.")
            default:
                if !entry.detail.isEmpty { Text(verbatim: entry.detail).lineLimit(4) }
            }
            if !entry.detail.isEmpty {
                DisclosureGroup("Technical details") {
                    Text(verbatim: entry.detail).font(.caption.monospaced()).textSelection(.enabled)
                }.font(.caption)
            }
        }.padding(.vertical, 4)
    }
    private var health: String { incidentServiceHealth(value("health")) }
}
