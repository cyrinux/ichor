import Foundation
import IchorCore
import Ichorgo

/// The CRD plugins (docs/plugins.md): which ones the cluster runs, and each one's render tree,
/// resolved and translated by the Go core. In the demo context Go renders the plugins' samples.
/// A plugin's buttons run its declared actions (KubePluginAction).
extension TalosClient {
    /// Every plugin with whether the cluster runs it and, when it does, its summary tile.
    /// hints: catalog ids seen in the inventory (see dataServiceHints), "" for none.
    func pluginDetect(hints: String) async throws -> PluginDetect {
        try await Self.json { [config = self.kubeConfig, context = self.kubeContext, kubeServer = self.kubeAPIServer] in
            IchorgoKubePluginDetect(config, context, kubeServer, hints, $0)
        }
    }

    /// Plugin `id` read over `scope` (a namespace, "" for all), its labels in `locale` ("" for English).
    func pluginData(id: String, scope: String = "", locale: String = pluginLocale()) async throws -> PluginTree {
        try await Self.json { [config = self.kubeConfig, context = self.kubeContext, kubeServer = self.kubeAPIServer] in
            IchorgoKubePluginData(config, context, kubeServer, id, scope, locale, $0)
        }
    }

    /// Runs action `action` of plugin `id` on `namespace`/`name` (namespace "" for a
    /// cluster-scoped object) with `paramsJSON`, a JSON object of its params ("" for none). Go
    /// checks the object again and refuses (PluginActionFailure reads why). Never log the params.
    func pluginAction(id: String, action: String, namespace: String, name: String, paramsJSON: String) async throws {
        try await Self.run { [config = self.kubeConfig, context = self.kubeContext, kubeServer = self.kubeAPIServer] error -> Void in
            _ = IchorgoKubePluginAction(config, context, kubeServer, id, action, namespace, name, paramsJSON, error)
        }
    }

    /// The built-in plugins and the resources each one reads (no cluster call).
    func pluginCatalog() async throws -> PluginCatalog {
        try await Self.json { IchorgoPluginCatalog($0) }
    }
}

/// The language Go translates a plugin's labels into: the one the app runs in ("fr"), so the
/// tree matches the app's chrome; Go reads "en" or a language it lacks as English.
func pluginLocale() -> String {
    Bundle.main.preferredLocalizations.first ?? ""
}
