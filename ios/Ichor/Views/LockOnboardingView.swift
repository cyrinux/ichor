import SwiftUI

/// Shown instead of the app while a real cluster is stored without the app lock (right after
/// the first import, or on updating from a version where the lock was optional). The only
/// ways forward are enabling the lock or deleting the stored config.
struct LockOnboardingView: View {
    @Environment(AppModel.self) private var model
    @Environment(\.scenePhase) private var scenePhase
    @Environment(\.openURL) private var openURL
    @State private var available = Authenticator.isAvailable
    @State private var error: String?
    @State private var done = false
    @State private var shownFeatures = 0
    @State private var confirmDelete = false

    private static let features: [(icon: String, title: LocalizedStringKey, detail: LocalizedStringKey)] = [
        ("faceid", "Face ID, Touch ID or passcode", "Asked when Ichor opens and after 30 seconds in the background."),
        ("power", "A check before risky actions", "Reboot, shutdown, upgrades and config exports ask for it first."),
        ("eye.slash", "Private by default", "The app switcher shows a blank cover, and alerts hide cluster details on the lock screen."),
    ]

    var body: some View {
        ScrollView {
            VStack(spacing: 24) {
                ShieldHero(done: done)
                VStack(spacing: 8) {
                    Text(done ? "Your clusters are protected" : "Lock your clusters away")
                        .font(.title2.bold())
                        .contentTransition(.opacity)
                    Text("Your talosconfig holds client keys with direct access to your nodes. Ichor keeps them behind the app lock.")
                        .foregroundStyle(.secondary)
                }
                .multilineTextAlignment(.center)
                VStack(alignment: .leading, spacing: 14) {
                    ForEach(Array(Self.features.enumerated()), id: \.offset) { index, feature in
                        FeatureRow(icon: feature.icon, title: feature.title, detail: feature.detail)
                            .opacity(index < shownFeatures ? 1 : 0)
                            .offset(y: index < shownFeatures ? 0 : 16)
                    }
                }
                if !done { actions.transition(.opacity) }
            }
            .padding(24)
            .frame(maxWidth: 480)
            .frame(maxWidth: .infinity)
        }
        .scrollBounceBehavior(.basedOnSize)
        .background(Color(.systemBackground))
        .task { await revealFeatures() }
        .task(id: done) {
            guard done else { return }
            try? await Task.sleep(for: .seconds(1.1))
            model.setLockEnabled(true)
        }
        // Back from the Settings app with a passcode set up.
        .onChange(of: scenePhase) { _, phase in
            if phase == .active { available = Authenticator.isAvailable }
        }
        .sensoryFeedback(.success, trigger: done)
        .confirmationDialog("Delete talosconfig?", isPresented: $confirmDelete, titleVisibility: .visible) {
            Button("Delete", role: .destructive) { model.clear() }
        } message: {
            Text("The config of every cluster, with their client keys, will be removed from this device.")
        }
    }

    @ViewBuilder
    private var actions: some View {
        VStack(spacing: 12) {
            if available {
                Button { Task { await enable() } } label: {
                    Label("Enable app lock", systemImage: "faceid").frame(maxWidth: .infinity)
                }
                .buttonStyle(.borderedProminent)
                .controlSize(.large)
                if let error { Text(error).font(.footnote).foregroundStyle(.red).multilineTextAlignment(.center) }
            } else {
                VStack(alignment: .leading, spacing: 8) {
                    Label("No screen lock on this device", systemImage: "exclamationmark.lock.fill").font(.headline)
                    Text("Set a passcode (and optionally Face ID or Touch ID) in the Settings app, then come back.")
                        .font(.footnote)
                    Button("Open Settings") {
                        if let url = URL(string: UIApplication.openSettingsURLString) { openURL(url) }
                    }
                    .buttonStyle(.borderedProminent)
                    .frame(maxWidth: .infinity)
                }
                .padding()
                .background(.red.opacity(0.12), in: RoundedRectangle(cornerRadius: 16))
            }
            Button("Delete the imported config instead") { confirmDelete = true }
                .font(.footnote)
        }
    }

    private func revealFeatures() async {
        while shownFeatures < Self.features.count {
            try? await Task.sleep(for: .milliseconds(120))
            withAnimation(.spring(duration: 0.45)) { shownFeatures += 1 }
        }
    }

    private func enable() async {
        if let message = await Authenticator.authenticate(reason: String(localized: "Enable app lock")) {
            error = message
        } else {
            withAnimation(.spring) { done = true }
        }
    }
}

/// A shield with slow pulsing rings; it turns into a check once the lock is on.
private struct ShieldHero: View {
    let done: Bool
    @State private var pulsing = false

    var body: some View {
        ZStack {
            ForEach(0..<2) { ring in
                Circle()
                    .fill(.tint)
                    .frame(width: 96, height: 96)
                    .scaleEffect(pulsing ? 1.65 : 1)
                    .opacity(pulsing ? 0 : 0.35)
                    .animation(.easeOut(duration: 2.4).repeatForever(autoreverses: false).delay(Double(ring) * 1.2), value: pulsing)
            }
            Circle().fill(.tint.opacity(0.2)).background(Circle().fill(Color(.systemBackground))).frame(width: 96, height: 96)
            Image(systemName: done ? "checkmark.shield.fill" : "lock.shield")
                .font(.system(size: 44))
                .foregroundStyle(.tint)
                .contentTransition(.symbolEffect(.replace))
        }
        .frame(width: 160, height: 160)
        .onAppear { pulsing = true }
        .accessibilityHidden(true)
    }
}

private struct FeatureRow: View {
    let icon: String
    let title: LocalizedStringKey
    let detail: LocalizedStringKey

    var body: some View {
        HStack(spacing: 16) {
            Image(systemName: icon)
                .font(.title3)
                .foregroundStyle(.tint)
                .frame(width: 44, height: 44)
                .background(.tint.opacity(0.15), in: Circle())
            VStack(alignment: .leading, spacing: 2) {
                Text(title).font(.subheadline.weight(.semibold))
                Text(detail).font(.footnote).foregroundStyle(.secondary)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }
}
