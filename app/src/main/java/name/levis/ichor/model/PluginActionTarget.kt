package name.levis.ichor.model

// Which object a plugin action acts on. A tab that reloads under an open dialog can reorder
// or replace its rows: the dialog, and the run it confirms, stay bound to the object they
// were opened on, never to whatever row now sits at the same place.

/** An action dialog opened on [ref] (namespace and name included): it runs on that object only. */
data class PluginActionPending(val ref: PluginRef, val actionId: String)

/**
 * The action [pending] still applies to on the row showing [ref]: null when the row now shows
 * another object (another name, or the same name in another namespace), when the action is
 * gone, or when it may not run any more (see [PluginAction.usable]).
 */
fun pendingAction(pending: PluginActionPending?, ref: PluginRef?, items: List<PluginAction>): PluginAction? {
    if (pending == null || ref == null || ref.key != pending.ref.key) return null
    return items.firstOrNull { it.id == pending.actionId && it.usable }
}

/** The label of param [name] as the form shows it, the raw name when it has none (or is unknown). */
fun PluginAction.paramLabel(name: String): String =
    params.firstOrNull { it.name == name }?.label?.takeIf { it.isNotEmpty() } ?: name

/** "namespace/name", or the name alone for a cluster-scoped object. */
val PluginRef.displayName: String get() = listOf(namespace, name).filter { it.isNotEmpty() }.joinToString("/")

/**
 * A list key per row: the row's object (so that its state, an open dialog included, follows
 * it across a reload), its position for a row without one, or whose object another row of
 * the list shows too (keys must be unique).
 */
fun pluginRowKeys(rows: List<PluginRow>): List<String> {
    val counts = rows.mapNotNull { it.ref?.key }.groupingBy { it }.eachCount()
    return rows.mapIndexed { i, row ->
        val key = row.ref?.key
        if (key != null && counts[key] == 1) "o:$key" else "i:$i"
    }
}
