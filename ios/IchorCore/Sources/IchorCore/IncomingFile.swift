import Foundation

/// Where a file opened with Ichor or sent to it goes: the import preview (a talosconfig, a
/// kubeconfig or a manifest: YAML text) or the restore flow (a backup).
public enum IncomingFileKind: Equatable, Sendable {
    case config, backup, unsupported
}

/// Files the share extension ("Send to Ichor") leaves in the App Group for the app to take
/// on its next activation: `incoming/<uuid>.<ext>`, kept a day at most.
public enum IncomingFile {
    /// The largest file sent to Ichor: the restore flow reads backups up to this size, and a
    /// config is far smaller (the import preview takes 1 MB).
    public static let maxBytes = 5 * 1024 * 1024

    /// The App Group folder the share extension writes to.
    public static let inboxFolder = "incoming"

    /// A file older than this was never taken (the app not opened since): dropped, it may hold
    /// credentials.
    public static let maxAge: TimeInterval = 24 * 60 * 60

    /// How many bytes `classify` needs from the start of a file.
    public static let headLength = 64

    private static let backupHeaders = ["ICHORBAK", "age-encryption.org/", "-----BEGIN AGE ENCRYPTED FILE-----"].map { Data($0.utf8) }

    /// By the name's extension first (`.ichorbackup`, `.age`), then by the first bytes: a
    /// backup's header, else text (a config); anything else (a PDF, an image) is not ours.
    public static func classify(name: String, head: Data) -> IncomingFileKind {
        let ext = URL(fileURLWithPath: name).pathExtension.lowercased()
        if ext == backupExtension || ext == backupAgeExtension { return .backup }
        if backupHeaders.contains(where: { head.starts(with: $0) }) { return .backup }
        return looksLikeText(head) ? .config : .unsupported
    }

    /// No NUL byte, and UTF-8 up to a character the head may have cut in two.
    static func looksLikeText(_ head: Data) -> Bool {
        guard !head.isEmpty, !head.contains(0) else { return false }
        for cut in 0...min(3, head.count - 1) where String(data: head.dropLast(cut), encoding: .utf8) != nil {
            return true
        }
        return false
    }

    /// The inbox name of a file sent as `name`: a fresh UUID with the name's extension (letters
    /// and digits only, so a sent name never reaches a path).
    public static func inboxName(for name: String, id: UUID = UUID()) -> String {
        let ext = URL(fileURLWithPath: name).pathExtension.lowercased()
        let safe = ext.count <= 16 && !ext.isEmpty && ext.allSatisfy { $0.isASCII && ($0.isLetter || $0.isNumber) }
        return safe ? "\(id.uuidString).\(ext)" : id.uuidString
    }

    /// The inbox's files to take, oldest first, and the stale ones (older than `maxAge`) to
    /// delete unread.
    public static func pending(in folder: URL, now: Date = Date(), fileManager: FileManager = .default) -> (take: [URL], stale: [URL]) {
        let keys: [URLResourceKey] = [.contentModificationDateKey, .isRegularFileKey]
        guard let urls = try? fileManager.contentsOfDirectory(at: folder, includingPropertiesForKeys: keys) else { return ([], []) }
        let files = urls.compactMap { url -> (URL, Date)? in
            guard let values = try? url.resourceValues(forKeys: Set(keys)), values.isRegularFile == true else { return nil }
            return (url, values.contentModificationDate ?? .distantPast)
        }
        let stale = files.filter { now.timeIntervalSince($0.1) > maxAge }.map(\.0)
        let take = files.filter { now.timeIntervalSince($0.1) <= maxAge }.sorted { $0.1 < $1.1 }.map(\.0)
        return (take, stale)
    }

    /// The first `headLength` bytes of a file, empty when it cannot be read.
    public static func head(of url: URL) -> Data {
        guard let handle = try? FileHandle(forReadingFrom: url) else { return Data() }
        defer { try? handle.close() }
        return (try? handle.read(upToCount: headLength)) ?? Data()
    }
}
