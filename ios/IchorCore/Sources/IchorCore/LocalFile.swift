import Foundation

/// A file the app keeps on the phone (a capture, a support bundle), as its list shows it.
public struct LocalFile: Equatable, Identifiable, Sendable {
    public let name: String
    public let size: Int64
    public let modified: Date

    public var id: String { name }

    public init(name: String, size: Int64, modified: Date) {
        self.name = name
        self.size = size
        self.modified = modified
    }
}
