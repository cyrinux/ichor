import Foundation

/// Where the cursor of the YAML editor is, as the field path the schema help (KubeExplain)
/// walks: "spec.template.spec.containers.image". List items are not in the path: the schema
/// walks an array through its items.
///
/// Read from indentation alone, line by line upwards, so it works on YAML that does not parse
/// yet. Limits: block-style YAML only (flow style `{a: 1}` / `[a]` is one value), the current
/// document only (stops at `---`), and a line inside a multi-line string (`|`, `>`) reads as
/// if it were a key.
public struct YamlCursor: Equatable, Sendable {
    /// The keys of the maps holding the cursor's line, outermost first.
    public let parents: [String]
    /// The key on the cursor's line, nil on a blank line or a list scalar ("- a").
    public let key: String?
    /// The key's column, or the cursor's column on a blank line.
    public let column: Int
    /// The line's key opens a block ("spec:"): fields are added inside it.
    public let opensBlock: Bool

    public init(parents: [String], key: String?, column: Int, opensBlock: Bool) {
        self.parents = parents
        self.key = key
        self.column = column
        self.opensBlock = opensBlock
    }

    /// What "Explain field" describes: the key's path, or the map holding a blank line.
    public var fieldPath: String { (parents + [key].compactMap { $0 }).joined(separator: ".") }

    /// The map whose fields "Add field" lists: the block the line opens, else the one holding it.
    public var addPath: String { opensBlock ? fieldPath : parents.joined(separator: ".") }

    /// The column a field added by "Add field" starts at.
    public var addColumn: Int { opensBlock ? column + 2 : column }
}

/// The schema help's answer for one field (KubeExplain).
public struct KubeExplain: Decodable, Equatable, Sendable {
    public let path: String
    public let type: String
    public let format: String
    public let description: String
    public let enumValues: [String]
    public let required: Bool
    public let children: [KubeExplainChild]

    public init(path: String = "", type: String = "", format: String = "", description: String = "",
                enumValues: [String] = [], required: Bool = false, children: [KubeExplainChild] = []) {
        self.path = path
        self.type = type
        self.format = format
        self.description = description
        self.enumValues = enumValues
        self.required = required
        self.children = children
    }

    private enum CodingKeys: String, CodingKey {
        case path, type, format, description, required, children
        case enumValues = "enum"
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        path = try c.field(.path, "")
        type = try c.field(.type, "")
        format = try c.field(.format, "")
        description = try c.field(.description, "")
        enumValues = try c.field(.enumValues, [String]())
        required = try c.field(.required, false)
        children = try c.field(.children, [KubeExplainChild]())
    }

    /// An array: a field added to it is a new list item.
    public var isList: Bool { type.hasPrefix("[]") }
}

/// One field of the object explained, its description cut to the first sentence.
public struct KubeExplainChild: Decodable, Equatable, Identifiable, Sendable {
    public let name: String
    public let type: String
    public let description: String
    public let required: Bool
    public let enumValues: [String]

    public var id: String { name }

    public init(name: String, type: String = "", description: String = "", required: Bool = false, enumValues: [String] = []) {
        self.name = name
        self.type = type
        self.description = description
        self.required = required
        self.enumValues = enumValues
    }

    private enum CodingKeys: String, CodingKey {
        case name, type, description, required
        case enumValues = "enum"
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        name = try c.field(.name, "")
        type = try c.field(.type, "")
        description = try c.field(.description, "")
        required = try c.field(.required, false)
        enumValues = try c.field(.enumValues, [String]())
    }
}

/// The start of the Go error when the cluster has no schema help to give.
public let schemaHelpUnavailablePrefix = "schema help unavailable"

/// One line of YAML read for its structure: the columns of its list dashes and its key.
private struct YamlLine {
    let dashes: [Int]
    let key: String?
    let column: Int
    let value: String

    /// A key whose value is the block below it, not a scalar on the line.
    var opensBlock: Bool {
        guard key != nil else { return false }
        guard let first = value.first else { return true }
        return "|>&!".contains(first)
    }
}

private func parseYamlLine(_ text: Substring) -> YamlLine? {
    let line = Array(text)
    var i = 0
    while i < line.count && line[i] == " " { i += 1 }
    if i >= line.count || line[i] == "#" { return nil }
    var dashes: [Int] = []
    while i < line.count && line[i] == "-" && (i + 1 == line.count || line[i + 1] == " ") {
        dashes.append(i)
        i += 1
        while i < line.count && line[i] == " " { i += 1 }
    }
    if i >= line.count || line[i] == "#" { return YamlLine(dashes: dashes, key: nil, column: i, value: "") }
    guard let end = keyEnd(line, i) else {
        return YamlLine(dashes: dashes, key: nil, column: i, value: String(line[i...]))
    }
    var key = String(line[i..<end]).trimmingCharacters(in: .whitespaces)
    for quote in ["\"", "'"] where key.count >= 2 && key.hasPrefix(quote) && key.hasSuffix(quote) {
        key = String(key.dropFirst().dropLast())
    }
    var value = String(line[(end + 1)...]).trimmingCharacters(in: .whitespaces)
    if value.hasPrefix("#") { value = "" }
    return YamlLine(dashes: dashes, key: key, column: i, value: value)
}

