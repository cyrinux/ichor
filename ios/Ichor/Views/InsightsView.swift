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
    /// Kept across tabs: only leaving the screen stops a running network test.
    @State private var netPerf = NetPerfSession()
    init(cluster: String, storageScope: String) {
        _session = State(initialValue: InsightsSession(cluster: cluster, storageScope: storageScope))
    }
    private func time(_ at: Int64) -> String {
        Date(epochMillis: at).formatted(date: .abbreviated, time: .standard)
    }
    private var picker: some View {
        Picker("Cluster insights", selection: $tab) {
            Text("Configuration drift").tag(0)
            Text("Incident recorder").tag(1)
            Text("Network").tag(2)
        }.pickerStyle(.segmented)
    }
    var body: some View {
        Group {
            if tab == 2 {
                VStack(spacing: 0) {
                    picker.padding(.horizontal)
                    NetPerfView(session: netPerf)
                }
            } else {
                List {
                    picker
                    if let error = session.error { Text(verbatim: error).foregroundStyle(.statusBad) }
                    if tab == 0 { drift } else { recorder }
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
        .onDisappear { if netPerf.isRunning { netPerf.leave() } }
    }
    @ViewBuilder private var drift: some View {
        Section {
            Text("Compare selected effective settings: DNS, NTP, MTU, extensions, Secure Boot, UKI and Talos version. Differences may be intentional; unavailable fields are unknown.")
                .font(.caption)
            Button("Refresh") { Task { if let client = model.client { await session.refresh(client: client) } } }
                .disabled(session.busy)
            Button("Save current baseline") { session.saveBaseline() }
                .disabled(session.snapshot == nil || session.busy)
            if let at = session.baselineAt {
                Text("Baseline: \(time(at))").font(.caption)
                Button("Delete baseline", role: .destructive) { Task { await session.deleteBaseline() } }
            } else {
                Text("Reference: first reachable node of each role. Save a baseline to track changes on the same nodes.").font(.caption)
            }
            if session.busy { ProgressView() }
        }
        if let snapshot = session.snapshot {
            Section {
                Text("Sample: \(time(snapshot.at))").font(.caption)
                if session.changes.isEmpty { Text("No differences in comparable fields.") }
                ForEach(session.changes) { change in
                    VStack(alignment: .leading, spacing: 4) {
                        Text(verbatim: "\(change.node) · \(change.key)").font(.headline)
                        Text("Reference: \(change.reference)").font(.caption)
                        Text(verbatim: "\(change.before.isEmpty ? "∅" : change.before) → \(change.after.isEmpty ? "∅" : change.after)").font(.caption.monospaced())
                    }
                }
            }
            ForEach(snapshot.nodes.filter { !$0.errors.isEmpty }) { node in
                Text(verbatim: "\(node.hostname): \(node.errors.keys.sorted().map { "\($0): \(node.errors[$0] ?? "")" }.joined(separator: "; "))")
                    .font(.caption).foregroundStyle(.statusBad)
            }
        }
    }
    @ViewBuilder private var recorder: some View {
        Section {
            Text("Record up to 10 minutes while this screen is in the foreground. Health, services, links and metrics are sampled, with live Talos events. No raw logs or credentials. The latest session is encrypted locally; starting replaces it. Keep up to 600 entries. Event timestamps use node clocks; ordering does not prove causation.")
                .font(.caption)
            if session.recording {
                Button("Stop recording") { session.stop() }
                ProgressView()
            } else {
                Button("Start recording") { if let client = model.client { session.start(client: client) } }
            }
            Button("Delete", role: .destructive) { session.deleteRecording() }
                .disabled(session.recording || session.document == nil)
        }
        if let doc = session.document {
            Section {
                Text(verbatim: "\(time(doc.startedAt)) — \(time(doc.updatedAt))").font(.caption)
                if doc.dropped > 0 { Text("Older entries discarded: \(doc.dropped)") }
                Toggle("Show metric samples", isOn: $showMetrics)
            }
            Section {
                ForEach(doc.entries.reversed().filter { showMetrics || $0.kind != "metrics" }) { entry in
                    IncidentEvidenceCard(entry: entry, timestamp: time(entry.at))
                }
            }
        } else { Text("No saved recording.") }
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
        entry.severity == "warning" || entry.severity == "error" ||
        (entry.kind == "status" && (ready == false || reachable == false)) ||
        (entry.kind == "service" && value("health") == "unhealthy") ||
        (entry.evidenceMetrics.map { !$0.errors.isEmpty || $0.network.contains { $0.errors > 0 || $0.drops > 0 } } ?? false)
    }
    private var positive: Bool {
        !warning && (entry.kind == "recovered" ||
        (entry.kind == "status" && ready == true && reachable == true) ||
        (entry.kind == "service" && value("health") == "healthy"))
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
        case "status":
            if reachable == false { return String(localized: "Unreachable") }
            if ready == true && reachable == true { return String(localized: "Ready") }
            if ready == false { return String(localized: "Not ready") }
            return String(localized: "Unknown")
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
                } else { Text("This older sample has incomplete details. Record a new session to see formatted metrics.") }
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
    private var health: String {
        switch value("health") {
        case "healthy": return String(localized: "Healthy")
        case "unhealthy": return String(localized: "Unhealthy")
        default: return String(localized: "Unknown")
        }
    }
}
