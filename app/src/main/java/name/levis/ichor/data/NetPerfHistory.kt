package name.levis.ichor.data

import kotlinx.serialization.builtins.ListSerializer
import name.levis.ichor.model.NetPerfReport
import java.io.File

/**
 * Finished network tests, newest first, encrypted on the phone and not backed up. One list per
 * scope: the cluster and the privacy mask, since the core masks node names in reports.
 */
class NetPerfHistory(private val directory: File) {
    private fun store(scope: String) = SecureStore(File(directory.apply { mkdirs() }, scope), "ichor-netperf-$scope")

    fun read(scope: String): List<NetPerfReport> =
        store(scope).read()?.let { TalosJson.decodeFromString(SERIALIZER, it.toString(Charsets.UTF_8)) }.orEmpty()

    fun save(scope: String, reports: List<NetPerfReport>) {
        if (reports.isEmpty()) store(scope).clear()
        else store(scope).write(TalosJson.encodeToString(SERIALIZER, reports).toByteArray())
    }

    private companion object {
        val SERIALIZER = ListSerializer(NetPerfReport.serializer())
    }
}
