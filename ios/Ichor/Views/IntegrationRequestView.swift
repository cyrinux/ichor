import SwiftUI
import IchorCore

/// The operators this cluster runs that Ichor does not show yet (os:admin): the user picks API
/// groups and Ichor opens a pre-filled GitHub issue they review before sending. Nothing is
/// sent from the app.
struct IntegrationRequestView: View {
    @Environment(AppModel.self) private var model
    @State private var state: LoadState<IntegrationReport> = .loading

    var body: some View {
        LoadStateView(state: state, retry: load) { report in
            List {
                Section {
                } footer: {
                    Text("Operators running on this cluster that Ichor does not show yet. Pick the API groups you would like to see: Ichor opens a pre-filled GitHub issue you review before sending. Only group, version and kind names go in, never namespaces or object names.")
                }
                if report.families.isEmpty {
                    Section {
                        Text("Everything this cluster runs that Ichor knows how to show is already in the app.")
                            .foregroundStyle(.secondary)
                    }
                }
                ForEach(report.families) { IntegrationFamilySection(family: $0) }
                Section {
                    Link(destination: IntegrationLinks.newRequest) {
                        Label("Suggest something else", systemImage: "plus.bubble")
                    }
                } footer: {
                    if !report.supported.isEmpty {
                        Text("Already shown in Ichor: \(report.supported.joined(separator: ", "))")
                    }
                }
            }
            .refreshable { await load() }
            .themedBackground()
        }
        .navigationTitle("Request an integration")
        .navigationBarTitleDisplayMode(.inline)
        .task { await load() }
    }

    private func load() async {
        guard let client = model.client else { return }
        state = state.refreshed(with: await .from { try await client.integrations() })
    }
}

/// One operator: its groups to tick (none at first), a search for an existing request and the
/// request itself.
private struct IntegrationFamilySection: View {
    let family: IntegrationFamily
    @State private var picked: Set<String> = []
    @State private var error: String?
    @Environment(\.openURL) private var openURL

    var body: some View {
        Section {
            ForEach(family.groups) { group in
                Button { toggle(group.name) } label: {
                    HStack(alignment: .firstTextBaseline, spacing: 10) {
                        Image(systemName: picked.contains(group.name) ? "checkmark.circle.fill" : "circle")
                            .foregroundStyle(picked.contains(group.name) ? Color.accentColor : Color.secondary)
                            .accessibilityHidden(true)
                        VStack(alignment: .leading, spacing: 2) {
                            Text(verbatim: "\(group.name)/\(group.version)").font(.callout.monospaced())
                            if !group.kinds.isEmpty {
                                Text(verbatim: group.kinds.joined(separator: ", "))
                                    .font(.caption)
                                    .foregroundStyle(.secondary)
                            }
                        }
                    }
                }
                .foregroundStyle(.primary)
                .accessibilityAddTraits(picked.contains(group.name) ? .isSelected : [])
            }
            HStack {
                Button("Search existing") {
                    if let url = TalosClient.integrationSearchURL(repo: IntegrationLinks.repo, family: family.id) { openURL(url) }
                }
                .buttonStyle(.bordered)
                Spacer()
                Button("Request") { Task { await request() } }
                    .buttonStyle(.borderedProminent)
                    .disabled(picked.isEmpty)
            }
            if let error { Text(error).font(.footnote).foregroundStyle(.statusBad) }
        } header: {
            Text(verbatim: family.id)
        }
    }

    private func toggle(_ name: String) {
        if picked.contains(name) { picked.remove(name) } else { picked.insert(name) }
    }

    private func request() async {
        do {
            let url = try await TalosClient.integrationIssueURL(repo: IntegrationLinks.repo, family: family.picking(picked), app: IntegrationLinks.app)
            error = nil
            if let url { openURL(url) }
        } catch {
            self.error = error.localizedDescription
        }
    }
}

/// Where integration requests and bug reports go.
enum IntegrationLinks {
    /// "owner/name", from the project's GitHub URL.
    static let repo = String(ProjectLinks.repo.path.drop(while: { $0 == "/" }))
    static let newRequest = ProjectLinks.repo.appendingPathComponent("issues/new").appending(queryItems: [URLQueryItem(name: "template", value: "integration.yml")])
    static let reportBug = ProjectLinks.repo.appendingPathComponent("issues/new/choose")

    /// "Ichor 1.4.0 (iOS)", in the request's App version field.
    static var app: String {
        "Ichor \(Bundle.main.infoDictionary?["CFBundleShortVersionString"] as? String ?? "?") (iOS)"
    }
}
