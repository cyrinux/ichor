import CoreImage
import SwiftUI
import TalosdevMobileCore
import UniformTypeIdentifiers

/// Issues a client certificate (os:admin) like `talosctl config new`: either renews this
/// device's own certificate in the stored config, or makes a talosconfig for another device,
/// handed over as a QR code or a file. The issued YAML only lives in this screen's memory.
struct IssueConfigView: View {
    enum Mode: String, CaseIterable, Identifiable {
        case renew, otherDevice

        var id: String { rawValue }

        var label: String {
            switch self {
            case .renew: String(localized: "This device")
            case .otherDevice: String(localized: "Another device")
            }
        }
    }

    @Environment(AppModel.self) private var model
    @State private var mode: Mode
    @State private var validity = ConfigValidity.default
    @State private var roles = defaultIssuedRoles
    @State private var confirming = false
    @State private var busy = false
    @State private var message: String?
    @State private var failed = false
    /// The talosconfig made for another device; cleared when the screen goes away.
    @State private var issued: IssuedConfig?

    init(initialMode: Mode = .renew) {
        _mode = State(initialValue: initialMode)
    }

    var body: some View {
        Form {
            if !model.allows(.issueConfig) {
                RoleNotice(feature: .issueConfig, roles: currentRoles)
            } else if let issued {
                IssuedConfigSection(issued: issued, contextName: model.activeContext) { self.issued = nil }
            } else {
                Section {
                    Picker("Mode", selection: $mode) {
                        ForEach(Mode.allCases) { Text($0.label).tag($0) }
                    }
                    .pickerStyle(.segmented)
                } footer: {
                    if mode == .renew {
                        Text("Issues a new certificate with the same roles and replaces this context's credentials on this device. Other contexts are kept.")
                    } else {
                        Text("Issues a separate talosconfig for another device or person, to scan as a QR code or save as a file.")
                    }
                }
                if mode == .renew { renewSection } else { rolesSection }
                Section("Validity") {
                    Picker("Valid for", selection: $validity) {
                        ForEach(ConfigValidity.allCases) { Text($0.localizedLabel).tag($0) }
                    }
                    LabeledContent("Expires", value: expiry.formatted(date: .abbreviated, time: .omitted))
                }
                Section {
                    Button {
                        confirming = true
                    } label: {
                        HStack {
                            Text(mode == .renew ? String(localized: "Renew certificate…") : String(localized: "Create talosconfig…"))
                            if busy {
                                Spacer()
                                ProgressView()
                            }
                        }
                    }
                    .disabled(busy || selectedRoles == nil)
                    if let message {
                        Text(message).font(.footnote).foregroundStyle(failed ? .red : .green)
                    }
                }
            }
        }
        .themedBackground()
        .navigationTitle("Issue talosconfig")
        .navigationBarTitleDisplayMode(.inline)
        .confirmationDialog(confirmTitle, isPresented: $confirming, titleVisibility: .visible) {
            Button(mode == .renew ? String(localized: "Renew") : String(localized: "Create")) {
                Task { await issue() }
            }
            Button("Cancel", role: .cancel) {}
        } message: {
            Text(confirmMessage)
        }
        .onChange(of: mode) { _, _ in message = nil }
        // Keep the credential in memory only while the screen is shown.
        .onDisappear { issued = nil }
    }

    private var currentRoles: [String] { model.activeSummary?.roles ?? [] }

    /// Renewal keeps the current certificate's (issuable) roles.
    private var selectedRoles: String? {
        mode == .renew ? rolesArgument(currentRoles) : rolesArgument(roles)
    }

    private var expiry: Date { Date().addingTimeInterval(TimeInterval(validity.hours) * 3_600) }

    private var renewSection: some View {
        Section("Current certificate") {
            LabeledContent("Context", value: model.activeContext)
            LabeledContent("Roles", value: currentRoles.joined(separator: ", "))
            if let notAfter = model.activeSummary?.certNotAfter, notAfter > 0 {
                LabeledContent("Cert expires", value: localizedCertExpiry(notAfter))
            }
        }
    }

