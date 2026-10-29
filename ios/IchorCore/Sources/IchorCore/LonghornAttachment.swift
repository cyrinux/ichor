import Foundation
public struct LonghornAttachmentNode: Decodable, Identifiable, Sendable {
    public let name: String
    public let eligible: Bool
    public var id: String { name }
}
public struct LonghornAttachmentPreview: Decodable, Sendable {
    public let namespace: String
    public let name: String
    public let state: String
    public let node: String
    public let canAttach: Bool
    public let canDetach: Bool
    public let error: String
    public let nodes: [LonghornAttachmentNode]
    public func accepts(_ selected: String, attach: Bool) -> Bool {
        guard !selected.isEmpty else { return false }
        return attach
            ? error.isEmpty && canAttach && nodes.contains { $0.name == selected && $0.eligible }
            : canDetach && selected == node
    }
}
