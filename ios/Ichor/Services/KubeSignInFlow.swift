import AuthenticationServices
import Foundation
import IchorCore
import Observation
import UIKit

/// One sign-in of a kubeconfig cluster, as the sign-in sheet drives it: credentials checked by
/// the Go core, then (EKS IAM Identity Center, OIDC, Azure) the browser or a device code.
/// Only ever started by the user: background checks never open a browser.
@Observable
@MainActor
final class KubeSignInFlow {
    enum Phase: Equatable {
        case idle
        case working
        /// The provider's page is open in the browser sheet; Go gets the answer itself.
        case browser
        /// A code to enter on the provider's page.
        case device(KubeSignInPrompt)
        case signedIn
        case failed(String)
    }

    private(set) var phase = Phase.idle
    @ObservationIgnored private var cancelRun: (@Sendable () -> Void)?
    @ObservationIgnored private var completeRun: (@Sendable (String) -> Void)?
    @ObservationIgnored private var web: WebSignInSession?
    @ObservationIgnored private var cancelled = false
    @ObservationIgnored private var runID = 0

    var isRunning: Bool {
        switch phase {
        case .working, .browser, .device: true
        case .idle, .signedIn, .failed: false
        }
    }

    /// Signs `target` in with `secrets` (KubeSetCredentials); a method that completes in the
    /// browser goes on with the interactive sign-in.
    func signIn(_ target: KubeSignInTarget, secrets: String) async {
        phase = .working
        do {
            try await TalosClient.setCredentials(kube: target.kube, context: target.context, secrets: secrets)
            phase = .signedIn
        } catch {
            let message = error.localizedDescription
            if isKubeSignInRequired(message) {
                start(target)
            } else {
                phase = .failed(message)
            }
        }
    }

    /// Starts the browser or device-code sign-in of `target`.
    func start(_ target: KubeSignInTarget) {
        stopRun()
        runID += 1
        let id = runID
        cancelled = false
        phase = .working
        let run = TalosClient.startSignIn(kube: target.kube, context: target.context)
        cancelRun = run.cancel
        completeRun = run.complete
        Task {
            for await event in run.events {
                guard id == self.runID else { return }
                switch event {
                case .prompt(let prompt): self.show(prompt)
                case .done(let error): self.finish(error)
                }
            }
        }
    }

    /// Stops waiting for the user; nothing is stored.
    func cancel() {
        cancelled = true
        stopRun()
        if isRunning { phase = .idle }
    }

    private func stopRun() {
        cancelRun?()
        cancelRun = nil
        completeRun = nil
        web?.cancel()
        web = nil
    }

    private func show(_ prompt: KubeSignInPrompt) {
        if prompt.isDevice {
            phase = .device(prompt)
            return
        }
        guard let url = URL(string: prompt.url) else {
            phase = .failed(String(localized: "The sign-in page address is not valid."))
            cancelRun?()
            return
        }
        phase = .browser
        let session = WebSignInSession()
        web = session
        let id = runID
        let started = session.start(url: url) { [weak self] callback in
            guard let self, id == self.runID else { return }
            if let callback {
                // The provider came back to the app rather than to Go's loopback address.
                self.completeRun?(callback.absoluteString)
            } else if self.phase == .browser {
                // The user closed the browser sheet.
                self.cancel()
            }
        }
        // No browser sheet: the system browser, the answer still reaches Go's address.
        if !started { UIApplication.shared.open(url) }
    }

    private func finish(_ error: String?) {
        let wasCancelled = cancelled
        web?.cancel()
        web = nil
        cancelRun = nil
        completeRun = nil
        if let error {
            phase = .failed(error)
        } else {
            // Go reports a cancelled sign-in as a success too.
            phase = wasCancelled ? .idle : .signedIn
        }
    }
}

/// The provider's sign-in page in an in-app browser sheet. The provider redirects to the
/// loopback address the Go core listens on; Go then reports the end and the sheet is closed.
final class WebSignInSession: NSObject, ASWebAuthenticationPresentationContextProviding {
    private var session: ASWebAuthenticationSession?

    /// The app's own scheme: what a provider set up for the app would come back to.
    private static let callbackScheme = "ichor"

    /// Opens `url`; `ended` gets the URL the browser came back with, or nil when it was closed.
    /// False when the sheet could not be shown.
    @MainActor
    func start(url: URL, ended: @escaping @MainActor (URL?) -> Void) -> Bool {
        let session = ASWebAuthenticationSession(url: url, callbackURLScheme: Self.callbackScheme) { callback, _ in
            Task { @MainActor in ended(callback) }
        }
        session.presentationContextProvider = self
        // Shares the browser's cookies: an identity provider the user is signed in to already
        // does not ask again.
        session.prefersEphemeralWebBrowserSession = false
        self.session = session
        return session.start()
    }

    @MainActor
    func cancel() {
        session?.cancel()
        session = nil
    }

    func presentationAnchor(for session: ASWebAuthenticationSession) -> ASPresentationAnchor {
        MainActor.assumeIsolated {
            let windows = UIApplication.shared.connectedScenes
                .compactMap { $0 as? UIWindowScene }
                .flatMap(\.windows)
            return windows.first(where: \.isKeyWindow) ?? windows.first ?? ASPresentationAnchor()
        }
    }
}
