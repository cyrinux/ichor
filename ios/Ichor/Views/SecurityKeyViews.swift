import IchorCore
import SwiftUI

// Security keys (a YubiKey or any FIDO2 key, over NFC or a wired port) for the app lock, see
// IchorCore/SecurityKeys.swift: the "tap your key" prompt Authenticator shows, the Settings
// section that enrols keys, and the flows (adding a key, requiring it) that take several taps.

/// Shows the pending security-key prompt, if any (SecurityKeyPrompts), over the whole scene.
struct SecurityKeyPromptHost: View {
    private let prompts = SecurityKeyPrompts.shared

    var body: some View {
        if let request = prompts.request {
            ZStack {
                Color.black.opacity(0.35).ignoresSafeArea()
                SecurityKeyPromptView(request: request)
                    .padding(24)
                    .frame(maxWidth: 420)
            }
            .transition(.opacity)
            .id(request.id)
        }
    }
}

/// Waits for a key, checks its assertion and resolves `request`. In the required mode the first
/// successful tap of the process also unwraps the data key the stored configs are sealed with.
private struct SecurityKeyPromptView: View {
    let request: SecurityKeyPromptRequest
    @State private var error: String?
    @State private var busy = false

    var body: some View {
        VStack(spacing: 16) {
            Image(systemName: "key.radiowaves.forward").font(.system(size: 36)).foregroundStyle(.tint).accessibilityHidden(true)
            Text(request.reason).font(.headline).multilineTextAlignment(.center).accessibilityAddTraits(.isHeader)
            Text(SecurityKeyClient.nfcAvailable
                 ? String(localized: "Hold your security key near the top of the iPhone, or plug it in.")
                 : String(localized: "Plug your security key in: this device cannot read NFC."))
                .font(.subheadline).foregroundStyle(.secondary).multilineTextAlignment(.center)
            if let error { Text(error).font(.footnote).foregroundStyle(.statusBad).multilineTextAlignment(.center) }
            if busy {
                ProgressView()
            } else {
                VStack(spacing: 8) {
                    if SecurityKeyClient.nfcAvailable {
                        Button { Task { await scan(.nfc) } } label: { Label("Tap the key", systemImage: "wave.3.right").frame(maxWidth: .infinity) }
                            .buttonStyle(.borderedProminent)
                    }
                    Button { Task { await scan(.wired) } } label: { Label("Use a plugged-in key", systemImage: "cable.connector").frame(maxWidth: .infinity) }
                        .buttonStyle(.bordered)
                    if request.allowBiometric {
                        Button("Use Face ID or passcode instead") { Task { request.resolve(await Authenticator.biometric(reason: request.reason)) } }
                    }
                    Button("Cancel", role: .cancel) { request.resolve(String(localized: "Cancelled")) }
                }
            }
        }
        .padding(24)
        .background(Color(.systemBackground), in: RoundedRectangle(cornerRadius: 20))
        .task { if SecurityKeyClient.nfcAvailable { await scan(.nfc) } }
    }

    private func scan(_ transport: SecurityKeyTransport) async {
        guard let enrolment = SecurityKeyStore.current else { return request.resolve(String(localized: "Cancelled")) }
        busy = true
        error = nil
        defer { busy = false }
        do {
            let assertion = try await SecurityKeyClient.withConnection(transport, message: String(localized: "Hold your security key near the top of the iPhone")) { connection in
                try await SecurityKeyClient.assert(connection, enrolment: enrolment, wantSecret: enrolment.required)
            }
            if enrolment.required, SecurityKeySession.shared.currentDek == nil {
                SecurityKeySession.shared.provide(try SecurityKeyCrypto.unwrapDek(enrolment, assertion: assertion))
            }
            request.resolve(nil)
        } catch {
            self.error = securityKeyMessage(error)
        }
    }
}

/// The enrolled security keys (Settings → Security): list, add one (a spare too), remove one,
/// and the "Require the key" mode that seals the stored configs with them. Each change is
/// checked first, like the other lock settings.
struct SecurityKeysSection: View {
    @Environment(AppModel.self) private var model
    @State private var flow: SecurityKeyFlow?
    @State private var message: String?
    @State private var busy = false

    private var keys: [EnrolledKey] { model.securityKeys?.keys ?? [] }

