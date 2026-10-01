import LocalAuthentication

/// Face ID / Touch ID with the device passcode as fallback.
enum Authenticator {
    static var isAvailable: Bool {
        var error: NSError?
        return LAContext().canEvaluatePolicy(.deviceOwnerAuthentication, error: &error)
    }

    /// Returns nil on success, or a message to show.
    static func authenticate(reason: String) async -> String? {
        let context = LAContext()
        do {
            return try await context.evaluatePolicy(.deviceOwnerAuthentication, localizedReason: reason)
                ? nil : "Authentication failed"
        } catch {
            return error.localizedDescription
        }
    }
}
