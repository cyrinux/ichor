import SwiftUI
import IchorCore

/// What a rollback of `workload` to `revision` undoes: the images from → to on top, then the
/// pod template diff, and "Roll back to revision N" behind the typed workload name
/// (HostnameConfirmationSheet). `history` says whether the pods only update when deleted
/// (OnDelete) and whether a paused Deployment refuses it. `rollback` runs once confirmed and
/// closes this sheet.
struct RevisionDiffSheet: View {
    let workload: KubeWorkload
    let revision: DeploymentRevision
    let history: WorkloadRevisionList
    let rollback: () async -> Void

    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    @State private var diff: LoadState<RevisionDiff> = .loading
    @State private var confirm = false

    private var rollbackAction: KubeAction? { KubeAction.rollback(kind: workload.kind) }

    var body: some View {
        NavigationStack {
            List {
                Section {
                    Text(verbatim: "\(workload.kind) · \(workload.namespace)")
                        .font(.caption.monospaced())
                        .foregroundStyle(.secondary)
                    if revision.created > 0 {
                        Text(relativeTime(revision.created)).font(.caption).foregroundStyle(.secondary)
                    }
                    if !revision.changeCause.isEmpty {
                        Text(verbatim: revision.changeCause).font(.caption)
                    }
                }
                switch diff {
                case .loading:
                    Section { ProgressView() }
                case .failed(let message):
                    Section { Text(verbatim: message).note() }
                case .loaded(let d, _, _):
                    diffSections(d)
                }
                Section {
                    Button(role: .destructive) {
                        confirm = true
                    } label: {
                        Text("Roll back to revision \(revision.revision)")
                    }
                    .disabled(history.paused || revision.current)
                    .kubeGated(rollbackAction)
                    KubeDeniedNote(rollbackAction)
                } footer: {
                    VStack(alignment: .leading, spacing: 4) {
                        if history.manual {
                            Text(
                                "Pods update when deleted (OnDelete): a rollback changes the template, then delete the pods to apply it."
                            )
                            .foregroundStyle(Color.statusWarn)
                        }
                        if history.paused {
                            Text("The rollout is paused: resume it before rolling back.")
                                .foregroundStyle(Color.statusWarn)
                        }
                    }
                }
            }
            .loadsKubeActionAccess(namespace: workload.namespace)
            .themedBackground()
            .navigationTitle(Text("Revision \(revision.revision) of \(workload.name)"))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() } }
            }
            .task { await load() }
            .sheet(isPresented: $confirm) {
                HostnameConfirmationSheet(
                    title: String(localized: "Roll back to revision \(revision.revision)?"),
                    message: String(
                        localized:
                            "\(workload.name) gets the pod template of revision \(revision.revision) back: its pods are replaced with a rolling update, like kubectl rollout undo."
                    ),
                    hostname: workload.name,
                    actionTitle: String(localized: "Roll back")
                ) {
                    confirm = false
                    Task { await rollback() }
                }
            }
        }
    }

    @ViewBuilder
    private func diffSections(_ d: RevisionDiff) -> some View {
        let changes = d.images.changes
        if !changes.isEmpty {
            Section("Images") {
                ForEach(changes, id: \.self) { change in
                    ImageChangeRow(change: change)
                }
            }
        }
        let lines = parseDiffLines(d.diff)
        Section("Pod template changes") {
            if lines.isEmpty {
                Text("Same pod template as the current revision.").note()
            } else {
                DiffLinesView(lines: lines)
                    .listRowInsets(EdgeInsets(top: 6, leading: 8, bottom: 6, trailing: 8))
            }
        }
    }

    private func load() async {
        guard let client = model.client else { return }
        let number = revision.revision
        diff = diff.refreshed(with: await .from { try await client.revisionDiff(workload, revision: number) })
    }
}

/// "postgres:17 → postgres:16", read as "postgres:17, back to postgres:16".
private struct ImageChangeRow: View {
    let change: ImageChange

    var body: some View {
        let none = String(localized: "none")
        let from = change.from.isEmpty ? none : change.from
        let to = change.to.isEmpty ? none : change.to
        Text(verbatim: "\(from) → \(to)")
            .font(.callout.monospaced())
            .foregroundStyle(Color.statusWarn)
            .lineLimit(2)
            .truncationMode(.middle)
            .accessibilityLabel(Text("\(from), back to \(to)"))
    }
}
