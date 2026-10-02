import SwiftUI

/// Full-screen lock. If the device lost its passcode, the only way forward is wiping the
/// stored config, so removing the passcode never bypasses the app lock.
struct LockView: View {
    @Environment(AppModel.self) private var model
    @State private var error: String?
    private let available = Authenticator.isAvailable

    var body: some View {
        VStack(spacing: 16) {
            Image(systemName: "lock.fill").font(.system(size: 44))
            if available {
                Text("Ichor is locked").font(.headline)
                if let error { Text(error).foregroundStyle(.red).multilineTextAlignment(.center) }
                Button("Unlock") { Task { await unlock() } }.buttonStyle(.borderedProminent)
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
    }

    private func unlock() async {
        if let message = await Authenticator.authenticate(reason: String(localized: "Unlock Ichor")) {
            error = message
        } else {
            model.unlock()
        }
    }
}
