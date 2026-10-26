import CryptoKit
import Foundation
import Ichorgo
import IchorCore
import Network
import Observation

/// A model the app can download (Go LocalModelCatalog): pinned by size and SHA-256.
struct LocalModelInfo: Decodable, Identifiable, Sendable {
    let id: String
    let name: String
    let recommended: Bool?
    let url: String
    let bytes: Int64
    let sha256: String
    let license: String
    let licenseUrl: String
    /// iOS gives an app about half of the phone's memory: its own minimum.
    let minMemoryMiBIOS: Int64
}

/// The model files on the phone, in Application Support/models (out of backups): downloads
/// as <id>.gguf, imports under imported/. Thread-safe: the engine reads it off the main thread.
final class LocalModelStore: Sendable {
    static let shared = LocalModelStore()
    static let importedPrefix = "file:"

    /// The downloadable models; empty when this version pins none.
    let catalog: [LocalModelInfo]

    private init() {
        let json = IchorgoLocalModelCatalog(nil)
        catalog = (try? TalosJSON.decode([LocalModelInfo].self, from: json)) ?? []
    }

    var directory: URL? { try? AppSupport.excludedFolder("models") }

    func info(_ id: String) -> LocalModelInfo? { catalog.first { $0.id == id } }

    /// Where the model `id` lives once complete (nil: an id this version does not know).
    func target(for id: String) -> URL? {
        guard let directory else { return nil }
        if id.hasPrefix(Self.importedPrefix) {
            guard let name = Self.safeName(String(id.dropFirst(Self.importedPrefix.count))) else { return nil }
            return directory.appendingPathComponent("imported", isDirectory: true).appendingPathComponent(name)
        }
        return info(id) == nil ? nil : directory.appendingPathComponent("\(id).gguf")
    }

    /// The model's file when it is on the phone, complete.
    func file(for id: String) -> URL? {
        guard let url = target(for: id),
              let size = try? url.resourceValues(forKeys: [.fileSizeKey]).fileSize, size > 0 else { return nil }
        return url
    }

    func imported() -> [String] {
        guard let folder = directory?.appendingPathComponent("imported", isDirectory: true),
              let names = try? FileManager.default.contentsOfDirectory(atPath: folder.path) else { return [] }
        return names.filter { !$0.hasPrefix(".") }.sorted().map { Self.importedPrefix + $0 }
    }

    func delete(_ id: String) {
        if let url = target(for: id) { try? FileManager.default.removeItem(at: url) }
    }

    /// The phone's memory in MiB, as the catalog's iOS minimums count it.
    var memoryMiB: Int64 { Int64(ProcessInfo.processInfo.physicalMemory / 1_048_576) }

    func fits(_ model: LocalModelInfo) -> Bool { memoryMiB >= model.minMemoryMiBIOS }

    /// Free space for a download, as iOS counts it for something the user asked for.
    var freeBytes: Int64 {
        guard let directory,
              let values = try? directory.resourceValues(forKeys: [.volumeAvailableCapacityForImportantUsageKey]) else { return 0 }
        return values.volumeAvailableCapacityForImportantUsage ?? 0
    }

    /// Copies a model file the user picked (Files) into the app; returns its id. Fails on a
    /// file that is not a GGUF model.
    func importFile(from source: URL) throws -> String {
        let scoped = source.startAccessingSecurityScopedResource()
        defer { if scoped { source.stopAccessingSecurityScopedResource() } }
        guard let name = Self.safeName(source.lastPathComponent), let directory else {
            throw TalosError(message: String(localized: "This file name cannot be used."))
        }
        let handle = try FileHandle(forReadingFrom: source)
        let magic = try handle.read(upToCount: 4)
        try handle.close()
        guard magic == Data("GGUF".utf8) else { throw TalosError(message: String(localized: "This is not a GGUF model file.")) }
        let folder = directory.appendingPathComponent("imported", isDirectory: true)
        try FileManager.default.createDirectory(at: folder, withIntermediateDirectories: true)
        let target = folder.appendingPathComponent(name)
        try? FileManager.default.removeItem(at: target)
        try FileManager.default.copyItem(at: source, to: target)
        return Self.importedPrefix + name
    }

    /// A file name without a path, or nil when nothing usable is left.
    static func safeName(_ name: String) -> String? {
        let last = name.split(separator: "/").last.map(String.init) ?? ""
        let allowed = CharacterSet(charactersIn: "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789._ -")
        let cleaned = String(last.unicodeScalars.map { allowed.contains($0) ? Character($0) : "_" })
            .trimmingCharacters(in: .whitespaces)
        let trimmed = String(cleaned.drop { $0 == "." })
        return trimmed.isEmpty ? nil : trimmed
    }
}

