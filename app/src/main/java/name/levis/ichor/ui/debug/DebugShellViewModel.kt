package name.levis.ichor.ui.debug

import androidx.lifecycle.ViewModel
import kotlinx.coroutines.flow.StateFlow
import name.levis.ichorgo.Ichorgo
import org.connectbot.terminal.TerminalEmulator

/**
 * The screen's view of the node's [DebugShell]. The shell is app-wide: leaving the screen
 * keeps it running (its notification opens it again); only Stop or the notification's Exit end it.
 */
class DebugShellViewModel(private val shells: DebugShells, key: ShellKey, hostname: String) : ViewModel() {
    private val shell = shells.open(key, hostname)

    val state: StateFlow<ShellState> = shell.state
    val emulator: TerminalEmulator get() = shell.emulator

    fun start(image: String, args: String) = shell.start(image, args)

    /** The ready-made commands; none if the core cannot list them (the button then hides). */
    val snippets: List<DebugSnippet> by lazy {
        runCatching { decodeDebugSnippets(Ichorgo.debugSnippets()) }.getOrDefault(emptyList())
    }

    /** Sends raw bytes (extra keys: Esc, Tab, arrows, Ctrl-C…). */
    fun send(bytes: ByteArray) = shell.send(bytes)

    /** Back to the setup form for a new shell. */
    fun reset() = shell.reset()

    fun stop() = shell.stop()

    override fun onCleared() {
        shells.release(shell.key)
    }
}
