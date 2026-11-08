import SwiftUI
import IchorCore

/// The followed image pull: each node's state, Stop while it runs, Close once it ended.
struct ImagePullSheet: View {
    @Environment(\.dismiss) private var dismiss
    private var job: ImagePullJob { .shared }

    var body: some View {
        NavigationStack {
            List {
                Section {
                    Text(verbatim: job.image).font(.callout.monospaced()).textSelection(.enabled)
                    Text(namespaceLabel(job.namespace)).font(.footnote).foregroundStyle(.secondary)
                    if job.progress.total > 0 {
                        ProgressView(value: Double(job.progress.done), total: Double(job.progress.total)) {
                            Text("\(job.progress.done) of \(job.progress.total) nodes done")
                        }
                    } else if job.isRunning {
                        ProgressView()
                    }
                    outcomeText
                }
                Section {
                    ForEach(job.progress.nodes) { PullNodeRow(node: $0) }
                }
            }
            .navigationTitle(Text("Pull an image"))
            .navigationBarTitleDisplayMode(.inline)
            .themedBackground()
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    if job.isRunning {
                        Button("Stop", role: .destructive) { job.stop() }
                    }
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Close") {
                        job.clear()
                        dismiss()
                    }
                }
            }
        }
    }

    @ViewBuilder private var outcomeText: some View {
        switch job.outcome {
        case .succeeded?: Text("Image pulled").foregroundStyle(.statusOK)
        case .stopped?: Text("Image pull stopped").foregroundStyle(.statusWarn)
        case .failed(let message)?: Text(verbatim: message).foregroundStyle(.statusBad)
        case nil: EmptyView()
        }
    }
}

private struct PullNodeRow: View {
    let node: ImagePullNode

    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            HStack {
                Text(verbatim: node.label).font(.callout.monospaced())
                Spacer()
                Text(stateLabel).font(.caption).foregroundStyle(stateColor)
            }
            if !node.error.isEmpty {
                Text(verbatim: node.error).font(.caption).foregroundStyle(.statusBad)
            }
        }
        .accessibilityElement(children: .combine)
    }

    private var stateLabel: String {
        switch node.state {
        case .pending: String(localized: "Waiting")
        case .pulling: String(localized: "Pulling")
        case .done: String(localized: "Pulled")
        case .failed: String(localized: "Failed")
        }
    }

    private var stateColor: Color {
        switch node.state {
        case .pending: .secondary
        case .pulling: .accentColor
        case .done: .statusOK
        case .failed: .statusBad
        }
    }
}

private func namespaceLabel(_ namespace: ImagePullNamespace) -> String {
    switch namespace {
    case .system: String(localized: "Talos system image")
    case .cri: String(localized: "Kubernetes image")
    }
}

/// Asks which image to pull on every node: a Kubernetes image by default, or a Talos system
/// image (an installer).
struct ImagePullRequestSheet: View {
    let onPull: (String, ImagePullNamespace) -> Void

    // Explicit: the private @State properties make the memberwise init private.
    init(onPull: @escaping (String, ImagePullNamespace) -> Void) {
        self.onPull = onPull
    }

    @Environment(\.dismiss) private var dismiss
    @State private var image = ""
    @State private var namespace = ImagePullNamespace.cri

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    TextField("Image", text: $image)
                        .font(.callout.monospaced())
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                    Picker("Pull into", selection: $namespace) {
                        ForEach(ImagePullNamespace.allCases, id: \.self) { Text(namespaceLabel($0)).tag($0) }
                    }
                } footer: {
                    Text("A Talos installer goes into the system images; a workload image into the Kubernetes images.")
                }
            }
            .navigationTitle(Text("Pull an image on every node…"))
            .navigationBarTitleDisplayMode(.inline)
            .themedBackground()
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() } }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Pull") {
                        onPull(image.trimmingCharacters(in: .whitespacesAndNewlines), namespace)
                    }
                    .disabled(image.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
                }
            }
        }
    }
}