/// The state of model downloads, for the settings screen. Downloads run in a background
/// URLSession (ModelDownloadSession), so they go on while the app is suspended.
@MainActor
@Observable
final class ModelDownloads {
    static let shared = ModelDownloads()

    enum Phase: Equatable {
        case downloading(done: Int64, allowMetered: Bool)
        case verifying
        case failed(String)
    }

    private(set) var phases: [String: Phase] = [:]
    /// Bumped when a model file appears or goes, so screens read the files again.
    private(set) var version = 0

    func set(_ id: String, _ phase: Phase?) { phases[id] = phase }

    func changed() { version += 1 }

    /// Starts the download the user tapped: on Wi-Fi unless `allowMetered`, never in Low Data Mode.
    func start(_ model: LocalModelInfo, allowMetered: Bool) {
        guard LocalModelStore.shared.freeBytes > model.bytes * 11 / 10 else {
            phases[model.id] = .failed(String(localized: "Not enough free space on the phone for this model."))
            return
        }
        phases[model.id] = .downloading(done: 0, allowMetered: allowMetered)
        ModelDownloadSession.shared.start(model, allowMetered: allowMetered)
    }

    func cancel(_ id: String) {
        ModelDownloadSession.shared.cancel(id)
        phases[id] = nil
    }

    func delete(_ id: String) {
        LocalModelHost.shared.unload()
        LocalModelStore.shared.delete(id)
        changed()
    }
}

/// The background URLSession that downloads models. The session allows any network; each
/// request narrows it: no expensive network (cellular, hotspot) unless the user allowed it
/// for this download, never a constrained one (Low Data Mode). A background task waits for
/// a network that qualifies, and goes on by itself when one comes back.
final class ModelDownloadSession: NSObject, URLSessionDownloadDelegate, @unchecked Sendable {
    static let shared = ModelDownloadSession()
    static let identifier = "name.levis.ichor.models"

    /// Set by the app delegate when iOS relaunches the app for the session's events.
    nonisolated(unsafe) static var backgroundCompletion: (() -> Void)?

    private var session: URLSession!

    override private init() {
        super.init()
        let config = URLSessionConfiguration.background(withIdentifier: Self.identifier)
        config.isDiscretionary = false // the user asked for it now
        config.sessionSendsLaunchEvents = true
        config.allowsConstrainedNetworkAccess = false
        session = URLSession(configuration: config, delegate: self, delegateQueue: nil)
    }

    /// Reconnects to downloads started before the app was relaunched.
    func resume() {
        session.getAllTasks { tasks in
            for task in tasks {
                guard let job = Self.parse(task.taskDescription) else { continue }
                let done = task.countOfBytesReceived
                Task { @MainActor in ModelDownloads.shared.set(job.id, .downloading(done: done, allowMetered: job.metered)) }
            }
        }
    }

    func start(_ model: LocalModelInfo, allowMetered: Bool) {
        guard let url = URL(string: model.url) else { return }
        let task: URLSessionDownloadTask
        // A resumed task keeps its first request's network rules: one that was allowed mobile
        // data is only resumed when this download allows it too.
        if let data = Self.resumeData(for: model.id), allowMetered || !Self.resumeWasMetered(model.id) {
            task = session.downloadTask(withResumeData: data)
        } else {
            var request = URLRequest(url: url)
            request.allowsExpensiveNetworkAccess = allowMetered
            request.allowsCellularAccess = allowMetered
            request.allowsConstrainedNetworkAccess = false
            task = session.downloadTask(with: request)
        }
        Self.clearResumeData(for: model.id)
        task.taskDescription = model.id + (allowMetered ? "|metered" : "")
        task.countOfBytesClientExpectsToReceive = model.bytes
        task.resume()
    }

    func cancel(_ id: String) {
        session.getAllTasks { tasks in
            tasks.filter { Self.parse($0.taskDescription)?.id == id }.forEach { $0.cancel() }
        }
        Self.clearResumeData(for: id)
    }

    // MARK: URLSessionDownloadDelegate

    func urlSession(_ session: URLSession, downloadTask: URLSessionDownloadTask, didWriteData bytesWritten: Int64,
                    totalBytesWritten: Int64, totalBytesExpectedToWrite: Int64) {
        guard let job = Self.parse(downloadTask.taskDescription) else { return }
        Task { @MainActor in ModelDownloads.shared.set(job.id, .downloading(done: totalBytesWritten, allowMetered: job.metered)) }
    }

