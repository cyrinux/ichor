import XCTest
@testable import IchorCore

final class IncomingFileTests: XCTestCase {
    private let kubeconfig = Data("apiVersion: v1\nkind: Config\nclusters: []\n".utf8)

    func testBackupsByExtension() {
        XCTAssertEqual(IncomingFile.classify(name: "ichor-2026-10-02.ichorbackup", head: Data([0x01, 0x02])), .backup)
        XCTAssertEqual(IncomingFile.classify(name: "ichor-2026-10-02.AGE", head: Data()), .backup)
    }

    func testBackupsByHeader() {
        XCTAssertEqual(IncomingFile.classify(name: "attachment", head: Data("ICHORBAK\u{0}\u{1}".utf8)), .backup)
        XCTAssertEqual(
            IncomingFile.classify(name: "file.bin", head: Data("age-encryption.org/v1\n-> scrypt".utf8)), .backup)
        XCTAssertEqual(
            IncomingFile.classify(name: "backup.txt", head: Data("-----BEGIN AGE ENCRYPTED FILE-----\n".utf8)), .backup)
    }

    func testConfigsAreText() {
        XCTAssertEqual(IncomingFile.classify(name: "config", head: kubeconfig), .config)
        XCTAssertEqual(IncomingFile.classify(name: "talosconfig.yaml", head: Data("context: demo\n".utf8)), .config)
        XCTAssertEqual(IncomingFile.classify(name: "prod.kubeconfig", head: kubeconfig), .config)
    }

    func testAHeadCutInsideACharacterIsStillText() {
        // "é" is two bytes: the head ends after the first.
        var head = Data("name: caf".utf8)
        head.append(0xC3)
        XCTAssertEqual(IncomingFile.classify(name: "x.yaml", head: head), .config)
    }

    func testOtherFilesAreRefused() {
        XCTAssertEqual(
            IncomingFile.classify(name: "report.pdf", head: Data([0x25, 0x50, 0x44, 0x46, 0x2D, 0x00, 0xFF])),
            .unsupported)
        XCTAssertEqual(
            IncomingFile.classify(
                name: "photo.png", head: Data([0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00])), .unsupported)
        XCTAssertEqual(IncomingFile.classify(name: "empty.yaml", head: Data()), .unsupported)
        XCTAssertEqual(IncomingFile.classify(name: "latin1", head: Data([0x61, 0xFF, 0xFE, 0xFD, 0x62])), .unsupported)
    }

    func testInboxNamesKeepOnlyASafeExtension() throws {
        let id = try XCTUnwrap(UUID(uuidString: "00000000-0000-0000-0000-000000000001"))
        XCTAssertEqual(IncomingFile.inboxName(for: "prod.KubeConfig", id: id), "\(id.uuidString).kubeconfig")
        XCTAssertEqual(IncomingFile.inboxName(for: "config", id: id), id.uuidString)
        XCTAssertEqual(IncomingFile.inboxName(for: "../../evil.ya ml", id: id), id.uuidString)
        XCTAssertEqual(IncomingFile.inboxName(for: "x." + String(repeating: "a", count: 17), id: id), id.uuidString)
    }

    func testPendingTakesFreshFilesOldestFirstAndDropsStaleOnes() throws {
        let folder = FileManager.default.temporaryDirectory.appendingPathComponent("incoming-\(UUID().uuidString)")
        try FileManager.default.createDirectory(at: folder, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(at: folder) }

        let now = Date(timeIntervalSince1970: 1_800_000_000)
        func file(_ name: String, age: TimeInterval) throws -> URL {
            let url = folder.appendingPathComponent(name)
            try Data(name.utf8).write(to: url)
            try FileManager.default.setAttributes(
                [.modificationDate: now.addingTimeInterval(-age)], ofItemAtPath: url.path)
            return url
        }
        let newer = try file("b.yaml", age: 60)
        let older = try file("a.yaml", age: 3600)
        let stale = try file("c.age", age: IncomingFile.maxAge + 1)
        try FileManager.default.createDirectory(
            at: folder.appendingPathComponent("sub"), withIntermediateDirectories: true)

        let pending = IncomingFile.pending(in: folder, now: now)
        XCTAssertEqual(pending.take.map(\.lastPathComponent), [older.lastPathComponent, newer.lastPathComponent])
        XCTAssertEqual(pending.stale.map(\.lastPathComponent), [stale.lastPathComponent])
    }

    func testStageProtectsTheFolderAndTheFileBeforeNamingIt() throws {
        let (source, inbox) = try stagingFolders()
        let fileManager = ProtectionFileManager()

        let staged = try IncomingFile.stage(source, in: inbox, fileManager: fileManager)

        XCTAssertEqual(try Data(contentsOf: staged), kubeconfig)
        XCTAssertEqual(staged.deletingLastPathComponent().path, inbox.path)
        XCTAssertEqual(staged.pathExtension, "yaml")
        XCTAssertFalse(staged.lastPathComponent.hasPrefix("."))
        XCTAssertEqual(fileManager.protected.count, 2)
        XCTAssertEqual(fileManager.protected.first, inbox.path)
        XCTAssertTrue(fileManager.protected.last?.hasPrefix(inbox.path + "/.") == true)
        XCTAssertEqual(try FileManager.default.contentsOfDirectory(atPath: inbox.path), [staged.lastPathComponent])
    }

