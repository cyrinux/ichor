import SwiftUI
import IchorCore

// A plugin's action buttons (docs/plugins.md "Actions"): the params form, the confirmation the
// plugin asks for (none, a plain question, or the object's name typed), the call to
// KubePluginAction and its outcome, said by the tab. Go checks the object again and decides:
// a button that is on is no promise. The params are never logged.

/// What a plugin's buttons need from the tab they sit in.
struct PluginActionContext {
    let pluginID: String
    /// How a run ended, for the tab to say; it reads again when `reload`.
    let finished: (PluginActionOutcome) -> Void
}

/// The end of a run: what to say, whether it went through, whether the rows are stale.
struct PluginActionOutcome {
    let message: String
    let succeeded: Bool
    let reload: Bool
}

private struct PluginActionContextKey: EnvironmentKey {
    static var defaultValue: PluginActionContext? { nil }
}

extension EnvironmentValues {
    /// Set by the plugin tab; nil elsewhere (the buttons stay off).
    var pluginActions: PluginActionContext? {
        get { self[PluginActionContextKey.self] }
        set { self[PluginActionContextKey.self] = newValue }
    }
}

/// A plugin's button on its row's object: on when the plugin enables it and the credentials are
/// not known to be refused (an unknown answer keeps it on, the API server still decides).
struct PluginActionButton: View {
    let action: PluginActionItem
    /// The row's object; nil when the row has none (the button stays off).
    let target: PluginRef?

    @Environment(AppModel.self) private var model
    @Environment(\.pluginActions) private var context
    /// The pending run of each step, each holding the object captured when it opened.
    @State private var form: PluginActionRun?
    @State private var plain: PluginActionRun?
    @State private var typed: PluginActionRun?
    @State private var running = false

    private var runnable: Bool { action.usable && target != nil && context != nil }

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack(spacing: 8) {
                Button {
                    start()
                } label: {
                    Text(verbatim: action.label)
                }
                .buttonStyle(.bordered)
                .disabled(!runnable || running)
                if running { ProgressView() }
            }
            if let note { Text(note).font(.caption).foregroundStyle(.secondary) }
        }
        .modifier(
            PluginActionConfirmation(action: action, plain: $plain, typed: $typed) { run in
                Task { await execute(run) }
            }
        )
        .sheet(item: $form) { request in
            PluginActionFormSheet(action: action, request: request) { run in
                form = nil
                Task { await execute(run) }
            }
        }
        // A reload that shows another object in this row, or turns the button off, closes
        // what was open for the old one.
        .onChange(of: target) { dropStale() }
        .onChange(of: action.usable) { dropStale() }
    }

    private func dropStale() {
        if let form, !form.applies(to: target, usable: action.usable) { self.form = nil }
        if let plain, !plain.applies(to: target, usable: action.usable) { self.plain = nil }
        if let typed, !typed.applies(to: target, usable: action.usable) { self.typed = nil }
    }

    private var note: String? {
        switch action.note {
        case .reason(let reason): reason
        case .notAllowed: String(localized: "Your credentials may not do this.")
        case .unavailable: String(localized: "Not available for this object now.")
        case nil: nil
        }
    }

    /// The form when the action has params, else the confirmation it asks for.
    /// The object is captured here: every later step runs on it, never on what the row shows then.
    private func start() {
        guard runnable, let target else { return }
        let run = PluginActionRun(target: target)
        if !action.params.isEmpty {
            form = run
            return
        }
        switch action.confirmation {
        case .none: Task { await execute(run) }
        case .plain: plain = run
        case .typedName: typed = run
        }
    }

    /// Runs on `run.target` only, and only while the row still shows it with the button on.
    private func execute(_ run: PluginActionRun) async {
        guard let client = model.client, let context, !running, run.applies(to: target, usable: action.usable) else {
            return
        }
        let target = run.target
        running = true
        defer { running = false }
        do {
            try await client.pluginAction(
                id: context.pluginID, action: action.id, namespace: target.namespace,
                name: target.name, paramsJSON: run.paramsJSON)
            context.finished(
                PluginActionOutcome(
                    message: String(localized: "\(action.label) sent to \(target.label)."), succeeded: true,
                    reload: true))
        } catch {
            let failure = PluginActionFailure(message: error.localizedDescription)
            context.finished(
                PluginActionOutcome(
                    message: pluginActionFailureText(failure, action: action, target: target), succeeded: false,
                    reload: failure.staleObject))
        }
    }
}

/// A refusal in the app's words.
func pluginActionFailureText(_ failure: PluginActionFailure, action: PluginActionItem, target: PluginRef) -> String {
    switch failure {
    case .disabled(let reason):
        String(localized: "\(action.label) refused on \(target.label): \(reason)")
    case .changed:
        String(localized: "\(target.label) changed since it was checked: reloaded, try again.")
    case .invalidParam(let name, let detail):
        String(localized: "\(action.label): parameter \(action.paramLabel(name)) refused: \(detail)")
    case .demo:
        String(localized: "Actions are unavailable in the demo cluster.")
    case .forbidden:
        String(localized: "\(action.label) on \(target.label): your credentials may not do this.")
    case .other(let message):
        String(localized: "\(action.label) failed on \(target.label): \(message)")
    }
}

