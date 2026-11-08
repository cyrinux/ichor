import SwiftUI
import IchorCore

/// What is wrong with the certificate a route serves; nothing when it is fine or unknown.
struct RouteCertBadge: View {
    let route: KubeRoute

    var body: some View {
        if let health = route.certHealth {
            StatusPill(label: text, color: health == .critical ? Color.statusBad : Color.statusWarn)
                .accessibilityElement(children: .combine)
        }
    }

    private var text: String {
        switch route.certIssue {
        case RouteCertIssue.expired: return String(localized: "Certificate expired")
        case RouteCertIssue.hostMismatch: return String(localized: "Certificate does not cover this host")
        default:
            let days = max(0, route.certDaysLeft(now: Int64(Date().timeIntervalSince1970)))
            return String(localized: "Certificate expires in \(days) days")
        }
    }
}
