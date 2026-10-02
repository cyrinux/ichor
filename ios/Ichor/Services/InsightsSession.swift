import Foundation
import IchorCore

@Observable
@MainActor
final class InsightsSession {
    private(set) var snapshot: DriftSnapshot?
    private(set) var changes: [DriftChange] = []
    private(set) var baselineAt: Int64?
    private(set) var document: IncidentDocument?
    private(set) var busy = false
    private(set) var recording = false
    private(set) var error: String?
    private var snapshotJSON: String?
    private var baselineJSON: String?
    private var recordingTask: Task<Void, Never>?
    private var pending: [NodeEvent] = []
    private var pendingLost = 0
    private var documentJSON = ""
    private let store: InsightsStore
    private let cluster: String

    init(cluster: String, storageScope: String) {
        self.cluster = cluster
        store = InsightsStore(scope: storageScope)
    }
    func load(client: TalosClient) async {
        do {
            baselineJSON = try store.read("baseline")
            if let baselineJSON { baselineAt = try decode(DriftSnapshot.self, baselineJSON).at }
            if let raw = try store.read("incident") { document = try decode(IncidentDocument.self, raw) }
        } catch { self.error = error.localizedDescription }
        await refresh(client: client)
    }
    func refresh(client: TalosClient) async {
        guard !busy else { return }
        busy = true
        defer { busy = false }
        do {
            let raw = try await client.driftSnapshot()
            try Task.checkCancellation()
            let value = try decode(DriftSnapshot.self, raw)
            guard value.scope == cluster else { return }
            let differences = try await TalosClient.compareDrift(baseline: baselineJSON ?? "", current: raw)
            try Task.checkCancellation()
            snapshotJSON = raw
            snapshot = value
            changes = differences
        } catch is CancellationError { }
        catch { self.error = error.localizedDescription }
    }
    func saveBaseline() {
        guard let raw = snapshotJSON else { return }
        do {
            try store.save("baseline", json: raw)
            baselineJSON = raw
            baselineAt = snapshot?.at
            changes = []
        } catch { self.error = error.localizedDescription }
    }
    func deleteBaseline() async {
        do {
            try store.delete("baseline")
            baselineJSON = nil
            baselineAt = nil
            if let raw = snapshotJSON { changes = try await TalosClient.compareDrift(baseline: "", current: raw) }
        } catch { self.error = error.localizedDescription }
    }
    func deleteRecording() {
        guard !recording else { return }
        do { try store.delete("incident"); document = nil }
        catch { self.error = error.localizedDescription }
    }
    func stop() { recordingTask?.cancel() }
    func start(client: TalosClient) {
        guard !recording else { return }
        recording = true
        error = nil
        document = nil
        documentJSON = ""
        pending = []
        pendingLost = 0
        recordingTask = Task {
            let eventTask = Task {
                for await event in client.events(node: nil, tail: 0) {
                    guard !Task.isCancelled else { break }
                    switch event {
                    case .event(let value):
                        if pending.count >= 100 { pending.removeFirst(); pendingLost += 1 }
                        pending.append(value)
                    case .done(let problem):
                        if let problem {
                            error = problem
                            let now = Int64(Date().timeIntervalSince1970 * 1000)
                            pending.append(NodeEvent(node: "", eventId: "stream-\(now)", at: now, kind: "recording", subject: "events", action: "unavailable", message: problem, severity: "warning"))
                        }
                    }
                }
            }
            let deadline = ContinuousClock.now.advanced(by: .seconds(600))
            var lastObservation: String?
            do {
                while ContinuousClock.now < deadline {
                    try Task.checkCancellation()
                    let raw = try await client.observation()
                    try Task.checkCancellation()
                    let value = try JSONSerialization.jsonObject(with: Data(raw.utf8)) as? [String: Any]
                    guard value?["scope"] as? String == cluster else { break }
                    lastObservation = raw
                    try await checkpoint(raw)
                    try await Task.sleep(for: .seconds(5))
                }
            } catch is CancellationError { }
            catch { self.error = error.localizedDescription }
            eventTask.cancel()
            if let lastObservation {
                do { try await checkpoint(lastObservation) }
                catch { self.error = error.localizedDescription }
            }
            recording = false
        }
    }
    private func checkpoint(_ observation: String) async throws {
        var events = pending
        pending = []
        if pendingLost > 0 {
            let now = Int64(Date().timeIntervalSince1970 * 1000)
            events.append(NodeEvent(node: "", eventId: "overflow-\(now)", at: now, kind: "recording", subject: "events", action: "overflow", message: "{\"discarded\":\(pendingLost)}", severity: "warning"))
            pendingLost = 0
        }
        let raw = try await TalosClient.updateIncident(previous: documentJSON, observation: observation, events: events)
        try store.save("incident", json: raw)
        documentJSON = raw
        document = try decode(IncidentDocument.self, raw)
    }
    private func decode<T: Decodable>(_ type: T.Type, _ raw: String) throws -> T {
        try JSONDecoder().decode(type, from: Data(raw.utf8))
    }
}
