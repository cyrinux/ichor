import Foundation
import Observation

/// One "tap your security key" prompt: what for, and whether Face ID may be used instead.
@MainActor
final class SecurityKeyPromptRequest: Identifiable {
    let id = UUID()
    let reason: String
    let allowBiometric: Bool
    private var continuation: CheckedContinuation<String?, Never>?

    init(reason: String, allowBiometric: Bool) {
        self.reason = reason
        self.allowBiometric = allowBiometric
    }

    /// Nil on success, or the message to show (Authenticator's convention).
    func resolve(_ failure: String?) {
        continuation?.resume(returning: failure)
        continuation = nil
    }

    func waitForResult() async -> String? {
        await withCheckedContinuation { continuation = $0 }
    }
}

/// The prompts Authenticator shows when a security key is enrolled: a request is posted here
/// and SecurityKeyPromptHost, composed over everything in RootView, shows it and resolves it.
@Observable
@MainActor
final class SecurityKeyPrompts {
    static let shared = SecurityKeyPrompts()

    private(set) var request: SecurityKeyPromptRequest?

    func request(reason: String, allowBiometric: Bool) async -> String? {
        let request = SecurityKeyPromptRequest(reason: reason, allowBiometric: allowBiometric)
        // One prompt at a time: a check started while another waits replaces it.
        self.request?.resolve(String(localized: "Cancelled"))
        self.request = request
        defer { if self.request === request { self.request = nil } }
        return await request.waitForResult()
    }
}
