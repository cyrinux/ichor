package name.levis.ichor.ui.debug

import androidx.compose.ui.graphics.Color
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import name.levis.ichor.R
import name.levis.ichor.data.ConfigRepository
import name.levis.ichor.data.KubeServers
import name.levis.ichor.ui.UiText
import name.levis.ichorgo.DebugListener
import name.levis.ichorgo.DebugSession
import name.levis.ichorgo.Ichorgo
import org.connectbot.terminal.TerminalEmulator
import org.connectbot.terminal.TerminalEmulatorFactory

sealed interface ShellState {
    data object Setup : ShellState
    data class Starting(val status: UiText) : ShellState
    data object Running : ShellState
    data class Exited(val code: Long, val message: String) : ShellState
}

/** Whether the container still runs: the shell (and the app with it) stays up until it exits. */
val ShellState.isLive: Boolean get() = this is ShellState.Starting || this is ShellState.Running

/**
 * One shell per node of a cluster (its talosconfig context), or per container of a pod
 * ([pod] set, [node] empty, `kubectl exec -it`), or per Kubernetes node of a cluster without
 * Talos ([kubeNode], a privileged pod in [namespace]): reopening it finds it again.
 */
data class ShellKey(
    val context: String,
    val node: String,
    val namespace: String = "",
    val pod: String = "",
    val container: String = "",
    val kubeNode: Boolean = false,
) {
    val isPod: Boolean get() = pod.isNotEmpty()

    /** The id of its notification; never 0, which Android refuses for a foreground service. */
    val notificationId: Int get() = (hashCode() and Int.MAX_VALUE).coerceAtLeast(1)
}

internal val TerminalForeground = Color(0xFFE4EAF1)
internal val TerminalBackground = Color(0xFF0B1220)

/**
 * Bridges a Go debug session (`talosctl debug`, or `kubectl exec -it` for a pod's [key]) and a
 * libvterm terminal: container output
 * is fed to the emulator, keystrokes and resizes go back to the container's TTY. It outlives
 * the screen (see [DebugShells]); [onLiveChange] tells when it starts or stops running.
 */
class DebugShell internal constructor(
    val key: ShellKey,
    val hostname: String,
    private val configs: ConfigRepository,
    private val kubeServers: KubeServers,
    private val onLiveChange: () -> Unit,
) {
    private val _state = MutableStateFlow<ShellState>(ShellState.Setup)
    val state: StateFlow<ShellState> = _state.asStateFlow()

    @Volatile private var session: DebugSession? = null
    @Volatile private var size = 80 to 24 // columns to rows, updated by the terminal view

    // Callbacks of a session replaced by "New shell" must not touch the new one's state.
    @Volatile private var generation = 0

    val emulator: TerminalEmulator = TerminalEmulatorFactory.create(
        initialRows = size.second,
        initialCols = size.first,
        defaultForeground = TerminalForeground,
        defaultBackground = TerminalBackground,
        onKeyboardInput = { bytes -> session?.write(bytes) },
        onResize = { dims ->
            size = dims.columns to dims.rows
            session?.resize(dims.columns.toLong(), dims.rows.toLong())
        },
    )

    /**
     * A node: [image] is the debug pod's. A pod: an empty [image] runs [args] in its container
     * (`kubectl exec`); a non-empty one adds a debug container from it, sharing the container's
     * processes (`kubectl debug`, for images without a shell).
     */
    fun start(image: String, args: String) {
        val stored = configs.config.value ?: return
        stop()
        val current = ++generation
        emulator.clearScreen()
        setState(ShellState.Starting(UiText.Res(R.string.debug_connecting)))
        val (cols, rows) = size.first.toLong() to size.second.toLong()
        val listener = object : DebugListener {
            override fun onStatus(message: String) {
                if (current == generation) setState(ShellState.Starting(UiText.Raw(message))) // from the Go core
            }

            override fun onOutput(data: ByteArray) {
                if (current != generation) return
                if (_state.value !is ShellState.Running) setState(ShellState.Running)
                emulator.writeInput(data) // thread-safe in termlib
            }

            override fun onExit(code: Long, errMessage: String) {
                if (current == generation) setState(ShellState.Exited(code, errMessage))
            }
        }
        session = if (key.kubeNode) {
            // A root shell on the node through a privileged pod (`kubectl debug node/`).
            val target = kubeServers.targetFor(stored.copy(activeContext = key.context))
            Ichorgo.startNodeDebug(target.yaml, target.context, target.server, key.node, key.namespace, image, cols, rows, listener)
        } else if (key.isPod) {
            // The pod's cluster, which may no longer be the one on screen.
            val target = kubeServers.targetFor(stored.copy(activeContext = key.context))
            if (image.isNotBlank()) {
                Ichorgo.startPodDebug(
                    target.yaml, target.context, target.server, key.namespace, key.pod, key.container, image, cols, rows, listener,
                )
            } else {
                Ichorgo.startPodShell(
                    target.yaml, target.context, target.server, key.namespace, key.pod, key.container, args, cols, rows, listener,
                )
            }
        } else {
            Ichorgo.startDebugShell(stored.yamlFor(key.context), key.context, key.node, image, args, cols, rows, listener)
        }
    }

    /** Sends raw bytes (extra keys: Esc, Tab, arrows, Ctrl-C…). */
    fun send(bytes: ByteArray) {
        session?.write(bytes)
    }

    /** Back to the setup form for a new shell. */
    fun reset() {
        stop()
        ++generation
        setState(ShellState.Setup)
    }

    /** Ends the container; the session still reports its exit (see DebugSession.Close). */
    fun stop() {
        session?.close()
        session = null
    }

    private fun setState(next: ShellState) {
        val wasLive = _state.value.isLive
        _state.value = next
        if (wasLive != next.isLive) onLiveChange()
    }
}
