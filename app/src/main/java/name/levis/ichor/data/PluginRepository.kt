package name.levis.ichor.data

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import name.levis.ichor.model.PluginCatalog
import name.levis.ichor.model.PluginDetect
import name.levis.ichor.model.PluginFindingsReport
import name.levis.ichor.model.PluginToolResult
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
 * resolved and translated by the Go core, and their actions. In the demo context Go renders the
 * plugins' samples and refuses every action.
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

    /**
     * Every active plugin's findings in one bounded call (10 s), for the background monitor;
     * their words in [locale] ("" for English). A plugin that cannot be read is in its errors,
     * the others' findings still stand. Never cached: each check reads afresh.
     */
    suspend fun findings(hints: String, locale: String): PluginFindingsReport = go.kube { cfg, ctx, server ->
        TalosJson.decodeFromString(PluginFindingsReport.serializer(), Ichorgo.kubePluginFindings(cfg, ctx, server, hints, locale))
    }

    /** The installed plugins (built-ins, then enabled imported ones) and what each one reads (no cluster call). */
    suspend fun catalog(): PluginCatalog = go.remember(PLUGIN_CATALOG) { last ->
        last()?.value ?: go.kube { _, _, _ -> TalosJson.decodeFromString(PluginCatalog.serializer(), Ichorgo.pluginCatalog()) }
    }

    /**
     * Runs action [actionId] of plugin [pluginId] on the object [namespace]/[name] ("" for a
     * cluster-scoped one) with [paramsJson], a JSON object of its params ("" for none). Go reads
     * the object first and refuses when the action does not apply to it any more; the params
     * go to the audit log only, never to this app's logs. Nothing is cached.
     */
    suspend fun action(pluginId: String, actionId: String, namespace: String, name: String, paramsJson: String) {
        go.kube { cfg, ctx, server -> Ichorgo.kubePluginAction(cfg, ctx, server, pluginId, actionId, namespace, name, paramsJson) }
    }

    /**
     * Runs tool [toolId] of plugin [pluginId] over what it reads in [scope] (a namespace, "" for
     * all), its words in [locale] ("" for English). Never cached: a tool runs only when tapped.
     * The demo context runs the tool's canned demo over the samples.
     */
    suspend fun tool(pluginId: String, toolId: String, scope: String, locale: String): PluginToolResult = go.kube { cfg, ctx, server ->
        TalosJson.decodeFromString(PluginToolResult.serializer(), Ichorgo.kubePluginTool(cfg, ctx, server, pluginId, toolId, scope, locale))
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
