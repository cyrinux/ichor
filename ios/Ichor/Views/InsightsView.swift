import SwiftUI
import IchorCore
import CryptoKit

struct InsightsView: View {
    @Environment(AppModel.self) private var model
    private var storageKey: String {
        guard model.privacyMask else { return "real" }
        return SHA256.hash(data: Data(model.privacyWords.utf8)).prefix(8).map { String(format: "%02x", $0) }.joined()
    }
    var body: some View {
        if let cluster = model.activeSummary?.fingerprint {
            InsightsContent(cluster: cluster, storageScope: "\(cluster)-\(storageKey)")
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
    init(cluster: String, storageScope: String) {
        _session = State(initialValue: InsightsSession(cluster: cluster, storageScope: storageScope))
    }
    private func time(_ at: Int64) -> String {
        Date(timeIntervalSince1970: Double(at) / 1000).formatted(date: .abbreviated, time: .standard)
    }
    var body: some View {
        List {
            Picker("Cluster insights", selection: $tab) {
                Text("Configuration drift").tag(0)
                Text("Incident recorder").tag(1)
            }.pickerStyle(.segmented)
            if let error = session.error { Text(verbatim: error).foregroundStyle(.red) }
            if tab == 0 { drift } else { recorder }
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
                    .font(.caption).foregroundStyle(.red)
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
                    VStack(alignment: .leading, spacing: 4) {
                        Text(verbatim: "\(time(entry.at)) · \(entry.node)").font(.caption)
                        Text(verbatim: "\(entry.kind) · \(entry.subject)").font(.headline)
                            .foregroundStyle(entry.severity == "info" ? Color.primary : Color.orange)
                        if !entry.detail.isEmpty { Text(verbatim: entry.detail).font(.caption.monospaced()) }
                    }
                }
            }
        } else { Text("No saved recording.") }
    }
}
