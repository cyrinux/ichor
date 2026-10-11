import Foundation

/// How far a slow read got (Go ReadProgress, the checkup's): its steps in order, each pending,
/// running, done or failed, with the count of a fan-out (`count` of `total` nodes asked) and
/// how long a finished one took.
public struct ReadProgress: Decodable, Equatable, Sendable {
    public var running = false
    public var done = 0
    public var total = 0
    public var steps: [ReadStep] = []

    public init(running: Bool = false, done: Int = 0, total: Int = 0, steps: [ReadStep] = []) {
        self.running = running
        self.done = done
        self.total = total
        self.steps = steps
    }

    private enum CodingKeys: String, CodingKey { case running, done, total, steps }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        running = try c.field(.running, false)
        done = try c.field(.done, 0)
        total = try c.field(.total, 0)
        steps = try c.field(.steps, [])
    }

    /// The share of steps over, nil while there are none to count.
    public var fraction: Double? {
        total > 0 ? Double(min(done, total)) / Double(total) : nil
    }

    public static func decode(_ json: String) -> ReadProgress? {
        try? TalosJSON.decode(ReadProgress.self, from: json)
    }
}

/// One step of a ReadProgress. Its `id` is a fixed word (never an object or node name).
public struct ReadStep: Decodable, Equatable, Sendable, Identifiable {
    public enum State: String, Sendable {
        case pending, running, done, failed
    }

    public var id: String
    public var state: State
    public var count = 0
    public var total = 0
    public var ms: Int64 = 0

    public init(id: String, state: State, count: Int = 0, total: Int = 0, ms: Int64 = 0) {
        self.id = id
        self.state = state
        self.count = count
        self.total = total
        self.ms = ms
    }

    private enum CodingKeys: String, CodingKey { case id, state, count, total, ms }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.field(.id, "")
        state = State(rawValue: try c.field(.state, "")) ?? .pending
        count = try c.field(.count, 0)
        total = try c.field(.total, 0)
        ms = try c.field(.ms, Int64(0))
    }
}
