import SwiftUI
import IchorCore

/// "Can I?" on screen: a screen loads which Kubernetes actions its namespace allows
/// (`loadsKubeActionAccess`), and each action button reads it from the environment. A definite
/// refusal disables the button and says why in one line; an unknown answer, a load still running
/// or one that failed leaves the button as it was: the API server still decides.

private struct KubeActionAccessKey: EnvironmentKey {
    static let defaultValue: KubeActionAccess? = nil
}

extension EnvironmentValues {
    /// The access of the namespace on screen, nil until loaded (or when the load failed).
    var kubeActionAccess: KubeActionAccess? {
        get { self[KubeActionAccessKey.self] }
        set { self[KubeActionAccessKey.self] = newValue }
    }
}

extension KubeAccess {
    /// "Your account cannot patch deployments.apps/scale in shop".
    var deniedText: String {
        let resource = deniedResource
        return isClusterWide
            ? String(localized: "Your account cannot \(verb) \(resource) across the cluster")
            : String(localized: "Your account cannot \(verb) \(resource) in \(namespace)")
    }
}

extension View {
    /// Loads the access of `namespace` ("" for cluster-wide: the node actions, or every
    /// namespace listed) for the active cluster and puts it in the environment of this view;
    /// nil asks nothing (no action on screen).
    func loadsKubeActionAccess(namespace: String?) -> some View {
        modifier(KubeActionAccessLoader(namespace: namespace))
    }

    /// Disables this view when the access in the environment refuses `action` on an object
    /// of `namespace` (nil: the namespace loaded). Pair it with a `KubeDeniedNote` or use
    /// `KubeGatedMenuButton` in a menu. A nil action: never disabled.
    func kubeGated(_ action: KubeAction?, in namespace: String? = nil) -> some View {
        modifier(KubeGatedModifier(action: action, namespace: namespace))
    }
}

private struct KubeActionAccessLoader: ViewModifier {
    let namespace: String?

    @Environment(AppModel.self) private var model
    @State private var loaded: (key: String, access: KubeActionAccess)?

    /// The cluster, its generation (screenshot mode) and the namespace.
    private var key: String { "\(model.activeContext)#\(model.dataGeneration)#\(model.client?.kubeServer ?? "")#\(namespace ?? "-")" }

    func body(content: Content) -> some View {
        content
            // Never another cluster's or namespace's answer while the new one loads.
            .environment(\.kubeActionAccess, loaded?.key == key ? loaded?.access : nil)
            .task(id: key) {
                guard let namespace, let client = model.client else { return }
                let asked = key
                // A failure keeps the actions offered: the API server still decides.
                guard let access = try? await client.kubeActionAccess(namespace: namespace), !Task.isCancelled else { return }
                loaded = (asked, access)
            }
    }
}

/// The refusal of `action` (nil: none asked) in `access`, nil until loaded.
private func kubeDenial(_ access: KubeActionAccess?, _ action: KubeAction?, in namespace: String?) -> KubeAccess? {
    guard let access, let action else { return nil }
    return access.denial(for: action, in: namespace)
}

private struct KubeGatedModifier: ViewModifier {
    let action: KubeAction?
    let namespace: String?

    @Environment(\.kubeActionAccess) private var access

    func body(content: Content) -> some View {
        content.disabled(kubeDenial(access, action, in: namespace) != nil)
    }
}

/// The one-line reason `action` is refused, as a caption; nothing when it is not.
struct KubeDeniedNote: View {
    let action: KubeAction?
    /// The object's namespace; nil for the namespace loaded.
    var namespace: String?

    @Environment(\.kubeActionAccess) private var access

    init(_ action: KubeAction?, in namespace: String? = nil) {
        self.action = action
        self.namespace = namespace
    }

    var body: some View {
        if let denial = kubeDenial(access, action, in: namespace) {
            KubeDeniedLine(text: denial.deniedText)
        }
    }
}

/// A List section holding the reasons `actions` are refused, one line each; nothing when none
/// is: for a list whose rows each carry the same refused buttons.
struct KubeDeniedSection: View {
    let actions: [KubeAction]
    /// The namespace of the rows; nil for the namespace loaded.
    var namespace: String?

    @Environment(\.kubeActionAccess) private var access

    var body: some View {
        let reasons = deniedReasons
        if !reasons.isEmpty {
            Section {
                ForEach(reasons, id: \.self) { KubeDeniedLine(text: $0) }
            }
        }
    }

    /// Each refusal's line once: two actions refused for the same permission share it. Rows of
    /// every namespace are not disabled by a namespaced refusal everywhere: nor is it said.
    private var deniedReasons: [String] {
        let everyNamespace = (namespace ?? access?.namespace ?? "").isEmpty
        var seen = Set<String>()
        return actions.filter { !(everyNamespace && $0.isNamespaced) }
            .compactMap { kubeDenial(access, $0, in: namespace)?.deniedText }
            .filter { seen.insert($0).inserted }
    }
}

/// A refusal's reason: a lock and the line, as a caption.
private struct KubeDeniedLine: View {
    let text: String

    var body: some View {
        Label {
            Text(verbatim: text)
        } icon: {
            Image(systemName: "lock")
        }
        .font(.caption)
        .foregroundStyle(.secondary)
    }
}

/// A menu item for `action`: when refused, disabled with the reason as its second line.
struct KubeGatedMenuButton: View {
    let title: String
    let systemImage: String
    let action: KubeAction
    var namespace: String?
    var role: ButtonRole?
    let perform: () -> Void

    @Environment(\.kubeActionAccess) private var access

    init(_ title: String, systemImage: String, action: KubeAction, in namespace: String? = nil,
         role: ButtonRole? = nil, perform: @escaping () -> Void) {
        self.title = title
        self.systemImage = systemImage
        self.action = action
        self.namespace = namespace
        self.role = role
        self.perform = perform
    }

    var body: some View {
        if let denial = kubeDenial(access, action, in: namespace) {
            Button(role: role, action: perform) {
                Text(verbatim: title)
                Text(verbatim: denial.deniedText)
            }
            .disabled(true)
        } else {
            Button(role: role, action: perform) {
                Label(title, systemImage: systemImage)
            }
        }
    }
}
