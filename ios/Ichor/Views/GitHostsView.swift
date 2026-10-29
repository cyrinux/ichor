import SwiftUI
import IchorCore

/// Settings → Git hosts: a token per host (github.com, a GitLab, Gitea or Forgejo instance) to read and
/// change the repositories Argo CD and Flux deploy from. Tap a host to change its kind or token,
/// swipe to remove it. The tokens are never shown again once saved.
struct GitHostsView: View {
    @Environment(GitHostStore.self) private var store
    @State private var editing: GitHostSheetTarget?
    @State private var confirmRemove: GitHost?

    var body: some View {
        Section {
            if store.hosts.isEmpty {
                Text("No Git host yet. Add one to browse the files an app deploys and propose a change.")
                    .foregroundStyle(.secondary)
            }
            ForEach(store.hosts) { host in
                Button {
                    editing = .edit(host)
                } label: {
                    GitHostRow(host: host, login: store.logins[host.host])
                }
                .foregroundStyle(.primary)
                .swipeActions {
                    Button("Remove", role: .destructive) { confirmRemove = host }
                }
                .contextMenu {
                    Button(role: .destructive) {
                        confirmRemove = host
                    } label: {
                        Label("Remove", systemImage: "trash")
                    }
                }
            }
            Button {
                editing = .add
            } label: {
                Label("Add a Git host", systemImage: "plus")
            }
        } header: {
            Text("Git hosts")
        } footer: {
            Text("A token per Git host, to read and change the repositories Argo CD and Flux deploy from.")
        }
        Section {
            Text(
                "Grant only what Ichor needs. GitHub fine-grained token: Contents and Pull requests, read and write, on the repositories you deploy from. GitLab: api, or read_repository and write_repository. Gitea: repository read and write, and issue write. The token stays on this phone, encrypted, and is only sent to its own host."
            )
            .font(.footnote)
            .foregroundStyle(.secondary)
        }
        .sheet(item: $editing) { target in
            GitHostSheet(existing: target.host)
        }
        .confirmationDialog(
            confirmRemove.map { String(localized: "Remove \($0.host)?") } ?? "",
            isPresented: Binding(get: { confirmRemove != nil }, set: { if !$0 { confirmRemove = nil } }),
            titleVisibility: .visible, presenting: confirmRemove
        ) { host in
            Button("Remove", role: .destructive) { store.remove(host.host) }
            Button("Cancel", role: .cancel) {}
        } message: { _ in
            Text("Its token is deleted from this phone.")
        }
    }
}

/// What the sheet opens on: a new host, or one of the list.
enum GitHostSheetTarget: Identifiable {
    case add
    case edit(GitHost)

    var id: String {
        switch self {
        case .add: ""
        case .edit(let host): host.host
        }
    }

    var host: GitHost? {
        switch self {
        case .add: nil
        case .edit(let host): host
        }
    }
}

/// A host of the list: its name, its kind and who its token last signed in as.
private struct GitHostRow: View {
    let host: GitHost
    let login: String?

    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            HStack {
                Text(verbatim: host.host)
                Spacer()
                GitHostKindLabel(kind: host.kind).font(.caption).foregroundStyle(.secondary)
            }
            if !host.aliases.isEmpty {
                Text("Also \(host.aliases.joined(separator: ", "))")
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .lineLimit(2)
            }
            Group {
                if let login {
                    Text("Signed in as \(login)")
                } else {
                    Text("Not checked")
                }
            }
            .font(.caption)
            .foregroundStyle(.secondary)
        }
    }
}

/// The kind's name: "Auto" is translated, the forges' names are proper nouns.
struct GitHostKindLabel: View {
    let kind: GitHostKind

    var body: some View {
        switch kind {
        case .auto: Text("Auto")
        case .github: Text(verbatim: "GitHub")
        case .gitlab: Text(verbatim: "GitLab")
        case .gitea: Text(verbatim: "Gitea/Forgejo")
        }
    }
}
