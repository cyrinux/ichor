import Foundation

// Find in repository, as the Go core answers it (go/ichorgo/repo_search.go): every line of a
// repository's text files holding the text, from a snapshot of the repository downloaded once
// per commit, or from a Flux source's artifact. Same records as Android (model/GitRepoSearch.kt).

/// One matching line: path from the repository root, line 1-based, text trimmed to 200 characters.
public struct GitSearchHit: Decodable, Hashable, Sendable, Identifiable {
    public let path: String
    public let line: Int
    public let text: String

    public var id: String { "\(path):\(line)" }

    public init(path: String, line: Int, text: String) {
        self.path = path
        self.line = line
        self.text = text
    }

    enum CodingKeys: String, CodingKey { case path, line, text }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        path = try c.field(.path, "")
        line = try c.field(.line, 0)
        text = try c.field(.text, "")
    }
}

public struct GitRepoSearch: Decodable, Equatable, Sendable {
    public let sha: String
    public let hits: [GitSearchHit]
    /// The text files searched.
    public let searched: Int
    /// More lines match than hits holds.
    public let truncated: Bool

    public init(sha: String = "", hits: [GitSearchHit] = [], searched: Int = 0, truncated: Bool = false) {
        self.sha = sha
        self.hits = hits
        self.searched = searched
        self.truncated = truncated
    }

    enum CodingKeys: String, CodingKey { case sha, hits, searched, truncated }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        sha = try c.field(.sha, "")
        hits = try c.field(.hits, [GitSearchHit]())
        searched = try c.field(.searched, 0)
        truncated = try c.field(.truncated, false)
    }

    /// The hits by file, files in path order and lines in order, as the results list shows them.
    public var byFile: [(path: String, hits: [GitSearchHit])] {
        Dictionary(grouping: hits, by: \.path)
            .map { (path: $0.key, hits: $0.value.sorted { $0.line < $1.line }) }
            .sorted { $0.path < $1.path }
    }
}
