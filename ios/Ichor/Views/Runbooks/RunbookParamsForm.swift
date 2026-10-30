import SwiftUI
import IchorCore

/// The values a runbook asks for, before its plan: one field per parameter, Continue off until
/// every required one is set. `failure` is what the core said of the values last tried. Same
/// as Android's RunbookParamsForm.kt.
struct RunbookParamsForm: View {
    let params: [RunbookParam]
    @Binding var values: [String: String]
    let choices: [String: [String]]
    let failure: String?
    let onContinue: () -> Void

    var body: some View {
        Section("Values for this run") {
            ForEach(params) { param in
                field(param)
            }
        }
        Section {
            if let failure {
                Text(verbatim: failure).font(.footnote).foregroundStyle(.red)
            }
            Button("Continue", action: onContinue)
                .disabled(!RunbookParam.missing(params, values: values).isEmpty)
        }
    }

    @ViewBuilder
    private func field(_ param: RunbookParam) -> some View {
        if param.kind == RunbookParam.kindBool {
            Toggle(
                isOn: Binding(
                    get: { values[param.name] == "true" },
                    set: { values[param.name] = $0 ? "true" : "false" })
            ) {
                Text(verbatim: param.prompt)
            }
        } else {
            HStack {
                TextField(
                    text: Binding(
                        get: { values[param.name] ?? "" },
                        set: { values[param.name] = $0 })
                ) {
                    Text(verbatim: param.prompt)
                }
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
                .keyboardType(param.kind == RunbookParam.kindInt ? .numberPad : .default)
                if let names = choices[param.kind], !names.isEmpty {
                    Menu {
                        ForEach(names, id: \.self) { name in
                            Button {
                                values[param.name] = name
                            } label: {
                                Text(verbatim: name)
                            }
                        }
                    } label: {
                        Image(systemName: "chevron.up.chevron.down")
                    }
                    .accessibilityLabel(Text(verbatim: param.prompt))
                }
            }
        }
    }
}

extension TalosClient {
    /// What the cluster offers for each parameter's kind; empty when it cannot say: the user
    /// types the value.
    func runbookValueChoices(_ params: [RunbookParam]) async -> [String: [String]] {
        var choices: [String: [String]] = [:]
        for kind in Set(params.map(\.kind)) {
            var names: [String] = []
            switch kind {
            case RunbookParam.kindNamespace:
                names = (try? await namespaces().namespaces) ?? []
            case RunbookParam.kindWorkload:
                for workloadKind in ["Deployment", "StatefulSet", "DaemonSet"] {
                    let page = try? await workloadsPage(kind: workloadKind, namespace: nil, token: "")
                    names += page?.items.map(\.name) ?? []
                }
            case RunbookParam.kindNode:
                names = (try? await overview().nodes.map(\.node)) ?? []
            default:
                break
            }
            choices[kind] = Array(Set(names)).sorted()
        }
        return choices
    }
}
