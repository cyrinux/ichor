package name.levis.talosmobile.ui.machineconfig

import android.content.ClipData
import android.content.ClipboardManager
import android.content.Context
import android.os.PersistableBundle
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.outlined.ContentCopy
import androidx.compose.material.icons.outlined.Search
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
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
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.text.SpanStyle
import androidx.compose.ui.text.buildAnnotatedString
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import name.levis.talosmobile.R
import name.levis.talosmobile.TalosApp
import name.levis.talosmobile.data.TalosRepository
import name.levis.talosmobile.security.AuthResult
import name.levis.talosmobile.security.SecureWhile
import name.levis.talosmobile.security.authenticate
import name.levis.talosmobile.security.findFragmentActivity
import name.levis.talosmobile.ui.LoadingViewModel
import name.levis.talosmobile.ui.UiState
import name.levis.talosmobile.ui.app
import name.levis.talosmobile.ui.components.ErrorBox
import name.levis.talosmobile.ui.components.LoadingBox
import name.levis.talosmobile.ui.factory
import name.levis.talosmobile.ui.theme.LocalStatusColors

/** The node's machine config (os:admin). Redacted unless the user asked to reveal secrets. */
class MachineConfigViewModel(private val talos: TalosRepository, private val node: String) : LoadingViewModel<String>() {
    private val _revealed = MutableStateFlow(false)
    val revealed: StateFlow<Boolean> = _revealed.asStateFlow()

    // Not cached anywhere: the revealed config holds the cluster's secrets.
    override suspend fun fetch() = talos.machineConfig(node, _revealed.value)

    fun reveal(on: Boolean) {
        if (_revealed.value == on) return
        _revealed.value = on
        refresh(reset = true) // never show secrets after hiding them, even while refetching
    }
}

