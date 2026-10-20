import SwiftUI
import IchorCore

/// The edit's build check under the review's diff: building, then "Builds" with the rendered
/// objects that change (each unfolds its diff), "Build fails" with the errors and the file and
/// line they name, or why the file is not checked (a Helm chart). Continue stays possible
/// either way: the person decides.
struct EditCheckSection: View {
    let state: LoadState<GitOpsEditCheck>
    let retry: () -> Void

    @State private var expanded: Set<String> = []

    var body: some View {
        Section {
            switch state {
            case .loading:
                HStack(spacing: 8) {
                    ProgressView()
                    Text("Building the kustomization with your change…").foregroundStyle(.secondary)
                }
            case .failed(let message):
                VStack(alignment: .leading, spacing: 6) {
                    Text("Could not check the build: \(message)").foregroundStyle(.secondary)
                    Button("Retry", action: retry)
                }
            case .loaded(let check, _, _):
                result(check)
            }
        } header: {
            Text("Build check")
        }
    }

    @ViewBuilder private func result(_ c: GitOpsEditCheck) -> some View {
        if c.broken {
            Label("Already fails at this commit", systemImage: "exclamationmark.triangle").foregroundStyle(.statusWarn)
            ForEach(Array(c.errors.enumerated()), id: \.offset) { _, e in errorRow(e) }
        } else if !c.ok {
            Label("Build fails", systemImage: "xmark.octagon").foregroundStyle(.statusBad)
            ForEach(Array(c.errors.enumerated()), id: \.offset) { _, e in errorRow(e) }
        } else if c.built {
            Group {
                if c.kind == "plain" { Label("Valid YAML", systemImage: "checkmark.circle") } else { Label("Builds", systemImage: "checkmark.circle") }
            }
            .foregroundStyle(.statusOK)
            if c.kind != "plain" {
                Text("Built from \(c.dir.isEmpty ? "/" : c.dir)").font(.caption).foregroundStyle(.secondary)
            }
            if c.resources.isEmpty {
                Text("No rendered object changes.").font(.caption).foregroundStyle(.secondary)
            } else {
                Text("Objects that change: \(c.resources.count) · unchanged: \(c.unchanged)")
                    .font(.caption).foregroundStyle(.secondary)
                ForEach(c.resources) { r in
                    DiffResourceRow(resource: r, expanded: binding(r.id))
                }
            }
        } else {
            Label("Not checked", systemImage: "questionmark.circle").foregroundStyle(.secondary)
        }
        if !c.note.isEmpty {
            Text(verbatim: c.note).font(.caption).foregroundStyle(.secondary)
        }
    }

    private func errorRow(_ e: EditCheckError) -> some View {
        VStack(alignment: .leading, spacing: 2) {
            if !e.path.isEmpty {
                Group {
                    if e.line > 0 { Text("\(e.path), line \(e.line)") } else { Text(verbatim: e.path) }
                }
                .font(.caption.monospaced().weight(.semibold))
            }
            Text(verbatim: e.message).font(.caption.monospaced())
        }
    }

    private func binding(_ id: String) -> Binding<Bool> {
        Binding(get: { expanded.contains(id) }, set: { on in
            if on { expanded.insert(id) } else { expanded.remove(id) }
        })
    }
}
