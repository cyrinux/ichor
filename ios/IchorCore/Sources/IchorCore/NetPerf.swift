import Foundation

// Mirrors go/ichorgo/kube_netperf.go.

/// Network paths a test measures.
public enum NetPerfPath {
    public static let pod = "pod"
    public static let host = "host"
}

/// Measurements made on each path.
public enum NetPerfTest {
    public static let throughput = "throughput"
    public static let latency = "latency"
}

public enum NetPerfPhase {
    public static let preparing = "preparing"
    public static let starting = "starting"
    public static let testing = "testing"
    public static let cleaning = "cleaning"
}

/// Seconds each measurement can last, offered in the setup.
public let netPerfDurations = [5, 10, 20]
public let netPerfDefaultSeconds = 10

public struct NetPerfNodeList: Decodable, Equatable, Sendable {
    public let nodes: [NetPerfNode]

    public init(nodes: [NetPerfNode] = []) { self.nodes = nodes }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        nodes = try c.field(.nodes, [])
    }

    private enum CodingKeys: String, CodingKey { case nodes }
}

/// A Kubernetes node a test can run on, by name.
public struct NetPerfNode: Decodable, Equatable, Identifiable, Sendable {
    public let name: String
    public let address: String
    public let controlPlane: Bool
    public let ready: Bool

    public var id: String { name }

    public init(name: String, address: String = "", controlPlane: Bool = false, ready: Bool = false) {
        self.name = name
        self.address = address
        self.controlPlane = controlPlane
        self.ready = ready
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        name = try c.decode(String.self, forKey: .name)
        address = try c.field(.address, "")
        controlPlane = try c.field(.controlPlane, false)
        ready = try c.field(.ready, false)
    }

    private enum CodingKeys: String, CodingKey { case name, address, controlPlane, ready }
}

/// Round trip in microseconds.
public struct NetPerfLatency: Codable, Equatable, Sendable {
    public let min: Double
    public let mean: Double
    public let max: Double
    public let p50: Double
    public let p90: Double
    public let p99: Double

    public init(min: Double = 0, mean: Double = 0, max: Double = 0, p50: Double = 0, p90: Double = 0, p99: Double = 0) {
        self.min = min
        self.mean = mean
        self.max = max
        self.p50 = p50
        self.p90 = p90
        self.p99 = p99
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        min = try c.field(.min, 0)
        mean = try c.field(.mean, 0)
        max = try c.field(.max, 0)
        p50 = try c.field(.p50, 0)
        p90 = try c.field(.p90, 0)
        p99 = try c.field(.p99, 0)
    }

    private enum CodingKeys: String, CodingKey { case min, mean, max, p50, p90, p99 }
}

public struct NetPerfResult: Codable, Equatable, Sendable {
    /// NetPerfPath.pod or .host.
    public let path: String
    /// NetPerfTest.throughput or .latency.
    public let test: String
    public let throughputMbps: Double
    /// Round trips per second.
    public let transactionRate: Double
    public let latency: NetPerfLatency?
    public let error: String

    public init(path: String, test: String, throughputMbps: Double = 0, transactionRate: Double = 0,
                latency: NetPerfLatency? = nil, error: String = "") {
        self.path = path
        self.test = test
        self.throughputMbps = throughputMbps
        self.transactionRate = transactionRate
        self.latency = latency
        self.error = error
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        path = try c.decode(String.self, forKey: .path)
        test = try c.decode(String.self, forKey: .test)
        throughputMbps = try c.field(.throughputMbps, 0)
        transactionRate = try c.field(.transactionRate, 0)
        latency = try c.decodeIfPresent(NetPerfLatency.self, forKey: .latency)
        error = try c.field(.error, "")
    }

    private enum CodingKeys: String, CodingKey {
        case path, test, throughputMbps, transactionRate, error
        case latency = "latencyUs"
    }
}

public struct NetPerfReport: Codable, Equatable, Sendable {
    public let server: String
    public let client: String
    public let hostNetwork: Bool
    public let seconds: Int
    public let image: String
    /// Unix ms.
    public let started: Int64
    public let finished: Int64
    public let results: [NetPerfResult]

    public init(server: String = "", client: String = "", hostNetwork: Bool = false, seconds: Int = 0, image: String = "",
                started: Int64 = 0, finished: Int64 = 0, results: [NetPerfResult] = []) {
        self.server = server
        self.client = client
        self.hostNetwork = hostNetwork
        self.seconds = seconds
        self.image = image
        self.started = started
        self.finished = finished
        self.results = results
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        server = try c.field(.server, "")
        client = try c.field(.client, "")
        hostNetwork = try c.field(.hostNetwork, false)
        seconds = try c.field(.seconds, 0)
        image = try c.field(.image, "")
        started = try c.field(.started, 0)
        finished = try c.field(.finished, 0)
        results = try c.field(.results, [])
    }

    private enum CodingKeys: String, CodingKey { case server, client, hostNetwork, seconds, image, started, finished, results }
}

public struct NetPerfProgress: Decodable, Equatable, Sendable {
    public let phase: String
    public let path: String
    public let test: String
    /// 1-based measurement number while phase is NetPerfPhase.testing.
    public let step: Int
    public let steps: Int
    /// While starting: "node: reason" of a pod waiting, e.g. its image being pulled.
    public let message: String
    public let at: Int64
    public let results: [NetPerfResult]

