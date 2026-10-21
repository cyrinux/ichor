import Foundation

// Edits staged in the cluster repository's browser, proposed later as one pull request with
// several files (GitForgePropose's "files"). In memory for the app's session only. Same rules
// as Android (data/StagedEdits.kt).

/// At most this many files go in one pull request (the Go core refuses more).
public let maxStagedFiles = 20

/// Where staged files go together: one repository, read and proposed at one branch.
public struct StagedKey: Hashable, Sendable {
    public let repoURL: String
    public let baseRef: String

    public init(repoURL: String, baseRef: String) {
        self.repoURL = repoURL
        self.baseRef = baseRef
    }

    /// The repository source's key, at the branch its files are read and proposed at.
    public init(source: GitSource) {
        self.init(repoURL: source.repoURL, baseRef: proposalBaseRef(for: source) ?? source.ref)
    }
}

/// One file staged for a pull request: original and its fileSha as read at the branch, draft
/// the new text.
public struct StagedFile: Hashable, Sendable {
    public let path: String
    public let original: String
    public let fileSha: String
    public let draft: String

    public init(path: String, original: String, fileSha: String, draft: String) {
        self.path = path
        self.original = original
        self.fileSha = fileSha
        self.draft = draft
    }

    public var proposal: GitProposalFile { GitProposalFile(path: path, content: draft, fileSha: fileSha) }
}

/// The staged files per repository and branch, one entry per path (staging a file again
/// replaces it in place).
public struct StagedBasket: Equatable, Sendable {
    public private(set) var files: [StagedKey: [StagedFile]] = [:]

    public init() {}

    /// The files staged for key, in the order they were first staged.
    public func of(_ key: StagedKey) -> [StagedFile] { files[key] ?? [] }

    /// Whether path can be staged for key: already there, or room for one more.
    public func canStage(_ key: StagedKey, path: String) -> Bool {
        let list = of(key)
        return list.contains { $0.path == path } || list.count < maxStagedFiles
    }

    public mutating func stage(_ key: StagedKey, _ file: StagedFile) {
        var list = of(key)
        if let at = list.firstIndex(where: { $0.path == file.path }) {
            list[at] = file
        } else {
            list.append(file)
        }
        files[key] = list
    }

    public mutating func remove(_ key: StagedKey, path: String) {
        let rest = of(key).filter { $0.path != path }
        files[key] = rest.isEmpty ? nil : rest
    }

    public mutating func clear(_ key: StagedKey) {
        files[key] = nil
    }
}

/// The path a pull request of several paths is titled after: their deepest common directory,
/// else the first path (files at the root have none in common).
public func stagedDefaultsPath(_ paths: [String]) -> String {
    guard paths.count > 1 else { return paths.first ?? "" }
    let dirs = paths.map { $0.split(separator: "/", omittingEmptySubsequences: false).dropLast().map(String.init) }
    var common = dirs[0]
    for dir in dirs.dropFirst() {
        common = Array(zip(common, dir).prefix { $0.0 == $0.1 }.map(\.0))
    }
    return common.isEmpty ? paths[0] : common.joined(separator: "/")
}
