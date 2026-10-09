package name.levis.ichor.data

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import kotlinx.serialization.builtins.ListSerializer
import name.levis.ichor.model.ActivityEntry
import name.levis.ichorgo.Ichorgo

/**
 * The action audit log the core keeps on this device (go/ichorgo/audit.go): every change the
 * app made to a cluster. No cluster call: it reads the encrypted file of the data directory.
 * [cluster]: a context name, or "" for every cluster.
 */
object ActivityLog {
    /** The entries of [cluster], newest first. */
    suspend fun entries(cluster: String): List<ActivityEntry> = withContext(Dispatchers.IO) {
        TalosJson.decodeFromString(ListSerializer(ActivityEntry.serializer()), Ichorgo.auditLog(cluster, ""))
    }

    /** The log of [cluster] as a Markdown table, to share. */
    suspend fun export(cluster: String): String = withContext(Dispatchers.IO) { Ichorgo.auditExport(cluster, "markdown") }

    /** Forgets the entries of [cluster]. */
    suspend fun clear(cluster: String) = withContext(Dispatchers.IO) { Ichorgo.auditClear(cluster) }
}
