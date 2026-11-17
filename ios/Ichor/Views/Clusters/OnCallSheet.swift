import SwiftUI
import IchorCore

/// The on-call integration of a cluster: PagerDuty or Grafana IRM, its token (never read back into
/// the field: left empty it keeps the stored one of the same provider); for PagerDuty the email it
/// acts as, the region and the services to watch, for Grafana IRM the stack's API address; Test, and
/// Delete to remove it from this device.
struct OnCallSheet: View {
    let fingerprint: String

    @Environment(\.dismiss) private var dismiss
    @State private var stored = OnCallConfig()
    @State private var kind = OnCallConfig.kindPagerDuty
    @State private var baseURL = ""
    @State private var token = ""
    @State private var from = ""
    @State private var region = OnCallConfig.regionUS
    @State private var services: [String] = []
    @State private var service = ""
    @State private var testing = false
    @State private var testResult: String?
    @State private var testFailed = false
    @State private var failure: String?

    /// A token saved for the other provider is not offered to this one.
    private var savedToken: String { stored.kind == kind ? stored.token : "" }

    private var current: OnCallConfig {
        let typed = token.trimmingCharacters(in: .whitespacesAndNewlines)
        return OnCallConfig(
            kind: kind, token: typed.isEmpty ? savedToken : typed,
            from: from.trimmingCharacters(in: .whitespacesAndNewlines), serviceIds: services, region: region,
            baseURL: baseURL.trimmingCharacters(in: .whitespacesAndNewlines))
    }

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    Picker("Provider", selection: $kind) {
                        Text(verbatim: "PagerDuty").tag(OnCallConfig.kindPagerDuty)
                        Text(verbatim: "Grafana IRM").tag(OnCallConfig.kindGrafana)
                    }
                    .onChange(of: kind) { _, _ in testResult = nil }
                } footer: {
                    Text("See the PagerDuty incident behind this cluster's alerts, and acknowledge it from the phone.")
                }
                Section {
                    SecureField("API token", text: $token)
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                    if !savedToken.isEmpty && token.isEmpty {
                        Text("A token is saved. Leave empty to keep it.").font(.footnote).foregroundStyle(.secondary)
                    }
                    if kind == OnCallConfig.kindGrafana {
                        TextField("Grafana IRM API URL", text: $baseURL)
                            .keyboardType(.URL)
                            .textInputAutocapitalization(.never)
                            .autocorrectionDisabled()
                    } else {
                        TextField("Your PagerDuty email", text: $from)
                            .keyboardType(.emailAddress)
                            .textInputAutocapitalization(.never)
                            .autocorrectionDisabled()
                        Picker("Region", selection: $region) {
                            Text("US").tag(OnCallConfig.regionUS)
                            Text("EU").tag(OnCallConfig.regionEU)
                        }
                    }
                } footer: {
                    if kind == OnCallConfig.kindGrafana {
                        Text("Paste https://<stack>.grafana.net/api/plugins/grafana-irm-app/resources/api/v1")
                    } else {
                        Text("PagerDuty needs it to know who acknowledges.")
                    }
                }
                if kind == OnCallConfig.kindPagerDuty { servicesSection }
                testSection
                if !stored.token.isEmpty {
                    Section {
                        Button("Delete", role: .destructive) {
                            OnCallStore.remove(fingerprint)
                            dismiss()
                        }
                    }
                }
            }
            .navigationTitle("On-call")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() } }
                ToolbarItem(placement: .confirmationAction) { Button("Save", action: save) }
            }
            .messageAlert($failure)
        }
        .task {
            stored = OnCallStore.read(fingerprint)
            kind = stored.kind
            baseURL = stored.baseURL
            from = stored.from
            region = stored.region
            services = stored.serviceIds
        }
    }

    private var servicesSection: some View {
        Section {
            HStack {
                TextField("Service IDs", text: $service)
                    .textInputAutocapitalization(.characters)
                    .autocorrectionDisabled()
                Button("Add", action: addService)
                    .disabled(service.trimmingCharacters(in: .whitespaces).isEmpty)
            }
            ForEach(services, id: \.self) { id in
                HStack {
                    Text(verbatim: id).font(.callout.monospaced())
                    Spacer()
                    Button {
                        services.removeAll { $0 == id }
                    } label: {
                        Image(systemName: "xmark.circle.fill").foregroundStyle(.secondary)
                    }
                    .buttonStyle(.plain)
                    .accessibilityLabel(Text("Remove service \(id)"))
                }
            }
        } footer: {
            Text("One ID at a time, such as P123ABC.")
        }
    }

    private var testSection: some View {
        Section {
            Button {
                test()
            } label: {
                HStack {
                    Text(testing ? "Testing…" : "Test")
                    if testing {
                        Spacer()
                        ProgressView()
                    }
                }
            }
            .disabled(testing || current.token.isEmpty)
            if let testResult {
                Text(verbatim: testResult).font(.footnote).foregroundStyle(testFailed ? Color.red : Color.green)
            }
        }
    }

    private func addService() {
        let id = service.trimmingCharacters(in: .whitespaces)
        if !id.isEmpty && !services.contains(id) { services.append(id) }
        service = ""
    }

    private func test() {
        testing = true
        testResult = nil
        let config = current
        Task {
            defer { testing = false }
            do {
                let who = try await TalosClient.onCallTest(config)
                testFailed = false
                testResult =
                    who.user.isEmpty
                    ? String(localized: "Connected with an account token")
                    : String(localized: "Connected as \(who.user)")
            } catch {
                testFailed = true
                testResult = String(localized: "Test failed: \(error.localizedDescription)")
            }
        }
    }

    private func save() {
        do {
            try OnCallStore.save(current, for: fingerprint)
            dismiss()
        } catch {
            failure = error.localizedDescription
        }
    }
}