    private var rolesSection: some View {
        Section {
            ForEach(issuableRoles, id: \.self) { role in
                Toggle(isOn: Binding(get: { roles.contains(role) }, set: { on in
                    roles = on ? roles.union([role]) : roles.subtracting([role])
                })) {
                    VStack(alignment: .leading, spacing: 2) {
                        Text(verbatim: role).font(.body.monospaced())
                        Text(roleDescription(role)).font(.caption).foregroundStyle(.secondary)
                    }
                }
            }
        } header: {
            Text("Roles")
        } footer: {
            Text("Give the least access needed: os:reader cannot change anything.")
        }
    }

    private func roleDescription(_ role: String) -> String {
        switch role {
        case "os:admin": String(localized: "Full control, including secrets and issuing certificates")
        case "os:operator": String(localized: "Reboot, shutdown, services and etcd maintenance, no secrets")
        case "os:reader": String(localized: "Read-only access, no secrets")
        case "os:etcd:backup": String(localized: "etcd snapshots only")
        default: ""
        }
    }

    private var confirmTitle: String {
        mode == .renew ? String(localized: "Renew this device's certificate?") : String(localized: "Create a talosconfig?")
    }

    private var confirmMessage: String {
        let roleList = (selectedRoles ?? "").replacingOccurrences(of: ",", with: ", ")
        let until = expiry.formatted(date: .abbreviated, time: .omitted)
        let grant = String(localized: "It grants access to the cluster with \(roleList) until \(until), and cannot be revoked short of rotating the cluster CA.")
        return mode == .renew
            ? String(localized: "The new certificate replaces this context's credentials on this device; the old one stays valid until it expires.") + " " + grant
            : String(localized: "Anyone holding the file or the QR code can use it.") + " " + grant
    }

    /// Face ID / passcode first when the app lock is on, then issue and store or show.
    private func issue() async {
        guard let client = model.client, let roleList = selectedRoles else { return }
        let reason = mode == .renew ? String(localized: "Renew the talosconfig certificate") : String(localized: "Create a talosconfig")
        if model.lock.enabled, let failure = await Authenticator.authenticate(reason: reason) {
            show(failure, failed: true)
            return
        }
        busy = true
        defer { busy = false }
        do {
            let yaml = try await client.generateTalosconfig(roles: roleList, hours: validity.hours)
            switch mode {
            case .renew:
                try await model.renewCredentials(with: yaml)
                let notAfter = model.activeSummary.map { localizedCertExpiry($0.certNotAfter) } ?? ""
                show(String(localized: "Certificate renewed: expires \(notAfter)."), failed: false)
            case .otherDevice:
                // The user just authenticated: show the QR code right away.
                issued = IssuedConfig(yaml: yaml, roles: roleList, expires: expiry)
                message = nil
            }
        } catch {
            show(error.localizedDescription, failed: true)
        }
    }

    private func show(_ text: String, failed: Bool) {
        message = text
        self.failed = failed
    }
}

struct IssuedConfig: Equatable {
    let yaml: String
    let roles: String
    let expires: Date
}

/// The issued talosconfig as a QR code (hidden whenever the app leaves the foreground or the
/// screen is recorded or mirrored, shown again only after authenticating) and as a file.
private struct IssuedConfigSection: View {
    let issued: IssuedConfig
    let contextName: String
    let onDone: () -> Void

    init(issued: IssuedConfig, contextName: String, onDone: @escaping () -> Void) {
        self.issued = issued
        self.contextName = contextName
        self.onDone = onDone
    }

    @Environment(AppModel.self) private var model
    @Environment(\.scenePhase) private var scenePhase
    @State private var revealed = true
    @State private var captured = UIScreen.main.isCaptured
    @State private var qrImage: UIImage?
    @State private var document: YAMLDocument?
    @State private var exporting = false
    @State private var message: String?

