import Foundation

/// The built-in Ollama plugin's id (go/plugin/builtin/ollama.yaml).
public let ollamaPluginID = "ollama"

/// The plugin card that lists the servers: its rows get the "Pull a model…" button.
public let ollamaServersCardID = "servers"

/// The ollama.com library, where the names a pull takes are listed.
public let ollamaLibraryURL = URL(string: "https://ollama.com/library")!

private let modelNameMax = 128
private let nameCharacters = Set("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789._/-")
private let tagCharacters = Set("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789._-")

/// A model name the Go core accepts for a pull (`name[:tag]`, as StartOllamaPull checks it again).
public func isOllamaModelName(_ name: String) -> Bool {
    guard (1...modelNameMax).contains(name.count), !name.contains("..") else { return false }
    let parts = name.split(separator: ":", maxSplits: 1, omittingEmptySubsequences: false)
    guard let base = parts.first, !base.isEmpty, base.allSatisfy(nameCharacters.contains) else { return false }
    guard parts.count == 2 else { return true }
    let tag = parts[1]
    return !tag.isEmpty && tag.allSatisfy(tagCharacters.contains)
}

/// Where a pull is: Ollama's step and the bytes done of total over the layers seen so far.
public struct OllamaPullProgress: Codable, Equatable, Sendable {
    public var status: String
    public var completed: Int64
    public var total: Int64

    public init(status: String = "", completed: Int64 = 0, total: Int64 = 0) {
        self.status = status
        self.completed = completed
        self.total = total
    }

    /// The share done, nil before the first layer (the bar is then indeterminate).
    public var fraction: Double? {
        guard total > 0 else { return nil }
        return min(max(Double(completed) / Double(total), 0), 1)
    }

    /// The bytes done, as the bar's label shows them ("1.5 GiB").
    public var completedText: String { formatBytes(completed) }

    /// The bytes over every layer seen so far.
    public var totalText: String { formatBytes(total) }
}