    func testStageLeavesNoCopyWhenTheProtectionCannotBeSet() throws {
        let (source, inbox) = try stagingFolders()
        let fileManager = ProtectionFileManager()
        fileManager.failSetOnFiles = true

        XCTAssertThrowsError(try IncomingFile.stage(source, in: inbox, fileManager: fileManager)) {
            XCTAssertEqual($0 as? IncomingFileError, .notProtected)
        }
        XCTAssertEqual(try FileManager.default.contentsOfDirectory(atPath: inbox.path), [])
    }

    func testStageLeavesNoCopyWhenTheFileReadsBackAWeakerClass() throws {
        let (source, inbox) = try stagingFolders()
        let fileManager = ProtectionFileManager()
        fileManager.fileClassReadBack = .completeUntilFirstUserAuthentication

        XCTAssertThrowsError(try IncomingFile.stage(source, in: inbox, fileManager: fileManager)) {
            XCTAssertEqual($0 as? IncomingFileError, .notProtected)
        }
        XCTAssertEqual(try FileManager.default.contentsOfDirectory(atPath: inbox.path), [])
    }

    func testStageCopiesNothingWhenTheFolderCannotBeProtected() throws {
        let (source, inbox) = try stagingFolders()
        let fileManager = ProtectionFileManager()
        fileManager.failSetOnFolders = true

        XCTAssertThrowsError(try IncomingFile.stage(source, in: inbox, fileManager: fileManager)) {
            XCTAssertEqual($0 as? IncomingFileError, .notProtected)
        }
        XCTAssertEqual(fileManager.protected, [])
        XCTAssertEqual(try FileManager.default.contentsOfDirectory(atPath: inbox.path), [])
    }

    func testPendingNeverTakesAnUnfinishedCopyAndDropsIt() throws {
        let folder = FileManager.default.temporaryDirectory.appendingPathComponent("incoming-\(UUID().uuidString)")
        try FileManager.default.createDirectory(at: folder, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(at: folder) }
        try kubeconfig.write(to: folder.appendingPathComponent(".unfinished.yaml"))
        try kubeconfig.write(to: folder.appendingPathComponent("sent.partial"))

        let pending = IncomingFile.pending(in: folder)

        XCTAssertEqual(pending.take.map(\.lastPathComponent), ["sent.partial"])
        XCTAssertEqual(pending.stale.map(\.lastPathComponent), [".unfinished.yaml"])
    }

    private func stagingFolders() throws -> (source: URL, inbox: URL) {
        let root = FileManager.default.temporaryDirectory.appendingPathComponent("stage-\(UUID().uuidString)")
        try FileManager.default.createDirectory(at: root, withIntermediateDirectories: true)
        addTeardownBlock { try? FileManager.default.removeItem(at: root) }
        let source = root.appendingPathComponent("config.yaml")
        try kubeconfig.write(to: source)
        return (source, root.appendingPathComponent("incoming", isDirectory: true))
    }

    func testPendingOfAMissingFolderIsEmpty() {
        let pending = IncomingFile.pending(in: URL(fileURLWithPath: "/nonexistent-\(UUID().uuidString)"))
        XCTAssertTrue(pending.take.isEmpty && pending.stale.isEmpty)
    }

    func testHeadReadsTheFirstBytes() throws {
        let url = FileManager.default.temporaryDirectory.appendingPathComponent("head-\(UUID().uuidString)")
        defer { try? FileManager.default.removeItem(at: url) }
        try Data(repeating: 0x41, count: 200).write(to: url)
        XCTAssertEqual(IncomingFile.head(of: url).count, IncomingFile.headLength)
        XCTAssertEqual(IncomingFile.head(of: url.appendingPathExtension("missing")), Data())
    }
}

/// Stands in for data protection, which only a device has: records the class set on each path and
/// reads it back, or fails as told.
private final class ProtectionFileManager: FileManager, @unchecked Sendable {
    var failSetOnFiles = false
    var failSetOnFolders = false
    var fileClassReadBack: FileProtectionType?
    private(set) var protected: [String] = []
    private var classes: [String: FileProtectionType] = [:]

    override func setAttributes(_ attributes: [FileAttributeKey: Any], ofItemAtPath path: String) throws {
        var isFolder: ObjCBool = false
        _ = fileExists(atPath: path, isDirectory: &isFolder)
        if isFolder.boolValue ? failSetOnFolders : failSetOnFiles { throw CocoaError(.fileWriteNoPermission) }
        classes[path] = attributes[.protectionKey] as? FileProtectionType
    }

    override func attributesOfItem(atPath path: String) throws -> [FileAttributeKey: Any] {
        var isFolder: ObjCBool = false
        _ = fileExists(atPath: path, isDirectory: &isFolder)
        guard let set = classes[path] else { return [:] }
        if !isFolder.boolValue, let fileClassReadBack { return [.protectionKey: fileClassReadBack] }
        if !protected.contains(path) { protected.append(path) }
        return [.protectionKey: set]
    }
}
