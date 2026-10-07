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
import kotlinx.coroutines.CoroutineScope
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

/**
 * `talosctl debug -n NODE IMAGE --args ARGS`: a privileged container with a terminal. For a
 * pod's [key], `kubectl exec -it`: a terminal in its running container. [hostname] names the
 * node, or the pod.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun DebugShellScreen(
    key: ShellKey,
    hostname: String,
    onBack: () -> Unit,
    vm: DebugShellViewModel = viewModel(
        key = "debug-$key",
        factory = factory { DebugShellViewModel(app.debugShells, key, hostname) },
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
                        Text(stringResource(if (key.isPod) R.string.pod_shell_title else R.string.debug_title))
                        Text(
                            if (key.isPod) listOf(key.namespace, hostname, key.container).filter { it.isNotEmpty() }.joinToString(" / ") else hostname,
                            style = MaterialTheme.typography.labelMedium,
                        )
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
                ShellState.Setup -> if (key.isPod) PodSetupForm(hostname, onStart = vm::start) else SetupForm(hostname, onStart = vm::start)
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
                            // The snippets are node diagnostics, for netshoot: not for any pod's image.
                            onSnippets = if (key.isPod || vm.snippets.isEmpty()) null else ({ showSnippets = true }),
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
        // With the app lock on, a privileged shell needs a fresh fingerprint/PIN, like reboot.
        authenticated(context, appLock.enabled.value, scope, context.getString(R.string.debug_auth, hostname), onError = { error = it }) {
            onStart(image, args)
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

/** `kubectl exec -it`: the command only; empty runs bash, or sh when the image lacks it. */
@Composable
private fun PodSetupForm(pod: String, onStart: (String, String) -> Unit) {
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    val prefs = remember { context.getSharedPreferences("ichor-debug", Context.MODE_PRIVATE) }
    var command by rememberSaveable { mutableStateOf(prefs.getString("pod-command", "").orEmpty()) }
    var error by remember { mutableStateOf<String?>(null) }
    val appLock = (context.applicationContext as TalosApp).appLock

    fun start() {
        prefs.edit().putString("pod-command", command.trim()).apply()
        // A shell can read the container's secrets: with the app lock on, it asks like the node's.
        authenticated(context, appLock.enabled.value, scope, context.getString(R.string.pod_shell_on, pod), onError = { error = it }) {
            onStart("", command)
        }
    }

    Column(
        Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Text(stringResource(R.string.pod_shell_intro, pod), style = MaterialTheme.typography.bodyMedium)
        OutlinedTextField(
            value = command,
            onValueChange = { command = it },
            label = { Text(stringResource(R.string.debug_command)) },
            placeholder = { Text(stringResource(R.string.pod_shell_command_auto), fontFamily = FontFamily.Monospace) },
            singleLine = true,
            textStyle = MaterialTheme.typography.bodyMedium.copy(fontFamily = FontFamily.Monospace),
            modifier = Modifier.fillMaxWidth(),
        )
        error?.let { Text(it, color = LocalStatusColors.current.bad) }
        Button(onClick = ::start, modifier = Modifier.fillMaxWidth()) { Text(stringResource(R.string.debug_start)) }
    }
}

/** Runs [onAllowed] at once, or with the app lock on after a fresh fingerprint/PIN ([reason]). */
private fun authenticated(
    context: Context,
    locked: Boolean,
    scope: CoroutineScope,
    reason: String,
    onError: (String) -> Unit,
    onAllowed: () -> Unit,
) {
    val activity = context.findFragmentActivity()
    if (!locked || activity == null) {
        onAllowed()
        return
    }
    scope.launch {
        when (val auth = authenticate(activity, reason)) {
            AuthResult.Success -> onAllowed()
            is AuthResult.Failure -> onError(auth.message)
        }
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
