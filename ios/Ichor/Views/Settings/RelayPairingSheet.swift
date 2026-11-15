import SwiftUI
import UIKit
import IchorCore

/// Pairs the cluster on screen with its in-cluster ichor-relay through a ntfy topic: the ntfy
/// app shows the relay's notification, and its tap opens Ichor on the cluster's Alerts screen.
/// Not end-to-end encrypted (SECURITY.md): the ntfy server reads the title, so the topic is
/// random by default. Unpairing is one tap: nothing is lost, the poller keeps alerting.
struct RelayPairingSheet: View {
    let client: TalosClient
    let fingerprint: String
    /// The cluster's nickname, in the notification title; empty for none.
    let label: String

    @Environment(\.dismiss) private var dismiss
    @Environment(\.openURL) private var openURL
    @State private var record: RelayPairingRecord?
    @State private var server = ntfyDefaultServer
    @State private var topic = randomNtfyTopic()
    @State private var verbose = false
    @State private var busy = false
    @State private var failure: String?
    @State private var unpairFailed = false

    init(client: TalosClient, fingerprint: String, label: String) {
        self.client = client
        self.fingerprint = fingerprint
        self.label = label
        _record = State(initialValue: RelayStore.read(fingerprint))
    }

    private var topicURL: URL? { ntfyTopicURL(server: server, topic: topic) }

    var body: some View {
        NavigationStack {
            Form {
                if let record {
                    paired(record)
                } else {
                    pairing
                }
                if let failure {
                    Section {
                        Label {
                            Text(verbatim: failure).textSelection(.enabled)
                        } icon: {
                            Image(systemName: "exclamationmark.triangle.fill")
                        }
                        .foregroundStyle(.statusBad)
                    }
                }
            }
            .navigationTitle("Push through ntfy")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .confirmationAction) { Button("Done") { dismiss() } }
            }
            .disabled(busy)
        }
    }

    @ViewBuilder private var pairing: some View {
        Section {
            TextField("ntfy server", text: $server)
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
                .keyboardType(.URL)
            HStack {
                TextField("Topic", text: $topic)
                    .textInputAutocapitalization(.never)
                    .autocorrectionDisabled()
                    .font(.body.monospaced())
                Button("Copy") { UIPasteboard.general.string = topic }
                    .buttonStyle(.borderless)
            }
            Toggle("Include alert text (not encrypted)", isOn: $verbose)
        } footer: {
            Text(
                "Subscribe to this topic in the ntfy app, then pair. The ntfy app shows the notification; tapping it opens Ichor. Alert names travel through your ntfy server in clear, and anyone who knows the topic can read them: keep the random topic, and prefer your own server."
            )
        }
        Section {
            Button {
                Task { await pair() }
            } label: {
                HStack {
                    if busy { ProgressView() }
                    Text("Pair")
                }
            }
            .disabled(topicURL == nil)
            if topicURL == nil {
                Text("Not a valid ntfy server or topic").font(.caption).foregroundStyle(.secondary)
            }
        }
    }

    @ViewBuilder private func paired(_ record: RelayPairingRecord) -> some View {
        Section {
            Text("Paired · \(ntfyTopicLabel(record.endpoint))")
            if let topic = URL(string: record.endpoint), let app = ntfyAppURL(topic) {
                Button("Open in ntfy") { openURL(app) }
            }
            Button("Unpair", role: .destructive) { Task { await unpair(record) } }
            if unpairFailed {
                Button("Forget on this phone", role: .destructive) { forget() }
            }
        } footer: {
            Text("Unpaired, the background checks still alert as before.")
        }
    }

    private func pair() async {
        guard let endpoint = topicURL?.absoluteString else { return }
        busy = true
        failure = nil
        defer { busy = false }
        do {
            let found = try await client.relayDiscover()
            guard found.canPair, let source = found.sources.first else {
                failure = cannotPair(found.reason)
                return
            }
            let device = RelayDevice(platform: RelayDevice.ntfy, endpoint: endpoint, label: label, verbose: verbose)
            let id = try await client.relayRegister(source, device: device)
            let paired = RelayPairingRecord(deviceId: id, endpoint: endpoint, source: source, pairedAt: Date())
            try RelayStore.save(paired, for: fingerprint)
            record = paired
        } catch {
            failure = error.localizedDescription
        }
    }

    /// Why Go refused (RBAC, no relay), else that none was found.
    private func cannotPair(_ reason: String) -> String {
        if reason.isEmpty { return String(localized: "No ichor-relay found in this cluster.") }
        return String(localized: "Cannot pair: \(reason)")
    }

    private func unpair(_ record: RelayPairingRecord) async {
        busy = true
        failure = nil
        defer { busy = false }
        do {
            try await client.relayUnregister(record.source, id: record.deviceId)
            forget()
        } catch {
            failure = error.localizedDescription
            unpairFailed = true
        }
    }

    /// Drops the pairing kept here, when the relay is gone or refuses: it may keep the device.
    private func forget() {
        RelayStore.delete(fingerprint)
        record = nil
        unpairFailed = false
        topic = randomNtfyTopic()
    }
}
