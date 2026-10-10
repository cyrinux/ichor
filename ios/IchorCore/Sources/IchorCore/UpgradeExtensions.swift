import Foundation

/// A node's system extensions against the Image Factory's list for the target version
/// (UpgradeExtensionCheck).
public struct UpgradeExtensionCheck: Decodable, Equatable, Sendable {
    public struct Installed: Decodable, Equatable, Sendable {
        public let name: String
        public let version: String

        private enum CodingKeys: String, CodingKey { case name, version }

        public init(from decoder: Decoder) throws {
            let c = try decoder.container(keyedBy: CodingKeys.self)
            name = try c.field(.name, "")
            version = try c.field(.version, "")
        }
    }

    public let schematic: String
    public let targetVersion: String
    public let installed: [Installed]
    /// Installed extensions the target version has no official build of.
    public let missing: [String]
    /// The image is not from the public Image Factory, or it could not be read (`error`).
    public let unknown: Bool
    public let error: String

    private enum CodingKeys: String, CodingKey { case schematic, targetVersion, installed, missing, unknown, error }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        schematic = try c.field(.schematic, "")
        targetVersion = try c.field(.targetVersion, "")
        installed = try c.field(.installed, [])
        missing = try c.field(.missing, [])
        unknown = try c.field(.unknown, false)
        error = try c.field(.error, "")
    }
}
