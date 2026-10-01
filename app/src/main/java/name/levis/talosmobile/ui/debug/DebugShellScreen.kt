package name.levis.talosmobile.ui.debug

import android.content.Context
import androidx.compose.foundation.background
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material3.Button
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilledTonalButton
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import kotlinx.coroutines.launch
import name.levis.talosmobile.TalosApp
import name.levis.talosmobile.security.AuthResult
import name.levis.talosmobile.security.authenticate
import name.levis.talosmobile.security.findFragmentActivity
import name.levis.talosmobile.ui.app
import name.levis.talosmobile.ui.factory
import name.levis.talosmobile.ui.theme.LocalStatusColors
import org.connectbot.terminal.Terminal

private const val DEFAULT_IMAGE = "nicolaka/netshoot:latest"
private const val DEFAULT_ARGS = "/bin/sh"
private val TerminalBackground = Color(0xFF0B1220)

/** `talosctl debug -n NODE IMAGE --args ARGS`: a privileged container with a terminal. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun DebugShellScreen(
    node: String,
    hostname: String,
    onBack: () -> Unit,
    vm: DebugShellViewModel = viewModel(key = "debug-$node", factory = factory { DebugShellViewModel(app.configRepository, node) }),
) {
    val state by vm.state.collectAsStateWithLifecycle()

    Scaffold(
        topBar = {
            TopAppBar(
                title = {
                    Column {
                        Text("Debug shell")
                        Text(hostname, style = MaterialTheme.typography.labelMedium)
                    }
                },
                navigationIcon = {
                    IconButton(onClick = {
                        vm.stop()
                        onBack()
                    }) { Icon(Icons.AutoMirrored.Filled.ArrowBack, "Back") }
                },
                actions = {
                    if (state is ShellState.Running || state is ShellState.Starting) {
                        TextButton(onClick = vm::stop) { Text("Stop") }
                    }
                },
            )
        },
    ) { padding ->
        Box(Modifier.padding(padding).fillMaxSize().imePadding()) {
            when (val s = state) {
                ShellState.Setup -> SetupForm(hostname, onStart = vm::start)
                else -> Column(Modifier.fillMaxSize().background(TerminalBackground)) {
                    StatusLine(s, onRestart = vm::reset)
                    Terminal(
                        terminalEmulator = vm.emulator,
                        modifier = Modifier.weight(1f).fillMaxWidth(),
                        backgroundColor = TerminalBackground,
                        foregroundColor = Color(0xFFE4EAF1),
                        keyboardEnabled = s is ShellState.Running,
                    )
                    if (s is ShellState.Running) ExtraKeys(onKey = vm::send)
                }
            }
        }
    }
}

@Composable
private fun SetupForm(hostname: String, onStart: (String, String) -> Unit) {
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    val prefs = remember { context.getSharedPreferences("talosdev-mobile-debug", Context.MODE_PRIVATE) }
    var image by rememberSaveable { mutableStateOf(prefs.getString("image", DEFAULT_IMAGE) ?: DEFAULT_IMAGE) }
    var args by rememberSaveable { mutableStateOf(prefs.getString("args", DEFAULT_ARGS) ?: DEFAULT_ARGS) }
    var error by remember { mutableStateOf<String?>(null) }
    val appLock = (context.applicationContext as TalosApp).appLock

    fun start() {
        prefs.edit().putString("image", image.trim()).putString("args", args.trim()).apply()
        val activity = context.findFragmentActivity()
        if (!appLock.enabled.value || activity == null) {
            onStart(image, args)
            return
        }
        // With the app lock on, a privileged shell needs a fresh fingerprint/PIN, like reboot.
        scope.launch {
            when (val auth = authenticate(activity, "Debug shell on $hostname")) {
                AuthResult.Success -> onStart(image, args)
                is AuthResult.Failure -> error = auth.message
            }
        }
    }

    Column(
        Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Text(
            "Runs the image as a privileged container on $hostname, with host access, and opens a " +
                "terminal in it. Talos pulls the image first if the node does not have it.",
            style = MaterialTheme.typography.bodyMedium,
        )
        OutlinedTextField(
            value = image,
            onValueChange = { image = it },
            label = { Text("Image") },
            singleLine = true,
            textStyle = MaterialTheme.typography.bodyMedium.copy(fontFamily = FontFamily.Monospace),
            modifier = Modifier.fillMaxWidth(),
        )
        OutlinedTextField(
            value = args,
            onValueChange = { args = it },
            label = { Text("Command") },
            singleLine = true,
            textStyle = MaterialTheme.typography.bodyMedium.copy(fontFamily = FontFamily.Monospace),
            modifier = Modifier.fillMaxWidth(),
        )
        error?.let { Text(it, color = LocalStatusColors.current.bad) }
        Button(onClick = ::start, enabled = image.isNotBlank(), modifier = Modifier.fillMaxWidth()) { Text("Start shell") }
    }
}

@Composable
private fun StatusLine(state: ShellState, onRestart: () -> Unit) {
    val text = when (state) {
        is ShellState.Starting -> state.status
        is ShellState.Exited -> if (state.code >= 0) "Exited with code ${state.code}" else state.message
        else -> null
    } ?: return
    Row(
        Modifier.fillMaxWidth().background(MaterialTheme.colorScheme.surfaceContainer).padding(horizontal = 12.dp, vertical = 6.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(text, style = MaterialTheme.typography.bodySmall, modifier = Modifier.weight(1f))
        if (state is ShellState.Exited) TextButton(onClick = onRestart) { Text("New shell") }
    }
}

/** Keys phone keyboards lack, sent as the bytes a terminal would produce. */
@Composable
private fun ExtraKeys(onKey: (ByteArray) -> Unit) {
    val keys = listOf(
        "Esc" to "\u001b", "Tab" to "\t", "Ctrl-C" to "\u0003", "Ctrl-D" to "\u0004",
        "←" to "\u001b[D", "↓" to "\u001b[B", "↑" to "\u001b[A", "→" to "\u001b[C",
        "|" to "|", "/" to "/", "-" to "-",
    )
    Row(
        Modifier.fillMaxWidth().background(MaterialTheme.colorScheme.surfaceContainer)
            .horizontalScroll(rememberScrollState()).padding(horizontal = 6.dp, vertical = 4.dp),
        horizontalArrangement = Arrangement.spacedBy(6.dp),
    ) {
        keys.forEach { (label, bytes) ->
            FilledTonalButton(onClick = { onKey(bytes.toByteArray()) }) {
                Text(label, fontFamily = FontFamily.Monospace)
            }
        }
    }
}
