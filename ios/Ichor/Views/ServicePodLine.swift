import IchorCore

extension ServicePod {
    /// "name · role · on node": a pod's row in a data-service list.
    var line: String {
        let roleLabel: String? = switch role {
        case "primary": String(localized: "primary")
        case "replica": String(localized: "replica")
        case "member": String(localized: "member")
        case "master": String(localized: "master")
        default: role.nonEmpty
        }
        let place: String = if node.isEmpty && phase == "Pending" {
            String(localized: "pending, not scheduled")
        } else if !node.isEmpty {
            String(localized: "on \(node)")
        } else {
            phase
        }
        return [name, roleLabel, place].compactMap { $0 }.filter { !$0.isEmpty }.joined(separator: " · ")
    }
}
