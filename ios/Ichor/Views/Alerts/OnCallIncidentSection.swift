import SwiftUI
import IchorCore

/// "Triggered" or "Acknowledged by Sam", read out as being about the on-call incident.
struct OnCallBadgeLabel: View {
    let incident: OnCallIncident

    private var label: String? {
        switch OnCallBadge(incident) {
        case .triggered: String(localized: "Triggered")
        case .acknowledged(let name):
            name.isEmpty ? String(localized: "Acknowledged") : String(localized: "Acknowledged by \(name)")
        case .hidden: nil
        }
    }

    var body: some View {
        if let label {
            Text(verbatim: label)
                .font(.caption.weight(.medium))
                .foregroundStyle(incident.status == .triggered ? Color.red : Color.orange)
                .lineLimit(1)
                .accessibilityLabel(Text("On-call incident: \(label)"))
        }
    }
}

/// The incident behind an alert in its sheet: its state and title, how sure the match is, Acknowledge
/// unless already done, Resolve after one question, and the link to PagerDuty. A failure stays here.
struct OnCallIncidentSection: View {
    let incident: OnCallIncident
    let confidence: OnCallConfidence
    /// Runs the verb and reloads; nil when done, else why not.
    let act: (OnCallVerb) async -> String?

    @State private var busy = false
    @State private var failure: String?
    @State private var confirmingResolve = false

    var body: some View {
        Section("PagerDuty incident") {
            OnCallBadgeLabel(incident: incident)
            Text(verbatim: incident.title).textSelection(.enabled)
            LabeledContent {
                Text(verbatim: incident.service)
            } label: {
                Text("Incident #\(String(incident.number))")
            }
            if confidence != .exact {
                Text("Matched by alert name only: check it is the same incident.").font(.footnote)
                    .foregroundStyle(.secondary)
            }
            if incident.status == .triggered {
                Button("Acknowledge") { run(.acknowledge) }.disabled(busy)
            }
            Button("Resolve", role: .destructive) { confirmingResolve = true }.disabled(busy)
            if let url = URL(string: incident.url), url.scheme?.lowercased() == "https" {
                Link(destination: url) { Text("Open in PagerDuty") }
            }
            if let failure {
                Text("PagerDuty: \(failure)").font(.footnote).foregroundStyle(.red)
            }
        }
        .confirmationDialog(
            String(localized: "Resolve this incident?"), isPresented: $confirmingResolve, titleVisibility: .visible
        ) {
            Button("Resolve", role: .destructive) { run(.resolve) }
            Button("Cancel", role: .cancel) {}
        } message: {
            Text("PagerDuty closes incident #\(String(incident.number)) for everyone on call.")
        }
    }

    private func run(_ verb: OnCallVerb) {
        busy = true
        failure = nil
        Task {
            failure = await act(verb)
            busy = false
        }
    }
}
