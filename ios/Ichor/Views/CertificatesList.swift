import SwiftUI
import IchorCore

/// Every certificate (problems first, then the soonest expiry) with its issuer, then the issuers.
/// A certificate opens what explains its state; one not being issued can be renewed now from its
/// context menu or swipe actions, after a confirmation.
struct CertificatesList: View {
    let status: CertManagerStatus
    let refresh: () async -> Void

    @Environment(AppModel.self) private var model
    @State private var confirmRenew: Certificate?
    @State private var running: Set<String> = []
    @State private var resultMessage: String?

    var body: some View {
        List {
            if !status.error.isEmpty { Section { ErrorLine(error: status.error) } }
            Section {
                if status.certificates.isEmpty {
                    Text("No certificates.").foregroundStyle(.secondary)
                }
                ForEach(status.certificates) { certificateRow($0) }
            }
            if !status.issuers.isEmpty {
                Section("Issuers") {
                    ForEach(status.issuers) { IssuerRow(issuer: $0) }
                }
            }
        }
        .refreshable { await refresh() }
        .themedBackground()
        .confirmationDialog(confirmRenew.map { String(localized: "Renew \($0.label)?") } ?? "",
                            isPresented: Binding(get: { confirmRenew != nil }, set: { if !$0 { confirmRenew = nil } }),
                            titleVisibility: .visible,
                            presenting: confirmRenew) { cert in
            Button("Renew now") { Task { await renew(cert) } }
            Button("Cancel", role: .cancel) {}
        } message: { _ in
            Text("cert-manager issues the certificate again now, whatever its renewal time. An ACME issuer such as Let’s Encrypt counts it against its rate limits.")
        }
        .alert(resultMessage ?? "", isPresented: Binding(get: { resultMessage != nil }, set: { if !$0 { resultMessage = nil } })) {
            Button("OK") {}
        }
    }

    private func certificateRow(_ cert: Certificate) -> some View {
        let busy = running.contains(cert.id)
        return NavigationLink {
            CertificateDetailsView(cert: cert, refreshList: refresh)
        } label: {
            CertificateRow(cert: cert, busy: busy)
        }
        .swipeActions(edge: .trailing) {
            if !busy && !cert.issuing {
                Button { confirmRenew = cert } label: { Label("Renew now", systemImage: "arrow.clockwise") }.tint(.blue)
            }
        }
        .contextMenu {
            if !busy && !cert.issuing {
                Button { confirmRenew = cert } label: { Label("Renew now", systemImage: "arrow.clockwise") }
            }
        }
    }

    private func renew(_ cert: Certificate) async {
        guard let client = model.client, !running.contains(cert.id) else { return }
        running.insert(cert.id)
        defer { running.remove(cert.id) }
        do {
            try await client.renewCertificate(namespace: cert.namespace, name: cert.name)
            resultMessage = String(localized: "Renewal of \(cert.label) requested")
            await refresh()
        } catch {
            resultMessage = String(localized: "Could not renew \(cert.label): \(error.localizedDescription)")
        }
    }
}

private struct CertificateRow: View {
    let cert: Certificate
    let busy: Bool

    var body: some View {
        VStack(alignment: .leading, spacing: 3) {
            HStack(spacing: 12) {
                HealthDot(health: cert.health)
                Text(verbatim: cert.label).font(.subheadline.monospaced()).lineLimit(1)
                Spacer()
                if busy { ProgressView() }
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
        let issuing = cert.issuing ? String(localized: "issuing") : nil
        return ([expiry, issuing].compactMap { $0 } + cert.reasons.compactMap(reasonText) + [failed].compactMap { $0 }).joined(separator: " · ")
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
            Text(verbatim: [type, issuer.server.nonEmpty, state].compactMap { $0 }.joined(separator: " · "))
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