/// The index of the colon ending the key starting at `start`, nil when the line has none.
private func keyEnd(_ line: [Character], _ start: Int) -> Int? {
    let first = line[start]
    if "{[|>&*!%@`".contains(first) { return nil }
    if first == "\"" || first == "'" {
        guard let close = line[(start + 1)...].firstIndex(of: first),
              close + 1 < line.count, line[close + 1] == ":" else { return nil }
        let colon = close + 1
        return colon + 1 == line.count || line[colon + 1] == " " ? colon : nil
    }
    for j in start..<line.count {
        if line[j] == "#" && j > start && line[j - 1] == " " { return nil }
        if line[j] == ":" && (j + 1 == line.count || line[j + 1] == " ") { return j }
    }
    return nil
}

/// The lines of `text` (CR dropped), the index of the one holding `utf16Offset` and the
/// cursor's column in it.
private func yamlLines(_ text: String, _ utf16Offset: Int) -> (lines: [Substring], index: Int, column: Int, offset: Int) {
    let offset = max(0, min(utf16Offset, text.utf16.count))
    // "\r\n" is one Character in Swift: split on it too, so lines match the "\n" units counted below.
    let lines = text.split(omittingEmptySubsequences: false) { $0 == "\n" || $0 == "\r\n" }.map { line -> Substring in
        line.hasSuffix("\r") ? line.dropLast() : line
    }
    let before = text.utf16.prefix(offset)
    let index = min(before.reduce(0) { $1 == 10 ? $0 + 1 : $0 }, lines.count - 1)
    let column = before.reversed().prefix { $0 != 10 }.count
    return (lines, index, column, offset)
}

/// The cursor at `utf16Offset` (as UITextView's selectedRange counts) of `text`.
public func yamlCursorAt(_ text: String, utf16Offset: Int) -> YamlCursor {
    let (lines, lineIndex, cursorColumn, _) = yamlLines(text, utf16Offset)
    let current = parseYamlLine(lines[lineIndex])

    var limit: Int
    var afterDash = false
    if let current {
        limit = current.column
        if let dash = current.dashes.first {
            limit = dash
            afterDash = true
        }
    } else {
        // A blank line counts from the cursor's column, a comment from its indentation.
        let raw = lines[lineIndex]
        limit = raw.allSatisfy({ $0 == " " || $0 == "\t" }) ? cursorColumn : raw.prefix { $0 == " " }.count
    }
    let column = current?.column ?? limit

    var parents: [String] = []
    var i = lineIndex - 1
    while i >= 0 {
        defer { i -= 1 }
        if lines[i].hasPrefix("---") || lines[i].hasPrefix("...") { break }
        if limit == 0 && !afterDash { break }
        guard let line = parseYamlLine(lines[i]) else { continue }
        if let key = line.key, line.opensBlock, afterDash ? line.column <= limit : line.column < limit {
            parents.insert(key, at: 0)
            limit = line.column
            afterDash = false
        }
        if let dash = line.dashes.first, dash < limit {
            limit = dash
            afterDash = true
        }
    }
    return YamlCursor(parents: parents, key: current?.key, column: column, opensBlock: current?.opensBlock ?? false)
}

/// `text` with `name: ` added where "Add field" adds at `utf16Offset`: on the cursor's line
/// when blank, else on a new line below it; as a list item ("- name: ") when `listItem`.
/// Returns the text and the cursor (a UTF-16 offset) after the inserted colon.
public func insertYamlField(_ text: String, utf16Offset: Int, name: String, listItem: Bool) -> (text: String, cursor: Int) {
    let cursor = yamlCursorAt(text, utf16Offset: utf16Offset)
    let units = Array(text.utf16)
    let offset = max(0, min(utf16Offset, units.count))
    var lineStart = offset
    while lineStart > 0 && units[lineStart - 1] != 10 { lineStart -= 1 }
    var lineEnd = offset
    while lineEnd < units.count && units[lineEnd] != 10 { lineEnd += 1 }
    let entry = String(repeating: " ", count: cursor.addColumn) + (listItem ? "- " : "") + "\(name): "
    let head = String(decoding: units[..<lineStart], as: UTF16.self)
    let line = String(decoding: units[lineStart..<lineEnd], as: UTF16.self)
    let tail = String(decoding: units[lineEnd...], as: UTF16.self)
    if line.allSatisfy({ $0 == " " || $0 == "\t" || $0 == "\r" }) {
        return (head + entry + tail, lineStart + entry.utf16.count)
    }
    return (head + line + "\n" + entry + tail, lineEnd + 1 + entry.utf16.count)
}