    public init(phase: String, path: String = "", test: String = "", step: Int = 0, steps: Int = 0, message: String = "",
                at: Int64 = 0, results: [NetPerfResult] = []) {
        self.phase = phase
        self.path = path
        self.test = test
        self.step = step
        self.steps = steps
        self.message = message
        self.at = at
        self.results = results
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        phase = try c.decode(String.self, forKey: .phase)
        path = try c.field(.path, "")
        test = try c.field(.test, "")
        step = try c.field(.step, 0)
        steps = try c.field(.steps, 0)
        message = try c.field(.message, "")
        at = try c.field(.at, 0)
        results = try c.field(.results, [])
    }

    private enum CodingKeys: String, CodingKey { case phase, path, test, step, steps, message, at, results }
}

/// What the user chose to test.
public struct NetPerfSetup: Equatable, Sendable {
    public var server: String
    public var client: String
    public var hostNetwork: Bool
    public var seconds: Int

    public init(server: String = "", client: String = "", hostNetwork: Bool = false, seconds: Int = netPerfDefaultSeconds) {
        self.server = server
        self.client = client
        self.hostNetwork = hostNetwork
        self.seconds = seconds
    }

    public var ready: Bool { !server.isEmpty && !client.isEmpty }

    /// Measurements the test makes: throughput and latency per network path.
    public var steps: Int { hostNetwork ? 4 : 2 }

    /// The paths the test measures, in order.
    public var paths: [String] { hostNetwork ? [NetPerfPath.pod, NetPerfPath.host] : [NetPerfPath.pod] }

    /// This setup with nodes that exist and are ready, the default pair filling what is not.
    public func withNodes(_ nodes: [NetPerfNode]) -> NetPerfSetup {
        var copy = self
        guard let pair = defaultNetPerfPair(nodes) else {
            copy.server = ""
            copy.client = ""
            return copy
        }
        let ready = Set(nodes.filter(\.ready).map(\.name))
        copy.server = ready.contains(server) ? server : pair.server
        copy.client = ready.contains(client) ? client : pair.client
        return copy
    }
}

public struct NetPerfPair: Equatable, Sendable {
    public let server: String
    public let client: String

    public init(server: String, client: String) {
        self.server = server
        self.client = client
    }
}

/// The pair a test starts with: two ready workers when there are, else two ready nodes, else
/// the one ready node against itself. Nil without a ready node.
public func defaultNetPerfPair(_ nodes: [NetPerfNode]) -> NetPerfPair? {
    let ready = nodes.filter(\.ready)
    let workers = ready.filter { !$0.controlPlane }
    let pick = workers.count >= 2 ? workers : ready
    switch pick.count {
    case 0: return nil
    case 1: return NetPerfPair(server: pick[0].name, client: pick[0].name)
    default: return NetPerfPair(server: pick[0].name, client: pick[1].name)
    }
}

/// Finished tests kept per cluster on the phone.
public let netPerfHistoryLimit = 20

extension NetPerfReport {
    /// What was tested, to show a saved report like the test that made it.
    public var setup: NetPerfSetup { NetPerfSetup(server: server, client: client, hostNetwork: hostNetwork, seconds: seconds) }
}

extension Array where Element == NetPerfReport {
    /// These saved tests, newest first, with `report` added in front and at most `limit` kept; a
    /// test already there (same start) is replaced.
    public func withReport(_ report: NetPerfReport, limit: Int = netPerfHistoryLimit) -> [NetPerfReport] {
        Array(([report] + filter { $0.started != report.started }).prefix(limit))
    }
}

/// One saved test in a pair's trend: its pod-to-pod figures, nil where not measured.
public struct NetPerfTrendPoint: Equatable, Sendable {
    /// Unix ms.
    public let started: Int64
    public let throughputMbps: Double?
    public let p50Us: Double?

    public init(started: Int64, throughputMbps: Double?, p50Us: Double?) {
        self.started = started
        self.throughputMbps = throughputMbps
        self.p50Us = p50Us
    }
}

extension Array where Element == NetPerfReport {
    /// The saved tests from `client` to `server`, oldest first, for the trend charts.
    public func trend(client: String, server: String) -> [NetPerfTrendPoint] {
        filter { $0.client == client && $0.server == server }
            .sorted { $0.started < $1.started }
            .map { report in
                let pod = report.results.filter { $0.path == NetPerfPath.pod && $0.error.isEmpty }
                return NetPerfTrendPoint(
                    started: report.started,
                    throughputMbps: pod.first { $0.test == NetPerfTest.throughput }?.throughputMbps,
                    p50Us: pod.first { $0.test == NetPerfTest.latency }?.latency?.p50
                )
            }
    }
}

/// "9.41 Gbit/s", "870 Mbit/s" (same output as the Android app).
public func formatMbps(_ mbps: Double) -> String {
    if mbps >= 1000 { return String(format: "%.2f Gbit/s", mbps / 1000) }
    if mbps >= 10 { return String(format: "%.0f Mbit/s", mbps) }
    return String(format: "%.1f Mbit/s", mbps)
}

/// "58 µs", "1.87 ms".
public func formatMicros(_ us: Double) -> String {
    us >= 1000 ? String(format: "%.2f ms", us / 1000) : String(format: "%.0f µs", us)
}
