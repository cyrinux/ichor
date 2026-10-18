package name.levis.ichor.data

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import name.levis.ichor.model.PluginCatalog
import name.levis.ichor.model.PluginDetect
import name.levis.ichor.model.PluginTree
import name.levis.ichor.model.PluginUserItem
import name.levis.ichor.model.PluginUserList
import name.levis.ichorgo.Ichorgo

/** What KubePluginDetect answered, per hints. */
const val PLUGINS_DETECT = "plugins"

/** The installed plugin catalog: the same for every cluster, until a user plugin is turned on or off. */
const val PLUGIN_CATALOG = "plugincatalog"

/** One plugin's render tree, per namespace scope and locale. */
fun pluginDataKey(pluginId: String, scope: String, locale: String) = "plugin|$pluginId|$scope|$locale"

/**
 * The CRD plugins (docs/plugins.md): which ones the cluster runs, and each one's render tree,
 * resolved and translated by the Go core. In the demo context Go renders the plugins' samples.
 * Actions (KubePluginAction) come with the actions step: there is no call for them yet.
 */
class PluginRepository(go: GoCall) : GoRepository(go) {
    /**
     * Every plugin with whether the cluster runs it and, when it does, its summary tile.
     * [hints]: catalog ids seen in the inventory (see [name.levis.ichor.model.dataServiceHints]), "" for none.
     */
    suspend fun detect(hints: String): PluginDetect = go.remember("$PLUGINS_DETECT|$hints") {
        go.kube { cfg, ctx, server -> TalosJson.decodeFromString(PluginDetect.serializer(), Ichorgo.kubePluginDetect(cfg, ctx, server, hints)) }
    }

    /** Plugin [pluginId] read over [scope] (a namespace, "" for all), its labels in [locale] ("" for English). */
    suspend fun data(pluginId: String, scope: String, locale: String): PluginTree = go.remember(pluginDataKey(pluginId, scope, locale)) {
        go.kube { cfg, ctx, server ->
            TalosJson.decodeFromString(PluginTree.serializer(), Ichorgo.kubePluginData(cfg, ctx, server, pluginId, scope, locale))
        }
    }

    /** The installed plugins (built-ins, then enabled imported ones) and what each one reads (no cluster call). */
    suspend fun catalog(): PluginCatalog = go.remember(PLUGIN_CATALOG) { last ->
        last()?.value ?: go.kube { _, _, _ -> TalosJson.decodeFromString(PluginCatalog.serializer(), Ichorgo.pluginCatalog()) }
    }

    /** The plugins the user imported, kept on this phone by the core (no cluster call). */
    suspend fun userPlugins(): PluginUserList = withContext(Dispatchers.IO) {
        TalosJson.decodeFromString(PluginUserList.serializer(), Ichorgo.pluginUserList())
    }

    /**
     * Validates [yaml] without storing it: the answer is what the consent sheet shows. A file
     * that does not validate throws its `plugin.yaml:<line>: …` problems.
     */
    suspend fun check(yaml: String): PluginUserItem = withContext(Dispatchers.IO) {
        TalosJson.decodeFromString(PluginUserItem.serializer(), Ichorgo.pluginUserCheck(yaml))
    }

    /** Stores [yaml], on: the consent sheet's Enable, after [check]. The same id again replaces it. */
    suspend fun import(yaml: String): PluginUserItem = withContext(Dispatchers.IO) {
        TalosJson.decodeFromString(PluginUserItem.serializer(), Ichorgo.pluginUserImport(yaml))
    }.also { forgetPlugins() }

    suspend fun setEnabled(id: String, enabled: Boolean) {
        withContext(Dispatchers.IO) { Ichorgo.pluginUserSetEnabled(id, enabled) }
        forgetPlugins()
    }

    suspend fun remove(id: String) {
        withContext(Dispatchers.IO) { Ichorgo.pluginUserRemove(id) }
        forgetPlugins()
    }

    /** A plugin file at an https URL, at most 1 MiB (the core refuses http and more). */
    suspend fun download(url: String): String = withContext(Dispatchers.IO) { Ichorgo.pluginFetchURL(url) }

    /**
     * The hub, the home tile and the catalog read the installed plugins again, for every
     * cluster: the keys [PLUGINS_DETECT], [PLUGIN_CATALOG] and [pluginDataKey] all start with "plugin".
     */
    private fun forgetPlugins() = go.results.forgetContaining("plugin")
}
