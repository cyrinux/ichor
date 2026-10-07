package name.levis.ichor.ui.debug

import android.content.Context
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import name.levis.ichor.data.ConfigRepository
import name.levis.ichor.data.KubeServers

/** A running shell as its notification shows it, and what opens it again. */
data class LiveShell(val key: ShellKey, val hostname: String)

/** The keys of [keys] whose cluster is no longer among [contexts]. */
internal fun orphanedShells(keys: Collection<ShellKey>, contexts: Set<String>): List<ShellKey> =
    keys.filter { it.context !in contexts }

/**
 * The debug shells, kept app-wide so leaving the screen does not end them. While one runs,
 * [DebugShellService] keeps the app alive and shows a notification per shell, to open or
 * exit it. A shell that is not running is dropped once no screen shows it: when its screen goes
 * ([release]), or when it exits while away.
 */
class DebugShells(private val context: Context, private val configs: ConfigRepository, private val kubeServers: KubeServers) {
    private var shells: Map<ShellKey, DebugShell> = emptyMap() // guarded by this, in opening order
    private var onScreen: Set<ShellKey> = emptySet() // guarded by this

    private val _live = MutableStateFlow<List<LiveShell>>(emptyList())
    val live: StateFlow<List<LiveShell>> = _live.asStateFlow()

    /** The node's (or pod's) shell: the one still open, or a new one on its setup form. */
    @Synchronized
    fun open(key: ShellKey, hostname: String): DebugShell {
        onScreen = onScreen + key
        return shells[key] ?: DebugShell(key, hostname, configs, kubeServers, onLiveChange = ::refresh).also { shells = shells + (key to it) }
    }

    /** Its screen went: a shell that is not running (setup form, exited) is not kept. */
    @Synchronized
    fun release(key: ShellKey) {
        onScreen = onScreen - key
        dropIdle()
    }

    /** Ends the shell (the notification's Exit) and forgets it. */
    fun close(key: ShellKey) = closeAll(listOf(key))

    /** Ends the shells of clusters removed from the config (all of them once none is left). */
    fun retainContexts(contexts: Set<String>) = closeAll(synchronized(this) { orphanedShells(shells.keys, contexts) })

    private fun closeAll(keys: List<ShellKey>) {
        if (keys.isEmpty()) return
        val closed = synchronized(this) {
            val found = keys.mapNotNull { shells[it] }
            shells = shells - keys.toSet()
            found
        }
        closed.forEach { it.stop() }
        refresh()
    }

    /** Publishes the running shells; the first one starts the service, which stops after the last. */
    private fun refresh() {
        val (wasEmpty, now) = synchronized(this) {
            dropIdle()
            val now = shells.values.filter { it.state.value.isLive }.map { LiveShell(it.key, it.hostname) }
            val wasEmpty = _live.value.isEmpty()
            _live.value = now
            wasEmpty to now
        }
        if (wasEmpty && now.isNotEmpty()) DebugShellService.start(context)
    }

    // Called holding the lock.
    private fun dropIdle() {
        shells = shells.filter { (key, shell) -> key in onScreen || shell.state.value.isLive }
    }
}
