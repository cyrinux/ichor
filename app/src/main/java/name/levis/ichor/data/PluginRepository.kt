package name.levis.ichor.data

import name.levis.ichor.model.PluginCatalog
import name.levis.ichor.model.PluginDetect
import name.levis.ichor.model.PluginTree
import name.levis.ichorgo.Ichorgo

/** What KubePluginDetect answered, per hints. */
const val PLUGINS_DETECT = "plugins"

/** The built-in plugin catalog: the same for every cluster. */
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

    /** The built-in plugins and the resources each one reads (no cluster call). */
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
}
