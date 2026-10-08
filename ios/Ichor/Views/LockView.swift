import SwiftUI

/// Full-screen lock. If the device lost its passcode, the only way forward is wiping the
/// stored config, so removing the passcode never bypasses the app lock. With a security key
/// enrolled the prompt asks for a tap (IchorCore/SecurityKeys.swift); with one required, losing
/// every key leaves wiping as the only way out too.
struct LockView: View {
    @Environment(AppModel.self) private var model
    @State private var error: String?
    @State private var confirmLost = false

    private var available: Bool { Authenticator.isAvailable || model.securityKeys != nil }

    var body: some View {
        VStack(spacing: 16) {
            Image(systemName: "lock.fill").font(.system(size: 44)).accessibilityHidden(true)
            if available {
                Text("Ichor is locked").font(.headline).accessibilityAddTraits(.isHeader)
                if let keys = model.securityKeys {
                    Text(keys.required ? String(localized: "Tap your security key to open Ichor.") : String(localized: "Tap your security key, or use Face ID or the passcode."))
                        .foregroundStyle(.secondary).multilineTextAlignment(.center)
                }
                if let error { Text(error).foregroundStyle(.statusBad).multilineTextAlignment(.center) }
                Button("Unlock") { Task { await unlock() } }.buttonStyle(.borderedProminent)
                if model.requiresKey {
                    Button("I lost my security keys") { confirmLost = true }.font(.footnote)
                }
            } else {
                Text("This device no longer has a passcode, so the app lock cannot verify you.")
                    .multilineTextAlignment(.center)
                Button("Delete stored talosconfig", role: .destructive) {
                    model.clear()
                    model.setLockEnabled(false)
                }
            }
        }
        .padding(32)
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .background(Color(.systemBackground))
        .task { if available { await unlock() } }
        .confirmationDialog("Lost your security keys?", isPresented: $confirmLost, titleVisibility: .visible) {
            Button("Delete stored talosconfig", role: .destructive) {
                model.clear()
                model.setLockEnabled(false)
            }
        } message: {
            Text("Without one of them the stored configs cannot be read. Delete them to start over and import your talosconfig again.")
        }
    }

    private func unlock() async {
        if let message = await Authenticator.authenticate(reason: String(localized: "Unlock Ichor")) {
            error = message
            announce(message)
        } else {
            model.unlock()
        }
    }
}
