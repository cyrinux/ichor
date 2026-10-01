package name.levis.talosmobile.shortcuts

import name.levis.talosmobile.model.ClusterLabels
import name.levis.talosmobile.model.ConfigSummary
import name.levis.talosmobile.model.seedOf

/** A launcher shortcut opening the cluster [fingerprint], labelled [label], drawn in [color]. */
data class ShortcutSpec(val id: String, val fingerprint: String, val label: String, val color: Int, val rank: Int)

private const val ID_PREFIX = "cluster-"

/** The shortcut id of the cluster [fingerprint]: stable across renames, screenshot mode and re-imports. */
fun shortcutId(fingerprint: String): String = ID_PREFIX + fingerprint

/** The shortcut ids of every cluster of [summary], shown or past what the launcher shows. */
fun clusterShortcutIds(summary: ConfigSummary): Set<String> =
    summary.contexts.map { it.fingerprint }.filter { it.isNotBlank() }.map(::shortcutId).toSet()

/**
 * A shortcut per cluster of [summary], in the config's order, at most [max] (what the
 * launcher shows). Clusters without a fingerprint cannot be told apart later: no shortcut.
 */
fun clusterShortcuts(summary: ConfigSummary, labels: ClusterLabels, colors: Map<String, Int>, max: Int): List<ShortcutSpec> =
    summary.contexts
        .filter { it.fingerprint.isNotBlank() }
        .distinctBy { it.fingerprint }
        .take(max.coerceAtLeast(0))
        .mapIndexed { rank, context ->
            ShortcutSpec(shortcutId(context.fingerprint), context.fingerprint, labels.of(context), colors.seedOf(context), rank)
        }

/**
 * The letter on a shortcut's icon: the first letter or digit of the cluster part of
 * [label] ("admin@prod" gives "P"), else of the whole label.
 */
fun shortcutInitial(label: String): String {
    val cluster = label.substringAfter('@').takeIf { it.any(Char::isLetterOrDigit) } ?: label
    return cluster.firstOrNull(Char::isLetterOrDigit)?.uppercase() ?: "T"
}