/// Asks before a run as the plugin says: a plain question, or the object's name typed. Both say
/// and confirm the object the run captured.
private struct PluginActionConfirmation: ViewModifier {
    let action: PluginActionItem
    @Binding var plain: PluginActionRun?
    @Binding var typed: PluginActionRun?
    let run: (PluginActionRun) -> Void

    func body(content: Content) -> some View {
        content
            .confirmationDialog(
                plain.map { String(localized: "\(action.label) on \($0.target.label)?") } ?? "",
                isPresented: $plain.isPresent(),
                titleVisibility: .visible,
                presenting: plain
            ) { request in
                Button {
                    run(request)
                } label: {
                    Text(verbatim: action.label)
                }
                Button("Cancel", role: .cancel) {}
            } message: { request in
                Text(verbatim: pluginActionExplanation(action, target: request.target))
            }
            .sheet(item: $typed) { request in
                HostnameConfirmationSheet(
                    title: action.label, message: pluginActionExplanation(action, target: request.target),
                    hostname: request.target.name, actionTitle: action.label
                ) {
                    typed = nil
                    run(request)
                }
            }
    }
}

/// What the confirmation says: the action, the object, and whose rights it uses.
private func pluginActionExplanation(_ action: PluginActionItem, target: PluginRef) -> String {
    String(localized: "\(action.label) runs on \(target.label) with your credentials.")
}

/// The params of an action as a form, from the plugin's defaults. Its Run is the confirmation:
/// for typedName, once the object's name is typed in it too, as in HostnameConfirmationSheet.
struct PluginActionFormSheet: View {
    let action: PluginActionItem
    /// The run asked for, with the object captured when the form opened.
    let request: PluginActionRun
    let onRun: (PluginActionRun) -> Void

    @Environment(\.dismiss) private var dismiss
    @State private var form: PluginActionForm
    @State private var typed = ""

    // Explicit: the private @State makes the memberwise init private.
    init(action: PluginActionItem, request: PluginActionRun, onRun: @escaping (PluginActionRun) -> Void) {
        self.action = action
        self.request = request
        self.onRun = onRun
        _form = State(initialValue: PluginActionForm(params: action.params))
    }

    /// The object the form runs on: the captured one, never what the row shows later.
    private var target: PluginRef { request.target }

    private var asksName: Bool { action.confirmation == .typedName }

    /// A blank name is never confirmed, least of all by an empty field.
    private var confirmed: Bool { !asksName || typedConfirmationMatches(typed, token: target.name) }

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    Text(verbatim: pluginActionExplanation(action, target: target))
                        .foregroundStyle(asksName ? Color.statusWarn : Color.primary)
                }
                ForEach(action.params, id: \.name) { param in
                    Section {
                        field(param)
                    } header: {
                        if param.type != "bool" { Text(verbatim: param.label) }
                    } footer: {
                        if let problem = form.problem(param) {
                            Text(problemText(problem)).foregroundStyle(Color.statusWarn)
                        }
                    }
                }
                if asksName {
                    Section("Type \(target.name) to confirm") {
                        TextField("Hostname", text: $typed, prompt: Text(verbatim: target.name))
                            .font(.body.monospaced())
                            .autocorrectionDisabled()
                            .textInputAutocapitalization(.never)
                    }
                }
            }
            .navigationTitle(action.label)
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() } }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Run") { submit() }.disabled(!form.isValid || !confirmed)
                }
            }
        }
    }

    @ViewBuilder
    private func field(_ param: PluginParam) -> some View {
        switch param.type {
        case "bool":
            Toggle(isOn: flag(param.name)) { Text(verbatim: param.label) }
        case "enum":
            Picker(selection: text(param.name)) {
                if param.required { Text(verbatim: "–").tag("") }
                ForEach(param.values, id: \.self) { value in
                    Text(verbatim: value).tag(value)
                }
            } label: {
                Text(verbatim: param.label)
            }
        case "int":
            TextField(text: text(param.name), prompt: param.defaultValue.map { Text(verbatim: $0.text) }) {
                Text(verbatim: param.label)
            }
            .keyboardType(.numbersAndPunctuation)
            .font(.body.monospaced())
        case "string":
            TextField(text: text(param.name), prompt: param.defaultValue.map { Text(verbatim: $0.text) }) {
                Text(verbatim: param.label)
            }
            .autocorrectionDisabled()
            .textInputAutocapitalization(.never)
        default:
            EmptyView()
        }
    }

    private func text(_ name: String) -> Binding<String> {
        Binding(get: { form.text(name) }, set: { form = form.withText($0, for: name) })
    }

    private func flag(_ name: String) -> Binding<Bool> {
        Binding(get: { form.flag(name) }, set: { form = form.withFlag($0, for: name) })
    }

    private func problemText(_ problem: PluginParamProblem) -> String {
        switch problem {
        case .required: String(localized: "Required")
        case .notAnInteger: String(localized: "Enter a whole number.")
        case .notAChoice: String(localized: "Choose one of the values.")
        case .tooLong: String(localized: "At most 1,024 bytes.")
        case .unsupported: String(localized: "This app cannot fill this value.")
        }
    }

    /// Run is the confirmation: no second question after it.
    private func submit() {
        guard confirmed, let json = form.paramsJSON else { return }
        onRun(request.with(paramsJSON: json))
    }
}
