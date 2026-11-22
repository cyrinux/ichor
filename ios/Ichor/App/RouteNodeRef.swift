import Foundation

extension Route {
    /// The node a share link opens, on its hub or on one of its tabs; nil for any other screen.
    var nodeRef: NodeRef? {
        switch self {
        case .node(let ref), .nodeTab(let ref, _): ref
        default: nil
        }
    }
}
