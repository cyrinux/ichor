import Foundation

/// A file that runs a dependency-update bot on the repository: its configuration, a workflow,
/// or Dependabot's.
public struct UpdateBotFile: Decodable, Hashable, Sendable {
    public let path: String
    public let kind: String

    public init(path: String, kind: String) {
        self.path = path
        self.kind = kind
    }

    enum CodingKeys: String, CodingKey { case path, kind }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        path = try c.field(.path, "")
        kind = try c.field(.kind, "")
    }
}

/// The configuration file drafted for a repository without a bot; empty when there is nothing
/// to propose.
public struct UpdateBotDraft: Decodable, Hashable, Sendable {
    public let path: String
    public let content: String

    public init(path: String = "", content: String = "") {
        self.path = path
        self.content = content
    }

    enum CodingKeys: String, CodingKey { case path, content }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        path = try c.field(.path, "")
        content = try c.field(.content, "")
    }
}

/// UpdateBotConfig's answer for the cluster repository at commit sha: the bot files present (a
/// bot is configured), else the draft to propose, and notes saying why.
public struct UpdateBotConfig: Decodable, Hashable, Sendable {
    public let sha: String
    public let present: [UpdateBotFile]
    public let draft: UpdateBotDraft
    public let notes: [String]

    public init(
        sha: String = "", present: [UpdateBotFile] = [], draft: UpdateBotDraft = UpdateBotDraft(), notes: [String] = []
    ) {
        self.sha = sha
        self.present = present
        self.draft = draft
        self.notes = notes
    }

    public var configured: Bool { !present.isEmpty }
    public var proposable: Bool { !configured && !draft.path.isEmpty && !draft.content.isEmpty }

    /// The pull request's description: one line per note.
    public var proposalBody: String { notes.map { "- \($0)" }.joined(separator: "\n") }

    enum CodingKeys: String, CodingKey { case sha, present, draft, notes }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        sha = try c.field(.sha, "")
        present = try c.field(.present, [UpdateBotFile]())
        draft = try c.field(.draft, UpdateBotDraft())
        notes = try c.field(.notes, [String]())
    }
}
