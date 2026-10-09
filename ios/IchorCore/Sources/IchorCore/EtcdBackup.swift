import Foundation

/// Member to take a snapshot from by default: a healthy follower (keeps load off the
/// leader), else any healthy voting member, else nil.
public func defaultSnapshotMember(_ statuses: [EtcdNodeStatus]) -> EtcdNodeStatus? {
    let healthy = statuses.filter { $0.error == nil && $0.errors.isEmpty && !$0.memberId.isEmpty }
    return healthy.first { !$0.isLeader && !$0.isLearner } ?? healthy.first { !$0.isLearner }
}

/// "etcd-<context>-<hostname>-<yyyyMMdd-HHmm>.snapshot", with characters that are awkward in
/// file names replaced by "-"; ".snapshot.age" when `encrypted`.
public func etcdSnapshotFilename(context: String, hostname: String, date: Date,
                                 timeZone: TimeZone = .current, encrypted: Bool = false) -> String {
    let name = "etcd-\(fileSafe(context))-\(fileSafe(hostname))-\(fileTimestamp(date, format: "yyyyMMdd-HHmm", timeZone: timeZone)).snapshot"
    return encrypted ? name + ".age" : name
}

/// How a snapshot is protected (the Linear plan document "Encrypted etcd snapshots"): age public keys (one per line:
/// age, SSH or YubiKey age1tag1 keys), an age passphrase, or nothing.
public enum SnapshotEncryption: Sendable, CustomStringConvertible {
    case keys(String)
    case passphrase(String)
    case none

    public var mode: SnapshotMode {
        switch self {
        case .keys: return .keys
        case .passphrase: return .passphrase
        case .none: return .none
        }
    }

    /// Never prints the passphrase.
    public var description: String {
        switch self {
        case .keys(let keys): return "keys(\(keys))"
        case .passphrase: return "passphrase(***)"
        case .none: return "none"
        }
    }
}

public enum SnapshotMode: String, Sendable, CaseIterable, Identifiable {
    case keys, passphrase, none

    public var id: String { rawValue }
}

/// One public key a snapshot is encrypted for, as IchorgoCheckSnapshotRecipients describes it.
public struct SnapshotRecipient: Decodable, Equatable, Sendable {
    public let type: String
    public let comment: String

    public init(type: String, comment: String) {
        self.type = type
        self.comment = comment
    }

    /// "ssh-ed25519 (admin@laptop)", or the type alone.
    public var label: String { comment.isEmpty ? type : "\(type) (\(comment))" }
}

/// Shortest passphrase Go accepts (snapshotcrypt.go minSnapshotPassphrase).
public let minSnapshotPassphrase = 12

/// Shell commands restoring `fileName` on a Unix machine: decrypt with age (unless `mode` is
/// none), check the SHA-256 of the clear snapshot, then recover the cluster with talosctl.
public func snapshotRestoreCommands(fileName: String, mode: SnapshotMode, sha256: String) -> String {
    let file = shellQuote(fileName)
    let clear = mode == .none ? file : "etcd.snapshot"
    let install = "# install age: apt install age | dnf install age | pacman -S age | brew install age"
    var lines: [String]
    switch mode {
    case .keys:
        lines = [
            install,
            "# with the private key matching one of the public keys (SSH key or age identity file):",
            "age -d -i ~/.ssh/id_ed25519 -o etcd.snapshot \(file)",
            "# YubiKey (age1tag1 key): plug it in, install age-plugin-yubikey, use its identity file:",
            "# age -d -i age-yubikey-identity-XXXXXXXX.txt -o etcd.snapshot \(file)",
        ]
    case .passphrase:
        lines = [install, "# asks for the passphrase:", "age -d -o etcd.snapshot \(file)"]
    case .none:
        lines = []
    }
    lines.append("sha256sum \(clear)  # expect \(sha256)")
    lines.append("talosctl -n <control-plane-ip> bootstrap --recover-from=./\(clear)")
    return lines.joined(separator: "\n")
}

private func shellQuote(_ text: String) -> String {
    let safe = CharacterSet.alphanumerics.union(CharacterSet(charactersIn: "._@%+=:,/-"))
    if !text.isEmpty, text.unicodeScalars.allSatisfy({ $0.isASCII && safe.contains($0) }) { return text }
    return "'" + text.replacingOccurrences(of: "'", with: "'\\''") + "'"
}

private func fileSafe(_ text: String) -> String {
    let allowed = CharacterSet.alphanumerics.union(CharacterSet(charactersIn: "._-"))
    let mapped = text.unicodeScalars.map { allowed.contains($0) ? String($0) : "-" }.joined()
    return mapped.isEmpty ? "unknown" : mapped
}

/// Progress in [0, 1] when the expected size is known, nil otherwise (indeterminate).
public func snapshotFraction(bytes: Int64, expected: Int64) -> Double? {
    guard expected > 0 else { return nil }
    return min(max(Double(bytes) / Double(expected), 0), 1)
}

/// A line of text with its 1-based number, for the machine config viewer.
public struct NumberedLine: Equatable, Identifiable, Sendable {
    public let number: Int
    public let text: String

    public var id: Int { number }

    public init(number: Int, text: String) {
        self.number = number
        self.text = text
    }
}

/// Lines containing the query (case-insensitive), keeping their original numbers.
public func filterLines(_ text: String, query: String) -> [NumberedLine] {
    var parts = text.split(separator: "\n", omittingEmptySubsequences: false)
    if parts.count > 1, parts.last?.isEmpty == true { parts.removeLast() } // trailing newline
    let lines = parts.enumerated().map { NumberedLine(number: $0.offset + 1, text: String($0.element)) }
    let needle = query.trimmingCharacters(in: .whitespaces)
    guard !needle.isEmpty else { return lines }
    return lines.filter { $0.text.range(of: needle, options: .caseInsensitive) != nil }
}
