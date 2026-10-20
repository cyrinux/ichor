import Foundation
import IchorCore
import Ichorgo

/// The CRD plugins (docs/plugins.md): which ones the cluster runs, and each one's render tree,
/// resolved and translated by the Go core. In the demo context Go renders the plugins' samples.
/// A plugin's buttons run its declared actions (KubePluginAction). Imported plugins
/// (Settings → Plugins) are kept by the core on this phone; enabled, they join the built-ins
/// in every call.
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

    /// Every active plugin's findings in one bounded call (10 s), for the background monitor: a
    /// plugin that could not be read is an `errors` entry and hides none of the others. Titles and
    /// details in `locale` ("" for English). hints: see pluginDetect.
    func pluginFindings(hints: String = "", locale: String = pluginLocale()) async throws -> PluginFindingsReport {
        try await Self.json { [config = self.kubeConfig, context = self.kubeContext, kubeServer = self.kubeAPIServer] in
            IchorgoKubePluginFindings(config, context, kubeServer, hints, locale, $0)
        }
    }

    /// The installed plugins (built-ins, then enabled imported ones) and what each one reads (no cluster call).
    func pluginCatalog() async throws -> PluginCatalog {
        try await Self.installedPlugins()
    }

    static func installedPlugins() async throws -> PluginCatalog {
        try await json { IchorgoPluginCatalog($0) }
    }

    /// The plugins the user imported, kept on this phone by the core (no cluster call).
    static func userPlugins() async throws -> PluginUserList {
        try await json { IchorgoPluginUserList($0) }
    }

    /// Validates `yaml` without storing it: the answer is what the consent sheet shows. A file
    /// that does not validate throws its `plugin.yaml:<line>: …` problems.
    static func checkPlugin(yaml: String) async throws -> PluginUserItem {
        try await json { IchorgoPluginUserCheck(yaml, $0) }
    }

    /// Stores `yaml`, on: the consent sheet's Enable, after checkPlugin. The same id again replaces it.
    static func importPlugin(yaml: String) async throws -> PluginUserItem {
        let item: PluginUserItem = try await json { IchorgoPluginUserImport(yaml, $0) }
        await PluginCatalogCache.reset()
        return item
    }

    static func setPluginEnabled(id: String, enabled: Bool) async throws {
        try await run { error -> Void in _ = IchorgoPluginUserSetEnabled(id, enabled, error) }
        await PluginCatalogCache.reset()
    }

    static func removePlugin(id: String) async throws {
        try await run { error -> Void in _ = IchorgoPluginUserRemove(id, error) }
        await PluginCatalogCache.reset()
    }

    /// A plugin file at an https URL, at most 1 MiB (the core refuses http and more).
    static func downloadPlugin(url: String) async throws -> String {
        try await run { IchorgoPluginFetchURL(url, $0) }
    }
}

/// The language Go translates a plugin's labels into: the one the app runs in ("fr"), so the
/// tree matches the app's chrome; Go reads "en" or a language it lacks as English.
func pluginLocale() -> String {
    Bundle.main.preferredLocalizations.first ?? ""
}
