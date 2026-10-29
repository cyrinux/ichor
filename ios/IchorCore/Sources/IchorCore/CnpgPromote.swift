import Foundation

public struct CnpgPromotePreview: Decodable, Equatable, Sendable {
    public let namespace, name, currentPrimary, instance, lagError: String
    public let lagBytes: Int64?
    public let ready, canPromote: Bool
    public let warnings: [String]
}
public extension CnpgCluster {
    var promotablePods: [ServicePod] {
        if phase == "Primary instance is being restarted in-place" || hibernated
            || (!targetPrimary.isEmpty && targetPrimary != currentPrimary)
        {
            return []
        }
        return instancePods.filter { $0.ready && $0.name != currentPrimary && $0.role != "primary" }
    }
}
