import SwiftUI
import IchorCore

/// The cluster needs its user to sign in (KubeSignInInfo says not signed in, or a call failed
/// with kube-sign-in-required): why, and the way to the sign-in sheet.
struct KubeSignInBanner: View {
    let target: KubeSignInTarget
    /// What the core said after its code, if a call failed.
    var reason: String?
    var onSignedIn: () -> Void

    init(target: KubeSignInTarget, reason: String? = nil, onSignedIn: @escaping () -> Void = {}) {
        self.target = target
        self.reason = reason
        self.onSignedIn = onSignedIn
    }

    @State private var signingIn: KubeSignInTarget?

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            Label("Sign in to use this cluster.", systemImage: "person.badge.key")
                .foregroundStyle(.statusWarn)
            if let reason, !reason.isEmpty {
                Text(reason).font(.footnote).foregroundStyle(.secondary)
            }
            Button("Sign in") { signingIn = target }
                .buttonStyle(.borderedProminent)
        }
        .sheet(item: $signingIn) { KubeSignInSheet(target: $0, onSignedIn: onSignedIn) }
    }
}

/// What a screen shows instead of its data when its load failed for want of a sign-in.
struct KubeSignInRequiredView: View {
    let target: KubeSignInTarget
    let reason: String
    let retry: () async -> Void

    init(target: KubeSignInTarget, reason: String, retry: @escaping () async -> Void) {
        self.target = target
        self.reason = reason
        self.retry = retry
    }

    @State private var signingIn: KubeSignInTarget?

    var body: some View {
        ContentUnavailableView {
            Label("Sign-in needed", systemImage: "person.badge.key")
        } description: {
            Text(reason.isEmpty ? String(localized: "Sign in to use this cluster.") : reason)
        } actions: {
            Button("Sign in") { signingIn = target }
                .buttonStyle(.borderedProminent)
        }
        .sheet(item: $signingIn) { target in
            KubeSignInSheet(target: target) { Task { await retry() } }
        }
    }
}

/// The sign-in of a cluster on its home: who it is signed in as and until when, with a way to
/// sign out; the banner while it is not signed in.
struct KubeSignInSection: View {
    let target: KubeSignInTarget

    init(target: KubeSignInTarget) {
        self.target = target
    }

    @Environment(AppModel.self) private var model
    @State private var info: KubeSignInInfo?
    @State private var signingIn: KubeSignInTarget?
    @State private var error: String?

    var body: some View {
        Section {
            if let info {
                if info.signedIn {
                    if let user = info.user { LabeledContent("Signed in as", value: user) }
                    if info.sessionExpires > 0 {
                        LabeledContent("Session ends", value: Date(timeIntervalSince1970: TimeInterval(info.sessionExpires))
                            .formatted(date: .abbreviated, time: .shortened))
                    }
                    Button("Sign in") { signingIn = target }
                    Button("Sign out", role: .destructive) { Task { await signOut() } }
                } else {
                    KubeSignInBanner(target: target)
                }
            } else if let error {
                Text(error).font(.footnote).foregroundStyle(.statusBad)
            }
        } header: {
            Text("Sign-in")
        }
        .sheet(item: $signingIn) { KubeSignInSheet(target: $0) }
        .task(id: "\(target.id)#\(model.dataGeneration)") { await load() }
    }

    private func load() async {
        do {
            info = try await TalosClient.signInInfo(kube: target.kube, context: target.context, talos: target.talos)
            error = nil
        } catch {
            self.error = error.localizedDescription
        }
    }

    private func signOut() async {
        do {
            try await TalosClient.signOut(kube: target.kube, context: target.context, talos: target.talos)
            model.reloadKubernetes()
        } catch {
            self.error = error.localizedDescription
        }
    }
}
