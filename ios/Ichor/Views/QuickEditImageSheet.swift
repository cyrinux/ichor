import SwiftUI
import IchorCore

/// Changes the tag of one image a source file sets: a container, or the kustomization's
/// `images:` entry (which wins over the manifests). The previous tag and the one running now
/// are offered when known. The file is rewritten by the Go core (comments kept) and checked
/// here; the editor then applies it at the branch and opens the review.
struct QuickEditImageSheet: View {
    let targets: [QuickEditImageTarget]
    /// The file as shown, to check the edit changes something.
    let content: String
    let app: GitOpsAppRef
    /// The edit chosen; the caller closes the sheet and opens the editor.
    let onApply: (QuickEdit) -> Void

    // Explicit: the private @State properties make the memberwise init private.
    init(targets: [QuickEditImageTarget], content: String, app: GitOpsAppRef, onApply: @escaping (QuickEdit) -> Void) {
        self.targets = targets
        self.content = content
        self.app = app
        self.onApply = onApply
        let first = targets.first
        _place = State(initialValue: first?.place)
        _tag = State(initialValue: first.map { imageTag($0.image) } ?? "")
    }

    @Environment(\.dismiss) private var dismiss
    @Environment(AppModel.self) private var model
    @State private var place: QuickEditImagePlace?
    @State private var tag: String
    @State private var checking = false
    @State private var message: String?
    /// The images the app runs now (an Argo CD app's), for the "Running now" suggestion.
    @State private var live: [String] = []

    private var target: QuickEditImageTarget? { targets.first { $0.place == place } ?? targets.first }

    private var trimmedTag: String { tag.trimmingCharacters(in: .whitespacesAndNewlines) }

    var body: some View {
        NavigationStack {
            Group {
                if let target { form(target) }
            }
            .navigationTitle(Text("Change image tag"))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() }.disabled(checking) }
            }
        }
        .interactiveDismissDisabled(checking)
        .task { await loadLive() }
        .onChange(of: place) { _, _ in
            tag = target.map { imageTag($0.image) } ?? ""
            message = nil
        }
        .onChange(of: tag) { _, _ in message = nil }
    }

    private func form(_ target: QuickEditImageTarget) -> some View {
        Form {
            Section {
                if targets.count > 1 {
                    Picker("Container", selection: $place) {
                        ForEach(targets) { option in
                            Text(verbatim: option.label).tag(Optional(option.place))
                        }
                    }
                } else {
                    LabeledContent("Container") { Text(verbatim: target.label) }
                }
                LabeledContent("Image") {
                    Text(verbatim: target.image)
                        .font(.caption.monospaced())
                        .lineLimit(2)
                        .truncationMode(.middle)
                        .textSelection(.enabled)
                }
            } footer: {
                if target.isKustomize {
                    Text("This kustomization sets the image: the change goes to its images entry.")
                }
            }
            Section("Tag") {
                TextField("Tag", text: $tag)
                    .font(.body.monospaced())
                    .autocorrectionDisabled()
                    .textInputAutocapitalization(.never)
                suggestionChips(target)
            }
            Section {
                if let message {
                    Label {
                        Text(verbatim: message).textSelection(.enabled)
                    } icon: {
                        Image(systemName: "info.circle")
                    }
                    .foregroundStyle(.secondary)
                }
                Button {
                    Task { await review(target) }
                } label: {
                    HStack {
                        if checking { ProgressView() }
                        Text("Review change")
                    }
                }
                .disabled(checking || trimmedTag.isEmpty)
            } footer: {
                Text("For routine bumps, Flux image automation or Argo CD Image Updater can open these changes for you.")
            }
        }
        .themedBackground()
        .disabled(checking)
    }

    /// "Previous tag" and "Running now", each when it is another tag than the file's.
    @ViewBuilder private func suggestionChips(_ target: QuickEditImageTarget) -> some View {
        // Argo CD keeps no images in its history: only the live ones are known for now.
        let suggestions = quickEditTagSuggestions(for: target.image, history: [], live: live)
        if !suggestions.isEmpty {
            HStack(spacing: 8) {
                if let previous = suggestions.previous {
                    chip(LocalizedStringKey("Previous tag"), tag: previous)
                }
                if let running = suggestions.running {
                    chip(LocalizedStringKey("Running now"), tag: running)
                }
            }
        }
    }

    private func chip(_ title: LocalizedStringKey, tag value: String) -> some View {
        Button { tag = value } label: {
            VStack(alignment: .leading, spacing: 2) {
                Text(title).font(.caption2).foregroundStyle(.secondary)
                Text(verbatim: value).font(.caption.monospaced()).lineLimit(1)
            }
        }
        .buttonStyle(.bordered)
        .accessibilityElement(children: .combine)
    }

    /// The images the Argo CD app runs now; nothing for Flux, or when it cannot be read.
    private func loadLive() async {
        guard app.tool == "argocd", let client = model.client,
              let status = try? await client.argoCD() else { return }
        live = status.apps.first { $0.namespace == app.namespace && $0.name == app.name }?.images ?? []
    }

    /// Checks the edit changes the file, then hands it on.
    private func review(_ target: QuickEditImageTarget) async {
        let edit = target.edit(tag: trimmedTag)
        checking = true
        defer { checking = false }
        message = nil
        do {
            let result = try await TalosClient.quickEdit(content, edit)
            if result.changed {
                onApply(edit)
            } else {
                message = String(localized: "This value is already set.")
            }
        } catch {
            message = error.localizedDescription
        }
    }
}
