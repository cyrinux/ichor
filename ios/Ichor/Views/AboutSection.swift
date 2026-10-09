import SwiftUI
import IchorCore

enum ProjectLinks {
    static let repo = URL(string: "https://github.com/cyrinux/ichor")!
    static let talos = URL(string: "https://www.talos.dev")!
    static let sponsor = URL(string: "https://github.com/sponsors/cyrinux")!
    static let btc = Donation.btc
    static let eth = Donation.eth
}

struct AboutSection: View {
    @State private var donation: Donation?

    var body: some View {
        Section {
            LabeledContent("Version", value: Self.version)
            NavigationLink(value: Route.changelog) { Label("What's new", systemImage: "sparkles") }
            Link(destination: ProjectLinks.repo) { Label("Source code on GitHub", systemImage: "chevron.left.forwardslash.chevron.right") }
            Link(destination: ProjectLinks.talos) { Label("Talos Linux (talos.dev)", systemImage: "arrow.up.right.square") }
            NavigationLink(value: Route.licenses) { Label("Open-source licenses", systemImage: "doc.text") }
            NavigationLink(value: Route.supportedIntegrations) { Label("Integrations", systemImage: "puzzlepiece.extension") }
            if Distribution.donations {
                Link(destination: ProjectLinks.sponsor) { Label("Sponsor on GitHub", systemImage: "heart") }
                ForEach(Donation.allCases) { coin in
                    Button { donation = coin } label: { DonationRow(coin: coin) }
                }
            }
        } header: {
            Text("About")
        } footer: {
            VStack(alignment: .leading, spacing: 6) {
                Text("Not affiliated with Sidero Labs. Talos is a trademark of Sidero Labs, Inc.")
                Text("App icons: Dashboard Icons (Apache 2.0) and selfh.st/icons (CC BY 4.0). App names and logos are trademarks of their owners.")
            }
        }
        .sheet(item: $donation) { coin in DonationSheet(coin: coin) }
    }

    static var version: String {
        let info = Bundle.main.infoDictionary
        return "\(info?["CFBundleShortVersionString"] as? String ?? "?") (\(info?["CFBundleVersion"] as? String ?? "?"))"
    }
}

/// Where this build's updates come from: Apple for the App Store build, the user otherwise
/// (a sideloaded app cannot update itself).
struct UpdatesSection: View {
    var body: some View {
        Section {
            Text(Distribution.appStore
                 ? LocalizedStringKey("This build gets its updates from the App Store, or from TestFlight for a beta.")
                 : LocalizedStringKey("This build cannot update itself: install each new release from GitHub again, with Sideloadly or AltStore."))
                .font(.callout)
                .foregroundStyle(.secondary)
        } header: {
            Text("Updates")
        }
    }
}

extension Donation {
    var title: LocalizedStringKey {
        switch self {
        case .bitcoin: "Donate in Bitcoin"
        case .ethereum: "Donate in Ethereum"
        }
    }

    var symbol: String {
        switch self {
        case .bitcoin: "bitcoinsign.circle"
        case .ethereum: "diamond"
        }
    }
}

private struct DonationRow: View {
    let coin: Donation

    var body: some View {
        Label {
            VStack(alignment: .leading, spacing: 2) {
                Text(coin.title)
                Text(verbatim: coin.address)
                    .font(.caption.monospaced())
                    .foregroundStyle(.secondary)
                    .lineLimit(1)
                    .truncationMode(.middle)
            }
        } icon: {
            Image(systemName: coin.symbol)
        }
    }
}

/// QR code, full address, copy and open-in-wallet for one donation address.
private struct DonationSheet: View {
    let coin: Donation
    @Environment(\.dismiss) private var dismiss
    @Environment(\.openURL) private var openURL
    @State private var qrImage: UIImage?
    @State private var copied = false
    @State private var noWallet = false

    var body: some View {
        NavigationStack {
            ScrollView {
                VStack(spacing: 20) {
                    if let qrImage {
                        Image(uiImage: qrImage)
                            .interpolation(.none)
                            .resizable()
                            .scaledToFit()
                            .frame(maxWidth: 260)
                            .accessibilityLabel(Text(coin.title))
                    }
                    Text(verbatim: coin.address)
                        .font(.body.monospaced())
                        .multilineTextAlignment(.center)
                        .textSelection(.enabled)
                    Button(action: copy) {
                        Label(copied ? LocalizedStringKey("Copied") : LocalizedStringKey("Copy address"), systemImage: copied ? "checkmark" : "doc.on.doc")
                    }
                    .buttonStyle(.bordered)
                    if let url = URL(string: coin.uri) {
                        Button {
                            openURL(url) { accepted in noWallet = !accepted }
                        } label: {
                            Label("Open in wallet", systemImage: "wallet.pass")
                        }
                        .buttonStyle(.borderedProminent)
                    }
                    if noWallet {
                        Text("No wallet app found. Copy the address instead.")
                            .font(.footnote)
                            .foregroundStyle(.secondary)
                    }
                }
                .padding()
                .frame(maxWidth: .infinity)
            }
            .navigationTitle(coin.title)
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .confirmationAction) { Button("Done") { dismiss() } }
            }
        }
        .onAppear { qrImage = makeQRCode(coin.uri) }
    }

    private func copy() {
        UIPasteboard.general.string = coin.address
        copied = true
        Task {
            try? await Task.sleep(for: .seconds(2))
            copied = false
        }
    }
}

/// Occasional, dismissable ask to support the project (timing rules in SupportState).
@Observable
@MainActor
final class SupportPrompt {
    private enum Keys {
        static let first = "support.firstSeen", launches = "support.launches", asked = "support.lastAsked", never = "support.never"
    }

    private(set) var visible = false

    func onLaunch() {
        let defaults = UserDefaults.standard
        let now = Date()
        let first = defaults.object(forKey: Keys.first) as? Date ?? now
        defaults.set(first, forKey: Keys.first)
        let launches = defaults.integer(forKey: Keys.launches) + 1
        defaults.set(launches, forKey: Keys.launches)
        visible = SupportState(firstSeen: first, launches: launches,
                               lastAsked: defaults.object(forKey: Keys.asked) as? Date,
                               never: defaults.bool(forKey: Keys.never)).shouldAsk(now: now)
    }

    func later() {
        UserDefaults.standard.set(Date(), forKey: Keys.asked)
        visible = false
    }

    func never() {
        UserDefaults.standard.set(true, forKey: Keys.never)
        visible = false
    }
}

struct SupportCard: View {
    let prompt: SupportPrompt
    @Environment(\.openURL) private var openURL

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            Text("Enjoying Ichor?").font(.headline)
            Text("It is free and open source. If it saves you time, you can support its development.")
                .font(.footnote).foregroundStyle(.secondary)
            HStack {
                Spacer()
                Button("Don't ask again") { prompt.never() }
                Button("Not now") { prompt.later() }
                Button("Sponsor") {
                    prompt.later()
                    openURL(ProjectLinks.sponsor)
                }
                .fontWeight(.semibold)
            }
            .buttonStyle(.borderless)
            .font(.footnote)
        }
    }
}
