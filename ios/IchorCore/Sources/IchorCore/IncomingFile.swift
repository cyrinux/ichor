import Foundation

/// Where a file opened with Ichor or sent to it goes: the import preview (a talosconfig, a
/// kubeconfig or a manifest: YAML text) or the restore flow (a backup).
public enum IncomingFileKind: Equatable, Sendable {
    case config, backup, unsupported
}

public enum IncomingFileError: Error, Equatable {
    /// The inbox or the copy did not take the protection class, so nothing was kept.
    case notProtected
}

/// Files the share extension ("Send to Ichor") leaves in the App Group for the app to take
/// on its next activation: `incoming/<uuid>.<ext>`, kept a day at most.
public enum IncomingFile {
    /// The largest file sent to Ichor: the restore flow reads backups up to this size, and a
    /// config is far smaller (the import preview takes 1 MB).
    public static let maxBytes = 5 * 1024 * 1024

    /// The largest config read, the import screen's bound too.
    public static let maxConfigBytes = 1024 * 1024

    /// How much `read` asks of a file at a time.
    private static let readChunk = 64 * 1024

    /// The App Group folder the share extension writes to.
    public static let inboxFolder = "incoming"

    /// A file older than this was never taken (the app not opened since): dropped, it may hold
    /// credentials.
    public static let maxAge: TimeInterval = 24 * 60 * 60

    /// A waiting file holds credentials: readable only while the device is unlocked, like the
    /// Keychain items it becomes.
    static let protection = FileProtectionType.complete

    /// Marks a copy still being staged; `inboxName` never starts with it.
    private static let unfinishedPrefix = "."

    /// How many bytes `classify` needs from the start of a file.
    public static let headLength = 64

    private static let backupHeaders = ["ICHORBAK", "age-encryption.org/", "-----BEGIN AGE ENCRYPTED FILE-----"].map {
        Data($0.utf8)
    }

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

    /// Copies a sent file into the inbox, unreadable while the device is locked whatever class
    /// its source had. The copy keeps a dot name until its class reads back right, so the app
    /// never takes a weaker one; on any failure it is deleted.
    @discardableResult
    public static func stage(_ source: URL, in inbox: URL, fileManager: FileManager = .default) throws -> URL {
        try fileManager.createDirectory(at: inbox, withIntermediateDirectories: true)
        try protect(inbox, fileManager: fileManager)
        let name = inboxName(for: source.lastPathComponent)
        let unfinished = inbox.appendingPathComponent(unfinishedPrefix + name)
        let staged = inbox.appendingPathComponent(name)
        defer { try? fileManager.removeItem(at: unfinished) }
        try fileManager.copyItem(at: source, to: unfinished)
        try protect(unfinished, fileManager: fileManager)
        try fileManager.moveItem(at: unfinished, to: staged)
        return staged
    }

    /// Sets `protection` and reads it back: a copy can keep its source's class, so the setter
    /// alone proves nothing. The simulator has no data protection to ask.
    private static func protect(_ url: URL, fileManager: FileManager) throws {
        #if !targetEnvironment(simulator)
            do {
                try fileManager.setAttributes([.protectionKey: protection], ofItemAtPath: url.path)
                let attributes = try fileManager.attributesOfItem(atPath: url.path)
                guard (attributes[.protectionKey] as? FileProtectionType)?.rawValue == protection.rawValue else {
                    throw IncomingFileError.notProtected
                }
            } catch {
                throw IncomingFileError.notProtected
            }
        #endif
    }

    /// The inbox's files to take, oldest first, and the ones to delete unread: older than
    /// `maxAge`, or a copy `stage` never finished.
    public static func pending(in folder: URL, now: Date = Date(), fileManager: FileManager = .default) -> (
        take: [URL], stale: [URL]
    ) {
        let keys: [URLResourceKey] = [.contentModificationDateKey, .isRegularFileKey]
        guard let urls = try? fileManager.contentsOfDirectory(at: folder, includingPropertiesForKeys: keys) else {
            return ([], [])
        }
        let files = urls.compactMap { url -> (URL, Date)? in
            guard let values = try? url.resourceValues(forKeys: Set(keys)), values.isRegularFile == true else {
                return nil
            }
            return (url, values.contentModificationDate ?? .distantPast)
        }
        let isStale = { (file: (URL, Date)) in
            file.0.lastPathComponent.hasPrefix(unfinishedPrefix) || now.timeIntervalSince(file.1) > maxAge
        }
        let stale = files.filter(isStale).map(\.0)
        let take = files.filter { !isStale($0) }.sorted { $0.1 < $1.1 }.map(\.0)
        return (take, stale)
    }

    /// The first `headLength` bytes of a file, empty when it cannot be read.
    public static func head(of url: URL) -> Data {
        guard let handle = try? FileHandle(forReadingFrom: url) else { return Data() }
        defer { try? handle.close() }
        return (try? handle.read(upToCount: headLength)) ?? Data()
    }

    /// The most bytes kept of a file of this kind; nil when it is not ours to read.
    public static func limit(for kind: IncomingFileKind) -> Int? {
        switch kind {
        case .config: return maxConfigBytes
        case .backup: return maxBytes
        case .unsupported: return nil
        }
    }

    /// A file's bytes, nil when it cannot be read or holds more than `limit`.
    public static func read(_ url: URL, limit: Int) -> Data? {
        try? readOrThrow(url, limit: limit)
    }

    /// The same read for a screen that words the two failures differently: nil when the file
    /// holds more than `limit`, a thrown error when it cannot be read.
    public static func readOrThrow(_ url: URL, limit: Int) throws -> Data? {
        let handle = try FileHandle(forReadingFrom: url)
        defer { try? handle.close() }
        let reported = try? url.resourceValues(forKeys: [.fileSizeKey]).fileSize
        return try readOrThrow(limit: limit, reportedSize: reported) { try handle.read(upToCount: $0) }
    }

    static func read(limit: Int, reportedSize: Int?, next: (Int) throws -> Data?) -> Data? {
        try? readOrThrow(limit: limit, reportedSize: reportedSize, next: next)
    }

    /// Refuses a reported size over `limit` without reading, then reads at most `limit + 1` bytes
    /// whatever was reported: a file provider can omit its size or serve more than it says. The
    /// buffer is sized up front, so growing it never allocates past that either.
    static func readOrThrow(limit: Int, reportedSize: Int?, next: (Int) throws -> Data?) throws -> Data? {
        if let reportedSize, reportedSize > limit { return nil }
        var data = Data(capacity: limit + 1)
        while data.count <= limit {
            let wanted = min(readChunk, limit + 1 - data.count)
            guard let chunk = try next(wanted), !chunk.isEmpty else { return data }
            data.append(chunk)
        }
        return nil
    }
}
