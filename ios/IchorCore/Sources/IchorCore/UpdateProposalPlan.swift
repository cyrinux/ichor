import Foundation

// UpdateProposalPlan's answer: one pull request for the Updates rows ticked, each file read at
// the request's branch with every edit applied. Same model as Android (model/ClusterUpdates.kt).

/// One item's edit in a planned file: line is on the content the items before it already edited.
public struct UpdateProposalChange: Decodable, Hashable, Sendable {
    public let item: String
    public let index: Int
    public let line: Int
    public let before: String
    public let after: String

    enum CodingKeys: String, CodingKey { case item, index, line, before, after }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        item = try c.field(.item, "")
        index = try c.field(.index, 0)
        line = try c.field(.line, 0)
        before = try c.field(.before, "")
        after = try c.field(.after, "")
    }
}

/// A file of the plan: original as read at the branch (its fileSha the lock), content with
/// every change applied.
public struct UpdateProposalFile: Decodable, Hashable, Sendable {
    public let path: String
    public let content: String
    public let original: String
    public let fileSha: String
    public let changes: [UpdateProposalChange]

    enum CodingKeys: String, CodingKey { case path, content, original, fileSha, changes }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        path = try c.field(.path, "")
        content = try c.field(.content, "")
        original = try c.field(.original, "")
        fileSha = try c.field(.fileSha, "")
        changes = try c.field(.changes, [UpdateProposalChange]())
    }

    /// The file staged for the pull request.
    public var staged: StagedFile { StagedFile(path: path, original: original, fileSha: fileSha, draft: content) }
}

/// An item left out of the plan, index in the items sent, and why.
public struct UpdateProposalSkip: Decodable, Hashable, Sendable {
    public let item: String
    public let index: Int
    public let reason: String

    enum CodingKeys: String, CodingKey { case item, index, reason }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        item = try c.field(.item, "")
        index = try c.field(.index, 0)
        reason = try c.field(.reason, "")
    }
}

/// One pull request on source's repository at baseRef for the items selected.
public struct UpdateProposalPlan: Decodable, Hashable, Sendable {
    public let source: GitSource?
    public let baseRef: String
    public let files: [UpdateProposalFile]
    public let title: String
    public let message: String
    public let branch: String
    public let body: String
    public let skipped: [UpdateProposalSkip]

    enum CodingKeys: String, CodingKey { case source, baseRef, files, title, message, branch, body, skipped }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        source = try c.decodeIfPresent(GitSource.self, forKey: .source)
        baseRef = try c.field(.baseRef, "")
        files = try c.field(.files, [UpdateProposalFile]())
        title = try c.field(.title, "")
        message = try c.field(.message, "")
        branch = try c.field(.branch, "")
        body = try c.field(.body, "")
        skipped = try c.field(.skipped, [UpdateProposalSkip]())
    }
}

/// The version a row is selected with: its smallest step (patch, else minor, else major); nil
/// when it cannot be proposed.
public func defaultSelection(_ row: UpdateRow) -> String? {
    row.editable ? row.candidates.first?.version : nil
}

/// UpdateProposalPlan's itemsJSON: each selected row's item with the version picked, in order.
public func updateProposalItemsJSON(_ selection: [(row: UpdateRow, version: String)]) -> String {
    "[" + selection.map { updateItemJSON($0.row, version: $0.version) }.joined(separator: ",") + "]"
}
