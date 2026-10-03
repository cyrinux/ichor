package name.levis.ichor.ui.debug

import name.levis.ichor.ui.UiText
import name.levis.ichor.R
import androidx.compose.ui.graphics.Color
import androidx.lifecycle.ViewModel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import name.levis.talosmobile.DebugListener
import name.levis.talosmobile.DebugSession
import name.levis.talosmobile.Talosmobile
import name.levis.ichor.data.ConfigRepository
import org.connectbot.terminal.TerminalEmulator
import org.connectbot.terminal.TerminalEmulatorFactory

sealed interface ShellState {
    data object Setup : ShellState
    data class Starting(val status: UiText) : ShellState
    data object Running : ShellState
    data class Exited(val code: Long, val message: String) : ShellState
}

/**
 * Bridges a Go debug session (`talosctl debug`) and a libvterm terminal: container output
 * is fed to the emulator, keystrokes and resizes go back to the container's TTY.
 */
class DebugShellViewModel(private val configs: ConfigRepository, private val node: String) : ViewModel() {
    private val _state = MutableStateFlow<ShellState>(ShellState.Setup)
    val state: StateFlow<ShellState> = _state.asStateFlow()

    private var session: DebugSession? = null
    private var size = 80 to 24 // columns to rows, updated by the terminal view

    val emulator: TerminalEmulator = TerminalEmulatorFactory.create(
        initialRows = size.second,
        initialCols = size.first,
        defaultForeground = Color(0xFFE4EAF1),
        defaultBackground = Color(0xFF0B1220),
        onKeyboardInput = { bytes -> session?.write(bytes) },
        onResize = { dims ->
            size = dims.columns to dims.rows
            session?.resize(dims.columns.toLong(), dims.rows.toLong())
        },
    )

    fun start(image: String, args: String) {
        val stored = configs.config.value ?: return
        stop()
        emulator.clearScreen()
        _state.value = ShellState.Starting(UiText.Res(R.string.debug_connecting))
        session = Talosmobile.startDebugShell(
            stored.yaml, stored.activeContext, node, image, args,
            size.first.toLong(), size.second.toLong(),
            object : DebugListener {
                override fun onStatus(message: String) {
                    _state.value = ShellState.Starting(UiText.Raw(message)) // from the Go core
                }

                override fun onOutput(data: ByteArray) {
                    if (_state.value !is ShellState.Running) _state.value = ShellState.Running
                    emulator.writeInput(data) // thread-safe in termlib
                }

                override fun onExit(code: Long, errMessage: String) {
                    _state.value = ShellState.Exited(code, errMessage)
                }
            },
        )
    }

    /** The ready-made commands; none if the core cannot list them (the button then hides). */
    val snippets: List<DebugSnippet> by lazy {
        runCatching { decodeDebugSnippets(Talosmobile.debugSnippets()) }.getOrDefault(emptyList())
    }

    /** Sends raw bytes (extra keys: Esc, Tab, arrows, Ctrl-C…). */
    fun send(bytes: ByteArray) {
        session?.write(bytes)
    }

    /** Back to the setup form for a new shell. */
    fun reset() {
        stop()
        _state.value = ShellState.Setup
    }

    fun stop() {
        session?.close()
        session = null
    }

    override fun onCleared() {
        stop()
    }
}
