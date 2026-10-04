import SwiftUI
import IchorCore

/// Every certificate (problems first, then the soonest expiry) with its issuer, then the issuers.
struct CertificatesList: View {
    let status: CertManagerStatus
    let refresh: () async -> Void

    var body: some View {
        List {
            if !status.error.isEmpty { Section { ErrorLine(error: status.error) } }
            Section {
                if status.certificates.isEmpty {
                    Text("No certificates.").foregroundStyle(.secondary)
                }
                ForEach(status.certificates) { CertificateRow(cert: $0) }
            }
            if !status.issuers.isEmpty {
                Section("Issuers") {
                    ForEach(status.issuers) { IssuerRow(issuer: $0) }
                }
            }
        }
        .refreshable { await refresh() }
        .themedBackground()
    }
}

private struct CertificateRow: View {
    let cert: Certificate

    var body: some View {
        VStack(alignment: .leading, spacing: 3) {
            HStack(spacing: 12) {
                HealthDot(health: cert.health)
                Text(verbatim: cert.label).font(.subheadline.monospaced()).lineLimit(1)
            }
            Text(verbatim: summary)
                .font(.caption)
                .foregroundStyle(cert.health.needsAttention ? cert.health.color : .secondary)
            Text(verbatim: [names, cert.issuer].filter { !$0.isEmpty }.joined(separator: " · "))
                .font(.caption.monospaced())
                .foregroundStyle(.secondary)
                .lineLimit(2)
            if !cert.message.isEmpty {
                Text(verbatim: cert.message).font(.caption).foregroundStyle(.secondary).lineLimit(3)
            }
        }
    }

    private var summary: String {
        let failed = cert.failedAttempts > 0 ? String(localized: "\(cert.failedAttempts) failed attempts") : nil
        return ([expiry] + cert.reasons.compactMap(reasonText) + [failed].compactMap { $0 }).joined(separator: " · ")
    }

    /// "expires in 23 days", "expired 2 days ago", or not issued yet.
    private var expiry: String {
        guard cert.notAfter > 0 else { return String(localized: "not issued yet") }
        let relative = relativeTime(cert.notAfter)
        return Date(epochMillis: cert.notAfter) > .now ? String(localized: "expires \(relative)") : String(localized: "expired \(relative)")
    }

    private var names: String {
        let more = cert.dnsNameCount - cert.dnsNames.count
        return cert.dnsNames.joined(separator: ", ") + (more > 0 ? " +\(more)" : "")
    }

    // Expiry is worded by the date itself: expired and expiring have no words of their own.
    private func reasonText(_ reason: CertReason) -> String? {
        switch reason {
        case .expired, .expiring: nil
        case .notReady: String(localized: "not ready")
        case .renewalOverdue: String(localized: "renewal overdue")
        case .issuer: String(localized: "issuer not ready")
        }
    }
}

private struct IssuerRow: View {
    let issuer: CertIssuer

    var body: some View {
        VStack(alignment: .leading, spacing: 3) {
            HStack(spacing: 12) {
                HealthDot(health: issuer.health)
                Text(verbatim: issuer.label).font(.subheadline.monospaced()).lineLimit(1)
            }
            let state: String? = issuer.ready ? nil : String(localized: "not ready")
            Text(verbatim: [type, issuer.server.isEmpty ? nil : issuer.server, state].compactMap { $0 }.joined(separator: " · "))
                .font(.caption)
                .foregroundStyle(issuer.health.needsAttention ? issuer.health.color : .secondary)
            if !issuer.ready && !issuer.message.isEmpty {
                Text(verbatim: issuer.message).font(.caption).foregroundStyle(.secondary).lineLimit(3)
            }
        }
    }

    private var type: String? {
        switch issuer.type {
        case "acme": "ACME"
        case "ca": "CA"
        case "selfSigned": String(localized: "self-signed")
        case "vault": "Vault"
        case "venafi": "Venafi"
        default: nil
        }
    }
}