    var body: some View {
        Group {
            sections
        }
        .fileExporter(isPresented: $exporting, document: document, contentType: .yaml,
                      defaultFilename: issuedFileName(context: contextName, roles: issued.roles)) { result in
            switch result {
            case .success: message = String(localized: "Saved.")
            case .failure(let error): message = error.localizedDescription
            }
            document = nil // don't keep the credential around
        }
        .onChange(of: scenePhase) { _, phase in
            if phase != .active { revealed = false }
        }
        .onReceive(NotificationCenter.default.publisher(for: UIScreen.capturedDidChangeNotification)) { _ in
            captured = UIScreen.main.isCaptured
            if captured { revealed = false }
        }
        .onReceive(NotificationCenter.default.publisher(for: UIApplication.userDidTakeScreenshotNotification)) { _ in
            revealed = false
            message = String(localized: "A screenshot was taken: it may contain the QR code. Delete it.")
        }
        .onAppear { qrImage = makeQRCode(issued.yaml) }
        .onDisappear { qrImage = nil }
    }

    @ViewBuilder
    private var sections: some View {
        Section {
            Label("Talosconfig created", systemImage: "checkmark.seal.fill").foregroundStyle(.green)
            LabeledContent("Roles", value: issued.roles.replacingOccurrences(of: ",", with: ", "))
            LabeledContent("Expires", value: issued.expires.formatted(date: .abbreviated, time: .omitted))
        } footer: {
            Text("Grants cluster access with these roles until it expires; it cannot be revoked short of rotating the cluster CA.")
        }
        Section {
            if fitsInQRCode(issued.yaml) {
                qrCode
            } else {
                Label("Too large for a QR code: save it to a file and transfer it securely (e.g. AirDrop).", systemImage: "qrcode")
                    .font(.footnote)
                    .foregroundStyle(.orange)
            }
        } header: {
            Text("QR code")
        } footer: {
            if fitsInQRCode(issued.yaml) {
                Text("On the other device, scan it from Talosdev Mobile: Import talosconfig › QR code.")
            }
        }
        Section {
            Button("Save to file…") {
                document = YAMLDocument(text: issued.yaml)
                exporting = true
            }
            if let message { Text(message).font(.footnote).foregroundStyle(.secondary) }
            Button("Done", role: .destructive, action: onDone)
        }
    }

    @ViewBuilder
    private var qrCode: some View {
        let visible = revealed && !captured && scenePhase == .active
        VStack(spacing: 12) {
            ZStack {
                if let qrImage {
                    Image(uiImage: qrImage)
                        .interpolation(.none)
                        .resizable()
                        .scaledToFit()
                        .blur(radius: visible ? 0 : 24)
                        .opacity(visible ? 1 : 0.15)
                        .accessibilityLabel(Text("Talosconfig QR code"))
                }
                if !visible {
                    Image(systemName: "eye.slash").font(.largeTitle).foregroundStyle(.secondary)
                }
            }
            .frame(maxWidth: .infinity)
            .padding(8)
            .background(Color.white, in: RoundedRectangle(cornerRadius: 8))
            if captured {
                Text("Hidden while the screen is recorded or mirrored.").font(.footnote).foregroundStyle(.secondary)
            } else if !visible {
                Button("Show QR code") { Task { await reveal() } }
            }
        }
    }

    private func reveal() async {
        if model.lock.enabled, let failure = await Authenticator.authenticate(reason: String(localized: "Show the talosconfig QR code")) {
            message = failure
            return
        }
        revealed = true
    }
}

/// QR code (error correction L, the densest level the importer scans reliably) as a crisp
/// bitmap; nil if CoreImage cannot encode it.
func makeQRCode(_ text: String) -> UIImage? {
    guard let filter = CIFilter(name: "CIQRCodeGenerator") else { return nil }
    filter.setValue(Data(text.utf8), forKey: "inputMessage")
    filter.setValue("L", forKey: "inputCorrectionLevel")
    guard let output = filter.outputImage else { return nil }
    let scaled = output.transformed(by: CGAffineTransform(scaleX: 8, y: 8))
    guard let cgImage = CIContext().createCGImage(scaled, from: scaled.extent) else { return nil }
    return UIImage(cgImage: cgImage)
}
