package name.levis.ichor.ui.debug

import name.levis.ichor.ui.asString
import androidx.compose.ui.res.stringResource
import name.levis.ichor.R
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
import androidx.compose.material3.Button
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilledTonalButton
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
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import kotlinx.coroutines.launch
import name.levis.ichor.TalosApp
import name.levis.ichor.security.AuthResult
import name.levis.ichor.security.authenticate
import name.levis.ichor.security.findFragmentActivity
import name.levis.ichor.ui.app
import name.levis.ichor.ui.components.BackButton
import name.levis.ichor.ui.factory
import name.levis.ichor.ui.theme.LocalStatusColors
import org.connectbot.terminal.Terminal

private const val DEFAULT_IMAGE = "nicolaka/netshoot:latest"
private const val DEFAULT_ARGS = "/bin/sh"

/** `talosctl debug -n NODE IMAGE --args ARGS`: a privileged container with a terminal. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun DebugShellScreen(
    node: String,
    hostname: String,
    context: String,
    onBack: () -> Unit,
    vm: DebugShellViewModel = viewModel(
        key = "debug-$context-$node",
        factory = factory { DebugShellViewModel(app.debugShells, ShellKey(context, node), hostname) },
    ),
) {
    val state by vm.state.collectAsStateWithLifecycle()
    var showSnippets by rememberSaveable { mutableStateOf(false) }

    if (showSnippets && state is ShellState.Running) {
        DebugSnippetsSheet(
            snippets = vm.snippets,
            onPick = {
                vm.send(it.bytes())
                showSnippets = false
            },
            onDismiss = { showSnippets = false },
        )
    }

    Scaffold(
        topBar = {
            TopAppBar(
                title = {
                    Column {
                        Text(stringResource(R.string.debug_title))
                        Text(hostname, style = MaterialTheme.typography.labelMedium)
                    }
                },
                // Back leaves the shell running: its notification opens it again.
                navigationIcon = { BackButton(onClick = onBack) },
                actions = {
                    if (state is ShellState.Running || state is ShellState.Starting) {
                        TextButton(onClick = vm::stop) { Text(stringResource(R.string.debug_stop)) }
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
                        foregroundColor = TerminalForeground,
                        keyboardEnabled = s is ShellState.Running,
                    )
                    if (s is ShellState.Running) {
                        ExtraKeys(
                            onKey = vm::send,
                            onSnippets = if (vm.snippets.isEmpty()) null else ({ showSnippets = true }),
                        )
                    }
                }
            }
        }
    }
}

@Composable
private fun SetupForm(hostname: String, onStart: (String, String) -> Unit) {
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    val prefs = remember { context.getSharedPreferences("ichor-debug", Context.MODE_PRIVATE) }
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
            when (val auth = authenticate(activity, context.getString(R.string.debug_auth, hostname))) {
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
            stringResource(R.string.debug_intro, hostname),
            style = MaterialTheme.typography.bodyMedium,
        )
        OutlinedTextField(
            value = image,
            onValueChange = { image = it },
            label = { Text(stringResource(R.string.debug_image)) },
            singleLine = true,
            textStyle = MaterialTheme.typography.bodyMedium.copy(fontFamily = FontFamily.Monospace),
            modifier = Modifier.fillMaxWidth(),
        )
        OutlinedTextField(
            value = args,
            onValueChange = { args = it },
            label = { Text(stringResource(R.string.debug_command)) },
            singleLine = true,
            textStyle = MaterialTheme.typography.bodyMedium.copy(fontFamily = FontFamily.Monospace),
            modifier = Modifier.fillMaxWidth(),
        )
        error?.let { Text(it, color = LocalStatusColors.current.bad) }
        Button(onClick = ::start, enabled = image.isNotBlank(), modifier = Modifier.fillMaxWidth()) { Text(stringResource(R.string.debug_start)) }
    }
}

@Composable
private fun StatusLine(state: ShellState, onRestart: () -> Unit) {
    val text = when (state) {
        is ShellState.Starting -> state.status.asString()
        is ShellState.Exited -> if (state.code >= 0) stringResource(R.string.debug_exited, state.code.toInt()) else state.message
        else -> null
    } ?: return
    Row(
        Modifier.fillMaxWidth().background(MaterialTheme.colorScheme.surfaceContainer).padding(horizontal = 12.dp, vertical = 6.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(text, style = MaterialTheme.typography.bodySmall, modifier = Modifier.weight(1f))
        if (state is ShellState.Exited) TextButton(onClick = onRestart) { Text(stringResource(R.string.debug_new_shell)) }
    }
}

/** Keys phone keyboards lack, sent as the bytes a terminal would produce. */
@Composable
private fun ExtraKeys(onKey: (ByteArray) -> Unit, onSnippets: (() -> Unit)?) {
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
        onSnippets?.let { Button(onClick = it) { Text(stringResource(R.string.debug_snippets)) } }
        keys.forEach { (label, bytes) ->
            FilledTonalButton(onClick = { onKey(bytes.toByteArray()) }) {
                Text(label, fontFamily = FontFamily.Monospace)
            }
        }
    }
}
