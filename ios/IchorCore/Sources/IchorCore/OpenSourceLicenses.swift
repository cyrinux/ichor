import Foundation

/// The third-party code compiled into the app, bundled as licenses.json (written at build time
/// by scripts/go-licenses.py --ios), for About › Open-source licenses.
public struct OpenSourceLicenses: Codable, Equatable, Sendable {
    /// The project's NOTICE and LICENSE.
    public var notice: String
    public var libraries: [OpenSourceLibrary]

    public init(notice: String = "", libraries: [OpenSourceLibrary] = []) {
        self.notice = notice
        self.libraries = libraries
    }
}

public struct OpenSourceLibrary: Codable, Equatable, Hashable, Identifiable, Sendable {
    /// Go module path, "Go standard library" or Swift package name.
    public var name: String
    public var version: String
    public var website: String
    /// SPDX ids, e.g. ["MPL-2.0"].
    public var licenses: [String]
    /// The license files, verbatim.
    public var text: String

    public var id: String { name }

    /// "v1.14.2 · MPL-2.0"; just the licenses when the version is unknown.
    public var summary: String {
        let spdx = licenses.joined(separator: ", ")
        return version.isEmpty ? spdx : "\(version) · \(spdx)"
    }
}

/// The licenses in `data`; empty when the file is missing (nil) or corrupt.
public func decodeOpenSourceLicenses(_ data: Data?) -> OpenSourceLicenses {
    guard let data, let decoded = try? JSONDecoder().decode(OpenSourceLicenses.self, from: data) else { return OpenSourceLicenses() }
    return decoded
}
