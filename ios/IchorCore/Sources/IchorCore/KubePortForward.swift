import Foundation

// Port-forward to a pod (go/ichorgo/kube_portforward.go): the Go core listens on the phone's
// loopback address only, so the forwarded port is reachable from this phone and nowhere else.

/// A port a pod's container declares (`ports[].containerPort`).
public struct KubeContainerPort: Equatable, Hashable, Identifiable, Sendable {
    public let port: Int
    /// "http", "" when unnamed.
    public let name: String

    public var id: Int { port }

    public init(port: Int, name: String = "") {
        self.port = port
        self.name = name
    }
}

/// The TCP ports the containers of a pod declare, read from the pod's YAML (KubeObjectYAML),
/// by port, each once. Indentation-agnostic: only the keys of each `ports` item are read.
public func containerPorts(fromPodYAML yaml: String) -> [KubeContainerPort] {
    let lines = yaml.split(separator: "\n", omittingEmptySubsequences: false).map(String.init)
    var found: [Int: KubeContainerPort] = [:]
    for (index, line) in lines.enumerated() {
        guard let key = yamlKey(line), key.name == "containerPort", let port = Int(key.value), (1...65535).contains(port) else {
            continue
        }
        let fields = yamlItemFields(lines, at: index, column: key.column)
        let proto = fields["protocol"] ?? ""
        guard proto.isEmpty || proto.uppercased() == "TCP" else { continue }
        if found[port] == nil || found[port]?.name.isEmpty == true {
            found[port] = KubeContainerPort(port: port, name: unquoted(fields["name"] ?? ""))
        }
    }
    return found.values.sorted { $0.port < $1.port }
}

/// A `key: value` line: the key's column (after any "- "), its name and its value.
private func yamlKey(_ line: String) -> (column: Int, name: String, value: String, dash: Bool)? {
    var column = 0
    var rest = Substring(line)
    while rest.first == " " {
        rest = rest.dropFirst()
        column += 1
    }
    var dash = false
    if rest.hasPrefix("- ") {
        dash = true
        rest = rest.dropFirst(2)
        column += 2
        while rest.first == " " {
            rest = rest.dropFirst()
            column += 1
        }
    }
    guard let colon = rest.firstIndex(of: ":") else { return nil }
    let name = String(rest[..<colon])
    guard !name.isEmpty, !name.contains(" "), !name.hasPrefix("#") else { return nil }
    let value = rest[rest.index(after: colon)...].trimmingCharacters(in: .whitespaces)
    return (column, name, value, dash)
}

/// The scalar fields of the list item holding line `index`, whose keys sit at `column`.
private func yamlItemFields(_ lines: [String], at index: Int, column: Int) -> [String: String] {
    let start = yamlItemStart(lines, at: index, column: column)
    var fields: [String: String] = [:]
    var cursor = start
    while cursor < lines.count {
        let line = lines[cursor]
        if line.trimmingCharacters(in: .whitespaces).isEmpty { cursor += 1; continue }
        guard let key = yamlKey(line) else {
            // A nested value deeper than the keys belongs to the item; anything else ends it.
            if leadingSpaces(line) > column { cursor += 1; continue }
            break
        }
        if cursor > start && (key.column < column || (key.dash && key.column == column)) { break }
        if key.column == column { fields[key.name] = key.value }
        cursor += 1
    }
    return fields
}

/// The item's "- " line: line `index` itself, or the one above it whose dash opens the item
/// (its keys before `index` at `column`, their nested values deeper); `index` when none.
private func yamlItemStart(_ lines: [String], at index: Int, column: Int) -> Int {
    var cursor = index
    while cursor >= 0 {
        let line = lines[cursor]
        guard let key = yamlKey(line) else {
            if line.trimmingCharacters(in: .whitespaces).isEmpty || leadingSpaces(line) > column {
                cursor -= 1
                continue
            }
            return index
        }
        if key.column == column && key.dash { return cursor }
        if key.column > column || (key.column == column && !key.dash) {
            cursor -= 1
            continue
        }
        return index
    }
    return index
}

private func leadingSpaces(_ line: String) -> Int {
    line.prefix { $0 == " " }.count
}

private func unquoted(_ value: String) -> String {
    guard value.count >= 2, let first = value.first, first == value.last, first == "\"" || first == "'" else { return value }
    return String(value.dropFirst().dropLast())
}

/// The URL to open for a forward ready at `address` ("127.0.0.1:PORT"); nil when it is not
/// a loopback address with a port.
public func portForwardURL(_ address: String) -> URL? {
    let trimmed = address.trimmingCharacters(in: .whitespaces)
    guard let colon = trimmed.lastIndex(of: ":"), let port = Int(trimmed[trimmed.index(after: colon)...]),
          (1...65535).contains(port) else { return nil }
    let host = String(trimmed[..<colon])
    guard host == "127.0.0.1" || host == "localhost" || host == "[::1]" else { return nil }
    return URL(string: "http://\(host):\(port)")
}

/// A port typed by the user, nil unless 1-65535.
public func parsePort(_ text: String) -> Int? {
    guard let port = Int(text.trimmingCharacters(in: .whitespaces)), (1...65535).contains(port) else { return nil }
    return port
}
