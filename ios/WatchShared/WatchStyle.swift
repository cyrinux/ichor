import IchorCore
import SwiftUI

/// What the watch app and its complication share: the widget's colours and wording.
enum WatchStyle {
    static let ok = rgb(0x5BD18B)
    static let warning = rgb(0xF2C14E)
    static let problem = rgb(0xF0716B)
    static let unknown = Color.gray

    static func rgb(_ hex: UInt32) -> Color {
        Color(red: Double((hex >> 16) & 0xFF) / 255, green: Double((hex >> 8) & 0xFF) / 255, blue: Double(hex & 0xFF) / 255)
    }

    /// A cluster's colour; grey before its first check.
    static func color(_ tone: GlanceItem.Tone?) -> Color {
        switch tone {
        case .ok: return ok
        case .warning: return warning
        case .problem: return problem
        case nil: return unknown
        }
    }

    static func color(_ health: NodeHealth) -> Color {
        switch health {
        case .ready: return ok
        case .notReady: return warning
        case .unreachable: return problem
        }
    }

    /// The same texts as the iOS widget (TalosWidget's Glance.text).
    static func text(_ item: GlanceItem) -> Text {
        switch item {
        case .notReady(let n): return Text("\(n) not ready")
        case .unreachable(let n): return Text("\(n) unreachable")
        case .allReady: return Text("all ready")
        case .etcdNoAlarms: return Text("etcd: no alarms")
        case .etcdAlarms(let n): return Text("etcd: \(n) alarms")
        }
    }

    /// Why an unreachable node does not answer; nil when there is nothing more than "Unreachable".
    static func problem(_ problem: WatchNodeProblem) -> Text? {
        switch problem {
        case .timeout: return Text("Timed out")
        case .refused: return Text("Connection refused")
        case .noSuchHost: return Text("Host not found")
        case .tls: return Text("TLS error")
        case .unreachable: return nil
        }
    }

    static func state(_ health: NodeHealth) -> Text {
        switch health {
        case .ready: return Text("Ready")
        case .notReady: return Text("Not ready")
        case .unreachable: return Text("Unreachable")
        }
    }

    /// A node's hostname, or its position when the phone had none to send.
    static func name(_ node: WatchNode, index: Int) -> Text {
        node.name.isEmpty ? Text("Node \(index + 1)") : Text(verbatim: node.name)
    }

    static func ready(_ cluster: WatchCluster) -> Text {
        Text("\(cluster.readyCount)/\(cluster.nodes.count) ready")
    }
}
