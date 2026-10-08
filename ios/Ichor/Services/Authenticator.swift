import Foundation
import LocalAuthentication

/// Face ID / Touch ID with the device passcode as fallback; or, with a security key enrolled
/// (IchorCore/SecurityKeys.swift), the "tap your key" prompt, which also offers Face ID unless
/// the key is required. Every check in the app (unlock, reboot, export…) goes through here.
enum Authenticator {
    static var isAvailable: Bool {
        var error: NSError?
        return LAContext().canEvaluatePolicy(.deviceOwnerAuthentication, error: &error)
    }

    /// Returns nil on success, or a message to show.
    static func authenticate(reason: String) async -> String? {
        if let enrolment = SecurityKeyStore.current {
            return await SecurityKeyPrompts.shared.request(reason: reason, allowBiometric: !enrolment.required && isAvailable)
        }
        return await biometric(reason: reason)
    }

    /// The system prompt alone: Face ID / Touch ID, falling back to the passcode.
    static func biometric(reason: String) async -> String? {
        let context = LAContext()
        do {
            return try await context.evaluatePolicy(.deviceOwnerAuthentication, localizedReason: reason)
                ? nil : String(localized: "Authentication failed")
        } catch {
            return error.localizedDescription
        }
    }
}
