import SwiftUI
import IchorCore

/// Every certificate (expired, then expiring within 30 days, then the rest) with its issuer, then
/// the issuers. A certificate opens what explains its state; one not being issued can be renewed
/// now from its context menu or swipe actions, after a confirmation, and from its row when it
/// needs attention or expires soon.
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
                    EmptyState(systemImage: "checkmark.shield", text: String(localized: "No certificates."))
                }
                let now = Date().epochMillis
                ForEach(status.certificatesByExpiry(now: now)) { certificateRow($0, now: now) }
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
                            isPresented: $confirmRenew.isPresent(),
                            titleVisibility: .visible,
                            presenting: confirmRenew) { cert in
            Button("Renew now") { Task { await renew(cert) } }
            Button("Cancel", role: .cancel) {}
        } message: { _ in
            Text("cert-manager issues the certificate again now, whatever its renewal time. An ACME issuer such as Let’s Encrypt counts it against its rate limits.")
        }
        .messageAlert($resultMessage)
    }

    private func certificateRow(_ cert: Certificate, now: Int64) -> some View {
        let busy = running.contains(cert.id)
        return NavigationLink {
            CertificateDetailsView(cert: cert, refreshList: refresh)
        } label: {
            CertificateRow(cert: cert, busy: busy, now: now) { confirmRenew = cert }
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

/// Three lines at most: the name, "expires in 32 days · letsencrypt-prod" (tinted when it needs
/// a look), then one DNS name and how many more. The renew button shows only on a certificate
/// that needs attention or expires within 30 days; the details offer renew for every one.
private struct CertificateRow: View {
    let cert: Certificate
    let busy: Bool
    let now: Int64
    let onRenew: () -> Void

    var body: some View {
        ResourceRow(health: cert.health, title: cert.label, subtitle: summary, trailing: trailing) {
            if let names {
                Text(verbatim: names).font(.caption.monospaced()).foregroundStyle(.secondary).lineLimit(1)
            }
        }
    }

    @ViewBuilder private var trailing: some View {
        if busy {
            ProgressView()
        } else if cert.showsRenew(now: now) && !cert.issuing {
            Button(action: onRenew) {
                Image(systemName: "arrow.clockwise")
                    .frame(minWidth: 44, minHeight: 44)
                    .contentShape(Rectangle())
            }
            .buttonStyle(.borderless)
            .accessibilityLabel(Text("Renew now"))
        }
    }

    private var summary: String {
        let failed = cert.failedAttempts > 0 ? String(localized: "\(cert.failedAttempts) failed attempts") : nil
        let issuing = cert.issuing ? String(localized: "issuing") : nil
        let issuer = cert.issuerName.isEmpty ? nil : cert.issuerName
        return ([expiry, issuing].compactMap { $0 } + cert.reasons.compactMap(reasonText) + [failed, issuer].compactMap { $0 })
            .joined(separator: " · ")
    }

    /// "expires in 23 days", "expired 2 days ago", or not issued yet.
    private var expiry: String {
        guard cert.notAfter > 0 else { return String(localized: "not issued yet") }
        let relative = relativeTime(cert.notAfter)
        return cert.expired(now: now) ? String(localized: "expired \(relative)") : String(localized: "expires \(relative)")
    }

    /// "a.example.org +2": the first name; the details list them all.
    private var names: String? {
        guard let first = cert.dnsNames.first else { return nil }
        let more = max(cert.dnsNameCount, cert.dnsNames.count) - 1
        return more > 0 ? "\(first) +\(more)" : first
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
        ResourceRow(health: issuer.health, title: issuer.label, subtitle: subtitle) {
            if !issuer.ready && !issuer.message.isEmpty {
                Text(verbatim: issuer.message).font(.caption).foregroundStyle(.secondary).lineLimit(3)
            }
        }
    }

    private var subtitle: String {
        let state: String? = issuer.ready ? nil : String(localized: "not ready")
        return [type, issuer.server.nonEmpty, state].compactMap { $0 }.joined(separator: " · ")
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
