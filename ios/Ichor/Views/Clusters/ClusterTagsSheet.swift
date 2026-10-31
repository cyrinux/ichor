import IchorCore
import SwiftUI

/// The tags of a cluster, comma-separated in one field; a tap on a tag of another cluster adds
/// it. Same content as Android's ClusterTagsDialog. Not destructive.
struct ClusterTagsSheet: View {
    let context: ContextSummary

    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    @State private var text = ""

    private var own: [String] { clusterFingerprints(of: context, contexts: model.summary?.contexts ?? []) }

    var body: some View {
        let suggestions = tagSuggestions(saved: model.clusterTags, own: own, typed: text)
        NavigationStack {
            Form {
                Section {
                    TextField("Tags", text: $text)
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                } footer: {
                    Text("Separated by commas. Kept on this device and in backups, never in the config.")
                }
                if !suggestions.isEmpty {
                    Section("Used on other clusters") {
                        ForEach(suggestions, id: \.self) { tag in
                            Button {
                                text = (normalizeTags(text) + [tag]).joined(separator: ", ")
                            } label: {
                                Label {
                                    Text(verbatim: tag)
                                } icon: {
                                    Image(systemName: "plus")
                                }
                            }
                        }
                    }
                }
            }
            .navigationTitle(String(localized: "Tags of \(model.labels.of(context))"))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { dismiss() }
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button("OK") {
                        model.setTags(text, for: context)
                        dismiss()
                    }
                }
            }
            .onAppear { text = clusterTagsOf(saved: model.clusterTags, fingerprints: own).joined(separator: ", ") }
        }
    }
}