    func urlSession(_ session: URLSession, downloadTask: URLSessionDownloadTask, didFinishDownloadingTo location: URL) {
        guard let id = Self.parse(downloadTask.taskDescription)?.id,
              let model = LocalModelStore.shared.info(id),
              let target = LocalModelStore.shared.target(for: id) else { return }
        if let http = downloadTask.response as? HTTPURLResponse, !(200...299).contains(http.statusCode) {
            Task { @MainActor in ModelDownloads.shared.set(id, .failed(String(localized: "The download failed. Try again."))) }
            return
        }
        // The file is gone once this returns: move it first, check it after.
        let part = target.appendingPathExtension("part")
        try? FileManager.default.removeItem(at: part)
        do {
            try FileManager.default.moveItem(at: location, to: part)
        } catch {
            Task { @MainActor in ModelDownloads.shared.set(id, .failed(error.localizedDescription)) }
            return
        }
        Task { @MainActor in ModelDownloads.shared.set(id, .verifying) }
        DispatchQueue.global(qos: .utility).async {
            let ok = Self.matches(part, model)
            if ok {
                try? FileManager.default.removeItem(at: target)
                try? FileManager.default.moveItem(at: part, to: target)
            } else {
                try? FileManager.default.removeItem(at: part)
            }
            Task { @MainActor in
                ModelDownloads.shared.set(id, ok ? nil : .failed(String(localized: "The downloaded file is not the expected one and was deleted. Try again.")))
                ModelDownloads.shared.changed()
            }
        }
    }

    func urlSession(_ session: URLSession, task: URLSessionTask, didCompleteWithError error: (any Error)?) {
        guard let error, let job = Self.parse(task.taskDescription) else { return }
        let id = job.id
        let nsError = error as NSError
        if let data = nsError.userInfo[NSURLSessionDownloadTaskResumeData] as? Data {
            Self.storeResumeData(data, for: id, metered: job.metered)
        }
        // Cancelled by the user: nothing to say.
        guard nsError.code != NSURLErrorCancelled else { return }
        Task { @MainActor in ModelDownloads.shared.set(id, .failed(String(localized: "The download failed. Try again."))) }
    }

    func urlSessionDidFinishEvents(forBackgroundURLSession session: URLSession) {
        DispatchQueue.main.async {
            Self.backgroundCompletion?()
            Self.backgroundCompletion = nil
        }
    }

    // MARK: Helpers

    /// The model a task downloads, and whether mobile data was allowed ("<id>|metered").
    private static func parse(_ description: String?) -> (id: String, metered: Bool)? {
        guard let description, let first = description.split(separator: "|").first else { return nil }
        return (String(first), description.hasSuffix("|metered"))
    }

    private static func resumeURL(for id: String) -> URL? {
        LocalModelStore.shared.directory?.appendingPathComponent("\(id).resume")
    }

    static func resumeData(for id: String) -> Data? { resumeURL(for: id).flatMap { try? Data(contentsOf: $0) } }

    private static func storeResumeData(_ data: Data, for id: String, metered: Bool) {
        guard let url = resumeURL(for: id) else { return }
        try? data.write(to: url)
        let marker = url.appendingPathExtension("metered")
        if metered { try? Data().write(to: marker) } else { try? FileManager.default.removeItem(at: marker) }
    }

    private static func resumeWasMetered(_ id: String) -> Bool {
        resumeURL(for: id).map { FileManager.default.fileExists(atPath: $0.appendingPathExtension("metered").path) } ?? false
    }

    private static func clearResumeData(for id: String) {
        guard let url = resumeURL(for: id) else { return }
        try? FileManager.default.removeItem(at: url)
        try? FileManager.default.removeItem(at: url.appendingPathExtension("metered"))
    }

    /// The file has the catalog's size and SHA-256.
    private static func matches(_ file: URL, _ model: LocalModelInfo) -> Bool {
        guard let handle = try? FileHandle(forReadingFrom: file) else { return false }
        defer { try? handle.close() }
        var hasher = SHA256()
        var size: Int64 = 0
        while let chunk = try? handle.read(upToCount: 1 << 20), !chunk.isEmpty {
            hasher.update(data: chunk)
            size += Int64(chunk.count)
        }
        let digest = hasher.finalize().map { String(format: "%02x", $0) }.joined()
        return size == model.bytes && digest == model.sha256.lowercased()
    }
}

/// Why a download is not moving right now, from the network the phone is on.
enum DownloadWait {
    case wifi, lowDataMode

    static func current(allowMetered: Bool) -> DownloadWait? {
        let path = MeteredNetwork.shared.path
        if path.isConstrained { return .lowDataMode }
        if path.isExpensive, !allowMetered { return .wifi }
        return nil
    }
}
