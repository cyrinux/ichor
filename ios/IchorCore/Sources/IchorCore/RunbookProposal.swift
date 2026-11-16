import Foundation

/// A step the model proposed that the app cannot run, and why. `id` is empty when the whole
/// block could not be read.
public struct RunbookRejected: Decodable, Equatable, Hashable, Sendable {
    public let id: String
    public let reason: String

    private enum CodingKeys: String, CodingKey { case id, reason }

    public init(id: String, reason: String) {
        self.id = id
        self.reason = reason
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.field(.id, "")
        reason = try c.field(.reason, "")
    }

    /// The row "Not possible from the app" shows.
    public var line: String { id.isEmpty ? reason : "\(id): \(reason)" }
}

/// ProposeRunbook's answer: the runbook the model wrote from the diagnosis answer, its plan as
/// the planner checked it, and the steps left out. `empty`: no step was left (no YAML, no plan).
public struct RunbookProposal: Decodable, Equatable, Sendable {
    public let runbookYAML: String
    public let plan: RunbookPlan?
    public let rejected: [RunbookRejected]
    public let empty: Bool

    private enum CodingKeys: String, CodingKey { case runbookYAML, plan, rejected, empty }

    public init(runbookYAML: String, plan: RunbookPlan?, rejected: [RunbookRejected] = [], empty: Bool = false) {
        self.runbookYAML = runbookYAML
        self.plan = plan
        self.rejected = rejected
        self.empty = empty
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        runbookYAML = try c.field(.runbookYAML, "")
        plan = try c.decodeIfPresent(RunbookPlan.self, forKey: .plan)
        rejected = try c.field(.rejected, [])
        empty = try c.field(.empty, false)
    }

    /// The plan to show, nil when nothing of the answer can run.
    public var runnablePlan: RunbookPlan? { empty || runbookYAML.isEmpty ? nil : plan }
}
