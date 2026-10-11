import SwiftUI
import IchorCore

/// Settings → Security: the app lock, and the security keys once it is on.
struct SecuritySection: View {
    @Environment(AppModel.self) private var model
    @State private var lockError: String?

    var body: some View {
        Section {
            Toggle("App lock", isOn: Binding(get: { model.lock.enabled }, set: { setLock($0) }))
                .disabled(model.lock.enabled && (lockRequired || model.requiresKey))
            if let lockError { Text(lockError).font(.footnote).foregroundStyle(.statusBad) }
        } header: {
            Text("Security")
        } footer: {
            VStack(alignment: .leading, spacing: 4) {
                Text("Face ID / Touch ID, or the device passcode, to open the app and before reboot or shutdown. Also hides the app in the app switcher.")
                if lockRequired && model.lock.enabled { Text("Always on while a cluster is imported.") }
                else if model.requiresKey && model.lock.enabled { Text("Always on while a security key is required.") }
            }
        }
        // A YubiKey (or any FIDO2 key) tapped on the iPhone as another way in, see IchorCore/SecurityKeys.swift.
        if model.lock.enabled { SecurityKeysSection() }
    }

    /// The lock cannot be turned off while a real cluster is stored.
    private var lockRequired: Bool { AppLockState.required(for: model.summary?.contexts ?? []) }

    /// Both enabling and disabling require authenticating first.
    private func setLock(_ enabled: Bool) {
        guard Authenticator.isAvailable else {
            lockError = String(localized: "Set up a passcode (and optionally Face ID) on this device first.")
            return
        }
        Task {
            if let failure = await Authenticator.authenticate(reason: enabled ? String(localized: "Enable app lock") : String(localized: "Disable app lock")) {
                lockError = failure
            } else {
                lockError = nil
                model.setLockEnabled(enabled)
            }
        }
    }
}
