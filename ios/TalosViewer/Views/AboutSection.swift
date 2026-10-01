import SwiftUI
import TalosViewerCore

enum ProjectLinks {
    static let repo = URL(string: "https://github.com/cyrinux/talosdev-mobile")!
    static let talos = URL(string: "https://www.talos.dev")!
    static let sponsor = URL(string: "https://github.com/sponsors/cyrinux")!
}

struct AboutSection: View {
    var body: some View {
        Section {
            LabeledContent("Version", value: Self.version)
            Link(destination: ProjectLinks.repo) { Label("Source code on GitHub", systemImage: "chevron.left.forwardslash.chevron.right") }
            Link(destination: ProjectLinks.talos) { Label("Talos Linux (talos.dev)", systemImage: "arrow.up.right.square") }
            Link(destination: ProjectLinks.sponsor) { Label("Sponsor on GitHub", systemImage: "heart") }
        } header: {
            Text("About")
        } footer: {
            Text("Not affiliated with Sidero Labs. Talos is a trademark of Sidero Labs, Inc.")
        }
    }

    static var version: String {
        let info = Bundle.main.infoDictionary
        return "\(info?["CFBundleShortVersionString"] as? String ?? "?") (\(info?["CFBundleVersion"] as? String ?? "?"))"
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
            Text("Enjoying Talos Viewer?").font(.headline)
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
