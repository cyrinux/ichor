import Foundation

/// A ready-made debug shell command, listed by the Go core. A `run` snippet is sent with
/// Enter; the others end where the argument goes, for the user to type it.
public struct DebugSnippet: Decodable, Equatable, Identifiable, Sendable {
    public let group: String
    public let label: String
    public let command: String
    public let run: Bool

    public var id: String { group + "/" + label }

    public init(group: String, label: String, command: String, run: Bool) {
        self.group = group
        self.label = label
        self.command = command
        self.run = run
    }

    /// The bytes to send to the TTY: Enter is a carriage return, as a keyboard sends it.
    public var bytes: [UInt8] { Array((run ? command + "\r" : command).utf8) }

    /// The command as listed: a typed one shows … where its argument goes.
    public var display: String {
        run ? command : command.trimmingCharacters(in: .whitespaces) + " …"
    }
}

/// Snippets sectioned by group, in the core's order.
public func groupedDebugSnippets(_ snippets: [DebugSnippet]) -> [(group: String, snippets: [DebugSnippet])] {
    snippets.reduce(into: []) { sections, snippet in
        if sections.last?.group == snippet.group {
            sections[sections.count - 1].snippets.append(snippet)
        } else {
            sections.append((snippet.group, [snippet]))
        }
    }
}
