import Foundation
import IchorCore
import SwiftUI
import UniformTypeIdentifiers

/// The files of one of the app's folders (captures, support bundles).
enum LocalFileStore {
    /// Every file of `folder`, unsorted: its store filters and orders them.
    static func list(in folder: URL) throws -> [LocalFile] {
        let keys: [URLResourceKey] = [.fileSizeKey, .contentModificationDateKey]
        let urls = try FileManager.default.contentsOfDirectory(at: folder, includingPropertiesForKeys: keys)
        return urls.map { url in
            let values = try? url.resourceValues(forKeys: Set(keys))
            return LocalFile(name: url.lastPathComponent, size: Int64(values?.fileSize ?? 0),
                             modified: values?.contentModificationDate ?? .distantPast)
        }
    }

    static func delete(_ url: URL) throws {
        try FileManager.default.removeItem(at: url)
    }
}

/// A file on disk handed to the system exporter ("Save to Files"), never read back.
struct ExportFileDocument: FileDocument {
    static var readableContentTypes: [UTType] { [.pcap, .zip] }
    let url: URL
    var options: FileWrapper.ReadingOptions = []

    init(url: URL, options: FileWrapper.ReadingOptions = []) {
        self.url = url
        self.options = options
    }

    init(configuration: ReadConfiguration) throws {
        throw CocoaError(.featureUnsupported) // export only
    }

    func fileWrapper(configuration: WriteConfiguration) throws -> FileWrapper {
        try FileWrapper(url: url, options: options)
    }
}
