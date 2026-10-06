import SwiftUI
import IchorCore

/// Why a certificate is in its state: its conditions, its latest requests with their ACME
/// orders and challenges, the events of that chain and the controller log lines naming it, with
/// a forced renewal when it is not being issued (os:admin).
struct CertificateDetailsView: View {
    let cert: Certificate
    /// Reloads the data services, for the list to show the issuance.
    let refreshList: () async -> Void

    @Environment(AppModel.self) private var model
    @State private var state: LoadState<CertDetails> = .loading
    @State private var confirmRenew = false
    @State private var renewing = false
    @State private var resultMessage: String?

    var body: some View {
        LoadStateView(state: state, retry: load) { details in
            List {
                if !details.error.isEmpty { Section { ErrorLine(error: details.error) } }
                if !cert.issuing {
                    Section {
                        Button {
                            confirmRenew = true
                        } label: {
                            HStack {
                                Label("Renew now", systemImage: "arrow.clockwise")
                                Spacer()
                                if renewing { ProgressView() }
                            }
                        }
                        .disabled(renewing)
                    }
                }
                Section("Conditions") {
                    ForEach(Array(details.conditions.enumerated()), id: \.offset) { ConditionRow(condition: $0.element) }
                }
                if !details.requests.isEmpty {
                    Section("Requests") { ForEach(details.requests) { RequestRow(request: $0) } }
                }
                Section("Events") {
                    if details.events.isEmpty {
                        Text("No events: Kubernetes keeps them for about an hour.").foregroundStyle(.secondary)
                    }
                    ForEach(Array(details.events.enumerated()), id: \.offset) { EventRow(event: $0.element) }
                }
                Section("Controller log") {
                    if details.log.isEmpty {
                        Text("No line of the cert-manager controller log names this certificate.").foregroundStyle(.secondary)
                    } else {
                        Text(verbatim: details.log.joined(separator: "\n"))
                            .font(.caption2.monospaced())
                            .foregroundStyle(.secondary)
                            .textSelection(.enabled)
                    }
                }
            }
            .refreshable { await load() }
            .themedBackground()
        }
        .task { await load() }
        .navigationTitle(Text(verbatim: cert.label))
        .navigationBarTitleDisplayMode(.inline)
        .confirmationDialog(Text("Renew \(cert.label)?"), isPresented: $confirmRenew, titleVisibility: .visible) {
            Button("Renew now") { Task { await renew() } }
            Button("Cancel", role: .cancel) {}
        } message: {
            Text("cert-manager issues the certificate again now, whatever its renewal time. An ACME issuer such as Let’s Encrypt counts it against its rate limits.")
        }
        .messageAlert($resultMessage)
    }

    private func load() async {
        guard let client = model.client else { return }
        let (namespace, name) = (cert.namespace, cert.name)
        let loaded: LoadState<CertDetails> = await .from { try await client.certificateDetails(namespace: namespace, name: name) }
        state = state.refreshed(with: loaded)
    }

    private func renew() async {
        guard let client = model.client, !renewing else { return }
        renewing = true
        defer { renewing = false }
        do {
            try await client.renewCertificate(namespace: cert.namespace, name: cert.name)
            resultMessage = String(localized: "Renewal of \(cert.label) requested")
            await load()
            await refreshList()
        } catch {
            resultMessage = String(localized: "Could not renew \(cert.label): \(error.localizedDescription)")
        }
    }
}

/// "Ready: False · Failed · 2 hours ago", then the message.
private struct ConditionRow: View {
    let condition: CertCondition

    var body: some View {
        let c = condition
        DetailLines(title: ["\(c.type): \(c.status)", c.reason, c.time > 0 ? relativeTime(c.time) : ""],
                    message: c.message,
                    health: c.health)
    }
}

private struct RequestRow: View {
    let request: CertRequestDetail

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            Text(verbatim: [request.name, request.created > 0 ? relativeTime(request.created) : ""].filter { !$0.isEmpty }.joined(separator: " · "))
                .font(.subheadline.monospaced())
            ForEach(Array(request.conditions.enumerated()), id: \.offset) { ConditionRow(condition: $0.element).padding(.leading, 12) }
            ForEach(request.orders) { order in
                DetailLines(title: [String(localized: "ACME order · \(order.state.isEmpty ? "…" : order.state)")],
                            message: order.reason,
                            health: order.failed ? .critical : .ok)
                    .padding(.leading, 12)
                ForEach(order.challenges) { challenge in
                    DetailLines(title: ["\(challenge.type) \(challenge.domain)", challenge.state.isEmpty ? "…" : challenge.state],
                                message: challenge.reason,
                                health: challenge.health)
                        .padding(.leading, 24)
                }
            }
        }
    }
}

/// "PresentError · Challenge/x · ×37 · 4 minutes ago", then the message.
private struct EventRow: View {
    let event: CertEvent

    var body: some View {
        DetailLines(title: [event.reason, event.object, event.count > 1 ? "×\(event.count)" : "", event.time > 0 ? relativeTime(event.time) : ""],
                    message: event.message,
                    health: event.warning ? .warning : .ok)
    }
}

/// A title of parts joined by " · ", coloured by health when it needs attention, then a message.
private struct DetailLines: View {
    let title: [String]
    let message: String
    let health: ServiceHealth

    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(verbatim: title.filter { !$0.isEmpty }.joined(separator: " · "))
                .font(.caption.weight(.medium))
                .foregroundStyle(health.needsAttention ? health.color : .primary)
            if !message.isEmpty {
                Text(verbatim: message).font(.caption).foregroundStyle(.secondary).textSelection(.enabled)
            }
        }
    }
}