/** Lines of [text] containing [query] (case-insensitive); every line when [query] is blank. */
fun matchingLines(text: String, query: String): List<String> {
    val lines = text.lines()
    val q = query.trim()
    return if (q.isEmpty()) lines else lines.filter { it.contains(q, ignoreCase = true) }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun MachineConfigScreen(
    node: String,
    hostname: String,
    onBack: () -> Unit,
    vm: MachineConfigViewModel = viewModel(key = "machineconfig-$node", factory = factory { MachineConfigViewModel(app.talosRepository, node) }),
) {
    val state by vm.state.collectAsStateWithLifecycle()
    val revealed by vm.revealed.collectAsStateWithLifecycle()
    LaunchedEffect(Unit) { if (state == UiState.Loading) vm.refresh() }

    val context = LocalContext.current
    val app = context.applicationContext as TalosApp
    val scope = rememberCoroutineScope()
    val snackbar = remember { SnackbarHostState() }
    var query by rememberSaveable { mutableStateOf("") }

    SecureWhile(revealed)

    // Revealing secrets needs a fresh fingerprint/PIN when the app lock is on, like reboot.
    fun toggleReveal(on: Boolean) {
        val activity = context.findFragmentActivity()
        if (!on || !app.appLock.enabled.value || activity == null) {
            vm.reveal(on)
            return
        }
        scope.launch {
            when (val auth = authenticate(activity, context.getString(R.string.machine_config_auth, hostname))) {
                AuthResult.Success -> vm.reveal(true)
                is AuthResult.Failure -> snackbar.showSnackbar(auth.message)
            }
        }
    }

    val loaded = (state as? UiState.Loaded)?.data
    val shown = remember(loaded, query) { loaded?.let { matchingLines(it, query) } }

    Scaffold(
        snackbarHost = { SnackbarHost(snackbar) },
        topBar = {
            TopAppBar(
                title = {
                    Column {
                        Text(stringResource(R.string.machine_config_title))
                        Text(hostname, style = MaterialTheme.typography.labelMedium, fontFamily = FontFamily.Monospace)
                    }
                },
                navigationIcon = { IconButton(onClick = onBack) { Icon(Icons.AutoMirrored.Filled.ArrowBack, stringResource(R.string.common_back)) } },
                actions = {
                    IconButton(
                        enabled = shown != null,
                        onClick = {
                            shown?.let {
                                copy(context, it.joinToString("\n"), sensitive = revealed)
                                scope.launch { snackbar.showSnackbar(context.getString(R.string.machine_config_copied)) }
                            }
                        },
                    ) { Icon(Icons.Outlined.ContentCopy, stringResource(R.string.machine_config_copy)) }
                },
            )
        },
    ) { padding ->
        Column(Modifier.padding(padding).fillMaxSize()) {
            Column(Modifier.padding(horizontal = 16.dp, vertical = 8.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Column(Modifier.weight(1f)) {
                        Text(stringResource(R.string.machine_config_reveal), style = MaterialTheme.typography.titleSmall)
                        Text(
                            stringResource(R.string.machine_config_reveal_desc),
                            style = MaterialTheme.typography.bodySmall,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                    }
                    Switch(checked = revealed, onCheckedChange = ::toggleReveal)
                }
                if (revealed) RevealedBanner()
                OutlinedTextField(
                    value = query,
                    onValueChange = { query = it },
                    placeholder = { Text(stringResource(R.string.machine_config_search)) },
                    leadingIcon = { Icon(Icons.Outlined.Search, contentDescription = null) },
                    supportingText = shown?.takeIf { query.isNotBlank() }?.let {
                        { Text(pluralStringResource(R.plurals.machine_config_matches, it.size, it.size)) }
                    },
                    singleLine = true,
                    modifier = Modifier.fillMaxWidth(),
                )
            }
            when (val s = state) {
                UiState.Loading -> LoadingBox()
                is UiState.Failed -> ErrorBox(s.message, vm::refresh)
                is UiState.Loaded -> {
                    if (s.refreshing) LinearProgressIndicator(Modifier.fillMaxWidth())
                    YamlView(shown.orEmpty(), query)
                }
            }
        }
    }
}

@Composable
private fun RevealedBanner() {
    val bad = LocalStatusColors.current.bad
    Card(colors = CardDefaults.cardColors(containerColor = bad.copy(alpha = 0.15f)), modifier = Modifier.fillMaxWidth()) {
        Text(
            stringResource(R.string.machine_config_revealed_warning),
            color = bad,
            style = MaterialTheme.typography.bodySmall,
            modifier = Modifier.padding(12.dp),
        )
    }
}

@Composable
private fun YamlView(lines: List<String>, query: String) {
    val highlight = MaterialTheme.colorScheme.tertiaryContainer
    val text = remember(lines, query, highlight) { highlighted(lines, query.trim(), highlight) }
    SelectionContainer {
        Box(
            Modifier
                .fillMaxSize()
                .verticalScroll(rememberScrollState())
                .horizontalScroll(rememberScrollState())
                .padding(horizontal = 16.dp, vertical = 8.dp),
        ) {
            Text(text, style = MaterialTheme.typography.bodySmall, fontFamily = FontFamily.Monospace, softWrap = false)
        }
    }
}

private fun highlighted(lines: List<String>, query: String, color: Color): AnnotatedString = buildAnnotatedString {
    val text = lines.joinToString("\n")
    append(text)
    if (query.isEmpty()) return@buildAnnotatedString
    var from = text.indexOf(query, ignoreCase = true)
    while (from >= 0) {
        addStyle(SpanStyle(background = color), from, from + query.length)
        from = text.indexOf(query, from + query.length, ignoreCase = true)
    }
}

private fun copy(context: Context, text: String, sensitive: Boolean) {
    val clip = ClipData.newPlainText(context.getString(R.string.machine_config_title), text)
    if (sensitive) {
        // Keeps revealed secrets out of the clipboard preview (honoured from Android 13).
        clip.description.extras = PersistableBundle().apply { putBoolean("android.content.extra.IS_SENSITIVE", true) }
    }
    context.getSystemService(ClipboardManager::class.java).setPrimaryClip(clip)
}