    var body: some View {
        Section {
            ForEach(keys, id: \.credentialId) { key in
                HStack {
                    Image(systemName: "key.fill").foregroundStyle(.tint).accessibilityHidden(true)
                    VStack(alignment: .leading, spacing: 2) {
                        Text(key.label)
                        Text("Added on \(key.enrolledAt.formatted(date: .abbreviated, time: .omitted))").font(.caption).foregroundStyle(.secondary)
                    }
                    Spacer()
                    Button { remove(key) } label: { Image(systemName: "trash") }
                        .buttonStyle(.borderless)
                        .accessibilityLabel(Text("Remove \(key.label)"))
                }
            }
            if keys.count < securityKeyMax {
                Button(keys.isEmpty ? String(localized: "Add a security key") : String(localized: "Add a spare key")) {
                    checked(String(localized: "Add a security key")) { flow = .enrol }
                }
            }
            if !keys.isEmpty {
                Toggle(isOn: Binding(get: { model.requiresKey }, set: { on in on ? checked(String(localized: "Require the key")) { flow = .require } : release() })) {
                    VStack(alignment: .leading, spacing: 2) {
                        Text("Require the key")
                        Text("The stored configs are sealed so that only a tap of one of these keys can read them: Face ID or the passcode alone no longer opens Ichor. Background alerts and the widget pause when iOS has closed the app, until you unlock it again. Lose every key and you must import your configs again.")
                            .font(.caption).foregroundStyle(.secondary)
                    }
                }
                .disabled(busy)
            }
            if let message { Text(message).font(.footnote).foregroundStyle(.statusBad) }
        } header: {
            Text("Security keys")
        } footer: {
            Text("Tap a YubiKey or another FIDO2 key on the iPhone, or plug it in, to open Ichor without Face ID. Up to two keys.")
        }
        .sheet(item: $flow) { flow in
            SecurityKeyFlowView(flow: flow)
        }
    }

    /// Runs `action` after a check (Face ID, or the key once one is enrolled).
    private func checked(_ reason: String, action: @escaping () -> Void) {
        Task {
            if let failure = await Authenticator.authenticate(reason: reason) {
                message = failure
            } else {
                message = nil
                action()
            }
        }
    }

    private func remove(_ key: EnrolledKey) {
        guard let current = model.securityKeys else { return }
        if current.required, current.keys.count == 1 {
            message = String(localized: "Turn off “Require the key” before removing the last key.")
            return
        }
        checked(String(localized: "Remove \(key.label)")) { model.setSecurityKeys(current.without(credentialId: key.credentialId)) }
    }

    /// Turns the requirement off: a tap first (the prompt asks for it), then the items are unsealed.
    private func release() {
        guard let current = model.securityKeys else { return }
        busy = true
        Task {
            defer { busy = false }
            if let failure = await Authenticator.authenticate(reason: String(localized: "Require the key")) {
                message = failure
                return
            }
            message = nil
            // Unsealed while the data key is still held; a failure puts the requirement back.
            model.setSecurityKeys(current.released())
            do {
                try SecureConfigStore.reseal()
                SecurityKeySession.shared.clear()
            } catch {
                model.setSecurityKeys(current)
                message = securityKeyMessage(error)
            }
        }
    }
}

/// A multi-tap operation on the keys, run in SecurityKeyFlowView.
enum SecurityKeyFlow: String, Identifiable {
    case enrol, require
    var id: String { rawValue }
}

