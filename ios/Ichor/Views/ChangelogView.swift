import SwiftUI
import IchorCore

/// The notes of one release: "0.4.1 · 1 Oct 2026", then its sections with bullets; a release
/// without user-facing changes says so.
struct ReleaseNotes: View {
    let release: ChangelogRelease

    var body: some View {
        Section {
            if release.sections.isEmpty {
                Text("Maintenance release").font(.subheadline).foregroundStyle(.secondary)
            }
            ForEach(release.sections.indices, id: \.self) { index in
                let section = release.sections[index]
                VStack(alignment: .leading, spacing: 6) {
                    Text(section.localizedTitle)
                        .font(.subheadline.weight(.semibold))
                        .foregroundStyle(section.knownKind == .breaking ? Color.orange : Color.primary)
                    ForEach(section.items.indices, id: \.self) { item in
                        HStack(alignment: .firstTextBaseline, spacing: 8) {
                            Text(verbatim: "•").foregroundStyle(.secondary)
                            Text(verbatim: section.items[item]).font(.subheadline)
                        }
                    }
                }
                .padding(.vertical, 2)
            }
        } header: {
            Text(verbatim: header).textCase(nil)
        }
    }

    private var header: String {
        guard let date = release.published else { return release.version }
        return "\(release.version) · \(date.formatted(date: .abbreviated, time: .omitted))"
    }
}

/// The full bundled release history (About → What's new).
struct ChangelogView: View {
    @State private var releases: [ChangelogRelease] = []
    @State private var loaded = false

    var body: some View {
        List {
            ForEach(releases) { ReleaseNotes(release: $0) }
        }
        .overlay {
            if loaded && releases.isEmpty {
                ContentUnavailableView("No release notes available", systemImage: "sparkles")
            }
        }
        .themedBackground()
        .navigationTitle("What's new")
        .navigationBarTitleDisplayMode(.inline)
        .onAppear {
            releases = ChangelogStore.load().releases
            loaded = true
        }
    }
}

/// What the overview presents after an update: the releases since the build launched before.
struct WhatsNewContent: Identifiable {
    let releases: [ChangelogRelease]
    let id = UUID()
}

/// "Updated to 0.4.1" with the notes of every release since the previous build.
struct WhatsNewSheet: View {
    let content: WhatsNewContent
    /// "Full changelog": the sheet closes and the history opens.
    let onFullChangelog: () -> Void

    @Environment(\.dismiss) private var dismiss

    var body: some View {
        NavigationStack {
            List {
                ForEach(content.releases) { ReleaseNotes(release: $0) }
                Section {
                    Button("Full changelog") { onFullChangelog() }
                }
            }
            .navigationTitle(String(localized: "Updated to \(ChangelogStore.currentVersion)"))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .confirmationAction) { Button("OK") { dismiss() } }
            }
        }
    }
}
