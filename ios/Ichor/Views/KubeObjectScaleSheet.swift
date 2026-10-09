import SwiftUI
import IchorCore

/// Scales an object of the browser like `kubectl scale`: any kind serving /scale (CRDs
/// included) or a Job (its parallelism; 0 pauses it). Its count is read first; scaling down
/// says how many pods stop, and scaling to 0 needs the object's name typed, as for a workload.
struct KubeObjectScaleSheet: View {
    let resource: KubeAPIResource
    /// "" for a cluster-scoped object.
    let namespace: String
    let name: String
    /// What refuses the scale (the credentials' role): Apply is then disabled, with it.
    let denial: KubeAccess?
    /// Called once scaled, before the sheet closes.
    let onScaled: () -> Void

    @Environment(\.dismiss) private var dismiss
    @Environment(AppModel.self) private var model
    @State private var scale: LoadState<KubeObjectScale> = .loading
    @State private var target = 0
    @State private var typed = ""
    @State private var applying = false
    @State private var failure: String?
    @State private var resultMessage: String?

    // Explicit: the private @State makes the memberwise init private.
    init(resource: KubeAPIResource, namespace: String, name: String, denial: KubeAccess?, onScaled: @escaping () -> Void) {
        self.resource = resource
        self.namespace = namespace
        self.name = name
        self.denial = denial
        self.onScaled = onScaled
    }

    var body: some View {
        NavigationStack {
            LoadStateView(state: scale, retry: loadScale) { scale in
                form(scale)
            }
            .navigationTitle(Text("Scale"))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() }.disabled(applying) }
            }
            .task { await loadScale() }
            .alert(resultMessage ?? "", isPresented: $resultMessage.isPresent()) {
                Button("OK") { dismiss() }
            }
        }
        .interactiveDismissDisabled(applying)
    }

    private func form(_ scale: KubeObjectScale) -> some View {
        Form {
            Section {
                Stepper(value: $target, in: 0...kubeMaxScaleReplicas) {
                    Text(verbatim: target == scale.replicas ? "\(target)" : "\(scale.replicas) → \(target)").monospacedDigit()
                }
                .disabled(applying)
                Text("Running now: \(String(scale.current))").foregroundStyle(.secondary)
            } header: {
                Text(verbatim: name)
            } footer: {
                if scale.isParallelism {
                    Text("A Job runs this many pods at once; 0 pauses it.")
                } else {
                    Text("A HorizontalPodAutoscaler that manages it sets its own count again.")
                }
            }
            if target < scale.replicas && !scaleNeedsTypedName(target) {
                Section {
                    Text("Pods that stop: \(String(scale.replicas - target))").foregroundStyle(.statusWarn)
                }
            }
            if toZero(scale) {
                Section {
                    Text("Every pod of \(name) stops: what it serves is down until it is scaled up again.")
                        .foregroundStyle(.statusWarn)
                }
                Section("Type \(name) to confirm") {
                    TextField("Name", text: $typed, prompt: Text(verbatim: name))
                        .font(.body.monospaced())
                        .autocorrectionDisabled()
                        .textInputAutocapitalization(.never)
                }
            }
            Section {
                if let failure {
                    Label {
                        Text(verbatim: failure).textSelection(.enabled)
                    } icon: {
                        Image(systemName: "exclamationmark.triangle.fill")
                    }
                    .foregroundStyle(.statusBad)
                }
                if let denial { KubeDeniedLine(text: denial.deniedText) }
                Button {
                    Task { await apply() }
                } label: {
                    HStack {
                        if applying { ProgressView() }
                        Text("Apply")
                    }
                }
                .disabled(applying || denial != nil || target == scale.replicas
                          || (toZero(scale) && !typedConfirmationMatches(typed, token: name)))
            }
        }
    }

    private func toZero(_ scale: KubeObjectScale) -> Bool {
        scaleNeedsTypedName(target) && target != scale.replicas
    }

    private func loadScale() async {
        guard let client = model.client else { return }
        let resource = resource
        let namespace = namespace
        let name = name
        scale = .loading
        let result: LoadState<KubeObjectScale> = await .from {
            try await client.objectScale(resource, namespace: namespace, name: name)
        }
        if case .loaded(let value, _, _) = result { target = value.replicas }
        scale = result
    }

    private func apply() async {
        guard let client = model.client, !applying else { return }
        applying = true
        defer { applying = false }
        failure = nil
        let wanted = target
        do {
            let warning = try await client.scaleObject(resource, namespace: namespace, name: name, replicas: wanted)
            onScaled()
            if warning.isEmpty {
                dismiss()
            } else {
                // The autoscaler will undo it: say so before closing.
                resultMessage = String(localized: "\(name) is scaled to \(wanted)") + "\n\n" + warning
            }
        } catch {
            failure = error.localizedDescription
        }
    }
}
