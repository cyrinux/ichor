package name.levis.ichor.ui.resources

import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.ContentCopy
import androidx.compose.material.icons.outlined.Lock
import androidx.compose.material.icons.outlined.Refresh
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.material3.Button
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.text.style.TextAlign
import name.levis.ichor.ui.components.BackButton
import name.levis.ichor.ui.components.copyToClipboard
import name.levis.ichor.ui.theme.LocalStatusColors
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import kotlinx.coroutines.launch
import name.levis.ichor.R
import name.levis.ichor.data.TalosRepository
import name.levis.ichor.security.SecureWhile
import name.levis.ichor.ui.LoadingViewModel
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.app
import name.levis.ichor.ui.components.ErrorBox
import name.levis.ichor.ui.components.LoadingBox
import name.levis.ichor.ui.factory
import name.levis.ichor.ui.components.TooltipIconButton

/** One resource as YAML. Never cached: a sensitive one holds secrets. */
class ResourceDetailViewModel(private val talos: TalosRepository, private val ref: ResourceRef, private val id: String) : LoadingViewModel<String>() {
    override suspend fun fetch() = talos.resourceGet(ref.node, ref.namespace, ref.type, id)
}

/** `talosctl get TYPE ID -o yaml`. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ResourceDetailScreen(
    ref: ResourceRef,
    id: String,
    onBack: () -> Unit,
    vm: ResourceDetailViewModel = viewModel(
        key = "resource-${ref.node}-${ref.namespace}-${ref.type}-$id",
        factory = factory { ResourceDetailViewModel(app.talosRepository, ref, id) },
    ),
) {
    val state by vm.state.collectAsStateWithLifecycle()
    // A sensitive resource is only fetched (and shown) once the user asked for it.
    var revealed by rememberSaveable { mutableStateOf(!ref.sensitive) }
    LaunchedEffect(revealed) { if (revealed && state == UiState.Loading) vm.refresh() }
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    val snackbar = remember { SnackbarHostState() }
    val yaml = (state as? UiState.Loaded)?.data

    // Secrets on screen: no screenshot, no recents thumbnail.
    SecureWhile(ref.sensitive)

    Scaffold(
        snackbarHost = { SnackbarHost(snackbar) },
        topBar = {
            TopAppBar(
                title = {
                    Column {
                        Row(verticalAlignment = Alignment.CenterVertically) {
                            if (ref.sensitive) {
                                Icon(
                                    Icons.Outlined.Lock,
                                    contentDescription = stringResource(R.string.resources_sensitive),
                                    modifier = Modifier.padding(end = 6.dp).size(18.dp),
                                )
                            }
                            Text(id, maxLines = 1, overflow = TextOverflow.Ellipsis)
                        }
                        Text(
                            listOf(ref.namespace, ref.type).filter { it.isNotEmpty() }.joinToString(" · "),
                            style = MaterialTheme.typography.labelMedium,
                            fontFamily = FontFamily.Monospace,
                            maxLines = 1,
                            overflow = TextOverflow.Ellipsis,
                        )
                    }
                },
                navigationIcon = { BackButton(onBack) },
                actions = {
                    TooltipIconButton(Icons.Outlined.Refresh, stringResource(R.string.common_refresh), onClick = vm::refresh, enabled = revealed)
                    TooltipIconButton(
                        Icons.Outlined.ContentCopy,
                        stringResource(R.string.resources_copy),
                        enabled = yaml != null,
                        onClick = {
                            yaml?.let {
                                copyToClipboard(context, id, it, ref.sensitive)
                                scope.launch { snackbar.showSnackbar(context.getString(R.string.machine_config_copied)) }
                            }
                        },
                    )
                },
            )
        },
    ) { padding ->
        Column(Modifier.padding(padding).fillMaxSize()) {
            if (!revealed) {
                SensitiveWarning(onShow = { revealed = true })
                return@Column
            }
            when (val s = state) {
                UiState.Loading -> LoadingBox()
                is UiState.Failed -> ErrorBox(s.message, vm::refresh)
                is UiState.Loaded -> {
                    if (s.refreshing) LinearProgressIndicator(Modifier.fillMaxWidth())
                    SelectionContainer {
                        Box(
                            Modifier
                                .fillMaxSize()
                                .verticalScroll(rememberScrollState())
                                .horizontalScroll(rememberScrollState())
                                .padding(horizontal = 16.dp, vertical = 8.dp),
                        ) {
                            Text(s.data, style = MaterialTheme.typography.bodySmall, fontFamily = FontFamily.Monospace, softWrap = false)
                        }
                    }
                }
            }
        }
    }
}

@Composable
private fun SensitiveWarning(onShow: () -> Unit) {
    Column(Modifier.fillMaxWidth().padding(24.dp), horizontalAlignment = Alignment.CenterHorizontally) {
        Icon(Icons.Outlined.Lock, contentDescription = null, tint = LocalStatusColors.current.warn)
        Text(
            stringResource(R.string.resources_sensitive_warning),
            style = MaterialTheme.typography.bodyMedium,
            textAlign = TextAlign.Center,
            modifier = Modifier.padding(vertical = 12.dp),
        )
        Button(onClick = onShow) { Text(stringResource(R.string.resources_show)) }
    }
}