/// Walks the user through the taps a flow needs (instruction, the key's FIDO PIN when a key
/// insists on it for new credentials, errors) and applies the result to the app lock.
private struct SecurityKeyFlowView: View {
    let flow: SecurityKeyFlow
    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    @State private var instruction = ""
    @State private var error: String?
    @State private var done = false
    @State private var busy = false
    @State private var askPin = false
    @State private var pin = ""
    /// The PIN typed, without the line breaks a pasted one often ends with.
    private var fidoPin: String { pin.filter { !$0.isNewline } }
    var body: some View {
        NavigationStack {
            VStack(spacing: 20) {
                Image(systemName: "key.radiowaves.forward").font(.system(size: 44)).foregroundStyle(.tint).accessibilityHidden(true)
                if done {
                    Label("Done.", systemImage: "checkmark.circle.fill").font(.headline)
                } else if askPin {
                    Text("This key only makes new credentials with its FIDO PIN. It is used once here; unlocking never asks for it.")
                        .multilineTextAlignment(.center)
                    // A FIDO PIN is any text (4+ code points, at most 63 UTF-8 bytes): the full keyboard.
                    SecureField("FIDO PIN", text: $pin).textFieldStyle(.roundedBorder)
                        .textInputAutocapitalization(.never).autocorrectionDisabled()
                    Button("OK") { Task { await run(pin: fidoPin) } }.buttonStyle(.borderedProminent)
                        .disabled(fidoPin.unicodeScalars.count < 4 || fidoPin.utf8.count > 63)
                } else {
                    Text(instruction).multilineTextAlignment(.center)
                    if let error { Text(error).font(.footnote).foregroundStyle(.statusBad).multilineTextAlignment(.center) }
                    if busy {
                        ProgressView()
                    } else {
                        if SecurityKeyClient.nfcAvailable {
                            Button { Task { await run(.nfc) } } label: { Label("Tap the key", systemImage: "wave.3.right").frame(maxWidth: .infinity) }
                                .buttonStyle(.borderedProminent)
                        }
                        Button { Task { await run(.wired) } } label: { Label("Use a plugged-in key", systemImage: "cable.connector").frame(maxWidth: .infinity) }
                            .buttonStyle(.bordered)
                    }
                }
            }
            .padding(24)
            .frame(maxWidth: 480)
            .navigationTitle(flow == .enrol ? String(localized: "Add a security key") : String(localized: "Require the key"))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button(done ? String(localized: "Close") : String(localized: "Cancel")) { dismiss() } }
            }
            .onAppear { instruction = flow == .enrol ? String(localized: "Tap the key to add, or plug it in.") : nextRequireInstruction() }
        }
        .interactiveDismissDisabled(busy)
    }

    // MARK: - The flows

    /// Keys already tapped for the requirement, and the data key they wrap.
    @State private var wrapped: [EnrolledKey] = []
    @State private var dek = SecurityKeyCrypto.newDek()
    /// The key made by the enrolment's first tap, waiting for the second one (required mode).
    @State private var added: EnrolledKey?
    @State private var lastTransport: SecurityKeyTransport = .nfc

    private func run(_ transport: SecurityKeyTransport) async {
        lastTransport = transport
        await run(pin: nil)
    }

    private func run(pin: String?) async {
        busy = true
        error = nil
        askPin = false
        defer { busy = false }
        do {
            switch flow {
            case .enrol: try await enrolStep(pin: pin)
            case .require: try await requireStep()
            }
        } catch SecurityKeyError.pinRequired {
            askPin = true
        } catch {
            self.error = securityKeyMessage(error)
        }
        self.pin = ""
    }

    private func enrolStep(pin: String?) async throws {
        let enrolment = model.securityKeys ?? SecurityKeyEnrolment.create()
        if added == nil {
            let label = enrolment.keys.isEmpty ? String(localized: "Security key") : String(localized: "Spare key")
            added = try await SecurityKeyClient.withConnection(lastTransport, message: String(localized: "Hold the key to add near the top of the iPhone")) { connection in
                try await SecurityKeyClient.enrol(connection, enrolment: enrolment, label: label, pin: pin)
            }
        }
        guard var key = added else { return }
        if enrolment.required {
            // A second tap of the same key wraps the data key for it.
            instruction = String(localized: "Touch the same key again to set it up for the sealed configs.")
            guard let dek = SecurityKeySession.shared.currentDek else { throw SecurityKeyRequiredError() }
            let withNew = enrolment.with(key)
            let assertion = try await SecurityKeyClient.withConnection(lastTransport, message: String(localized: "Hold the same key near the top of the iPhone")) { connection in
                try await SecurityKeyClient.assert(connection, enrolment: withNew, wantSecret: true)
            }
            guard assertion.key.credentialId == key.credentialId else {
                error = String(localized: "That was \(assertion.key.label). Tap \(key.label).")
                return
            }
            key = try SecurityKeyCrypto.wrapping(key, dek: dek, assertion: assertion)
        }
        model.setSecurityKeys(enrolment.with(key))
        done = true
    }

    private func nextRequireInstruction() -> String {
        let keys = model.securityKeys?.keys ?? []
        guard let next = keys.first(where: { key in !wrapped.contains { $0.credentialId == key.credentialId } }) else { return "" }
        return String(localized: "Tap key \(wrapped.count + 1) of \(keys.count): \(next.label)")
    }

    /// One tap per enrolled key wraps the new data key for it; after the last, the stored configs
    /// are sealed. A failure while sealing puts everything back.
    private func requireStep() async throws {
        guard let enrolment = model.securityKeys, !enrolment.required else {
            done = true
            return
        }
        guard let expected = enrolment.keys.first(where: { key in !wrapped.contains { $0.credentialId == key.credentialId } }) else { return }
        let assertion = try await SecurityKeyClient.withConnection(lastTransport, message: String(localized: "Hold \(expected.label) near the top of the iPhone")) { connection in
            try await SecurityKeyClient.assert(connection, enrolment: enrolment, wantSecret: true)
        }
        guard assertion.key.credentialId == expected.credentialId else {
            error = String(localized: "That was \(assertion.key.label). Tap \(expected.label).")
            return
        }
        let wrappedKey = try SecurityKeyCrypto.wrapping(expected, dek: dek, assertion: assertion)
        wrapped.append(wrappedKey)
        if wrapped.count < enrolment.keys.count {
            instruction = nextRequireInstruction()
            return
        }
        instruction = String(localized: "Sealing the stored configs…")
        var updated = enrolment
        for key in wrapped { updated = updated.with(key) }
        updated.mode = .required
        SecurityKeySession.shared.provide(dek)
        model.setSecurityKeys(updated)
        do {
            try SecureConfigStore.reseal()
        } catch {
            // Back to Enclave-only items while the data key is still held, then forget it.
            model.setSecurityKeys(enrolment)
            try? SecureConfigStore.reseal()
            SecurityKeySession.shared.clear()
            throw error
        }
        done = true
    }
}
