package name.levis.ichor.ui.machineconfig

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.ContentCopy
import androidx.compose.material.icons.outlined.Delete
import androidx.compose.material.icons.outlined.Edit
import name.levis.ichor.ui.design.IchorAlertDialog
import name.levis.ichor.ui.design.IchorCard
import androidx.compose.material3.CardDefaults
import name.levis.ichor.ui.design.IchorFilterChip
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import name.levis.ichor.ui.design.IchorOutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import name.levis.ichor.ui.design.IchorTextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.Saver
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import androidx.lifecycle.ViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharedFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asSharedFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import name.levis.ichor.R
import name.levis.ichor.data.TalosRepository
import name.levis.ichor.model.MetaKey
import name.levis.ichor.model.MetaKeyList
import name.levis.ichor.model.metaHex
import name.levis.ichor.model.parseMetaKey
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.UiText
import name.levis.ichor.ui.components.ErrorBox
import name.levis.ichor.ui.components.LoadingBox
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.TooltipIconButton
import name.levis.ichor.ui.components.copyToClipboard
import name.levis.ichor.ui.node.HostnameConfirmDialog
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.ui.uiStateOf
import name.levis.ichor.ui.uiText
import name.levis.ichor.util.formatBytes

/** The META keys of one node, and the writes and deletes made from the screen. */
class MetaKeysViewModel(private val talos: TalosRepository, private val node: String) : ViewModel() {
    private val _state = MutableStateFlow<UiState<MetaKeyList>>(UiState.Loading)
    val state: StateFlow<UiState<MetaKeyList>> = _state.asStateFlow()
    private val _busy = MutableStateFlow(false)
    val busy: StateFlow<Boolean> = _busy.asStateFlow()
    private val _messages = MutableSharedFlow<UiText>(extraBufferCapacity = 4)
    val messages: SharedFlow<UiText> = _messages.asSharedFlow()

    fun refresh() {
        viewModelScope.launch { _state.value = uiStateOf { talos.metaKeys(node) } }
    }

    fun write(key: Int, value: String) = change(UiText.Res(R.string.meta_written, metaHex(key))) { talos.metaWrite(node, key, value) }

    fun delete(key: Int) = change(UiText.Res(R.string.meta_deleted, metaHex(key))) { talos.metaDelete(node, key) }

    /** Runs [action], says how it went, and reads the keys again either way. */
    private fun change(done: UiText, action: suspend () -> Unit) {
        if (_busy.value) return
        _busy.value = true
        viewModelScope.launch {
            val failure = runCatching { action() }.exceptionOrNull()
            _messages.tryEmit(failure?.uiText() ?: done)
            _state.value = uiStateOf { talos.metaKeys(node) }
            _busy.value = false
        }
    }
}

/** What the user is about to do, before the typed hostname. */
private sealed interface MetaChange {
    val key: Int

    data class Write(override val key: Int, val value: String) : MetaChange

    data class Delete(override val key: Int) : MetaChange
}

/**
 * The META view of the machine config screen. Each write or delete asks for the typed
 * [hostname], then [authenticated] (the app lock) before it reaches the node.
 */
@Composable
fun MetaKeysPane(
    vm: MetaKeysViewModel,
    hostname: String,
    authenticated: (title: Int, action: () -> Unit) -> Unit,
    onMessage: (UiText) -> Unit,
    modifier: Modifier = Modifier,
) {
    val state by vm.state.collectAsStateWithLifecycle()
    val busy by vm.busy.collectAsStateWithLifecycle()
    var editing by rememberSaveable { mutableStateOf<Int?>(null) } // the key being edited; -1: a new one
    var pending by rememberSaveable(stateSaver = metaChangeSaver) { mutableStateOf<MetaChange?>(null) }
    LaunchedEffect(vm) {
        if (vm.state.value == UiState.Loading) vm.refresh()
        vm.messages.collect(onMessage)
    }

    Column(modifier) {
        if (busy) LinearProgressIndicator(Modifier.fillMaxWidth())
        when (val s = state) {
            UiState.Loading -> LoadingBox()
            is UiState.Failed -> ErrorBox(s.message, vm::refresh)
            is UiState.Loaded -> MetaKeyListView(
                s.data,
                enabled = !busy,
                onAdd = { editing = -1 },
                onEdit = { editing = it.key },
                onDelete = { pending = MetaChange.Delete(it.key) },
            )
        }
    }

    val list = (state as? UiState.Loaded)?.data
    editing?.let { key ->
        MetaEditDialog(
            current = list?.keys?.firstOrNull { it.key == key },
            known = list?.known.orEmpty(),
            onWrite = { k, value ->
                editing = null
                pending = MetaChange.Write(k, value)
            },
            onDismiss = { editing = null },
        )
    }

    pending?.let { change ->
        val (title, label, text) = when (change) {
            is MetaChange.Write -> Triple(R.string.meta_write_title, R.string.meta_write, R.string.meta_write_text)
            is MetaChange.Delete -> Triple(R.string.meta_delete_title, R.string.meta_delete, R.string.meta_delete_text)
        }
        HostnameConfirmDialog(
            title = stringResource(title, metaHex(change.key), hostname),
            hostname = hostname,
            confirmLabel = stringResource(label),
            onConfirm = {
                pending = null
                authenticated(R.string.meta_auth) {
                    when (change) {
                        is MetaChange.Write -> vm.write(change.key, change.value)
                        is MetaChange.Delete -> vm.delete(change.key)
                    }
                }
            },
            onDismiss = { pending = null },
            emphasized = true,
        ) {
            Text(stringResource(text), style = MaterialTheme.typography.bodyMedium)
        }
    }
}

private val metaChangeSaver = Saver<MetaChange?, List<Any>>(
    save = { change ->
        when (change) {
            null -> emptyList()
            is MetaChange.Write -> listOf(change.key, change.value)
            is MetaChange.Delete -> listOf(change.key)
        }
    },
    restore = { saved ->
        when (saved.size) {
            2 -> MetaChange.Write(saved[0] as Int, saved[1] as String)
            1 -> MetaChange.Delete(saved[0] as Int)
            else -> null
        }
    },
)

@Composable
private fun MetaKeyListView(list: MetaKeyList, enabled: Boolean, onAdd: () -> Unit, onEdit: (MetaKey) -> Unit, onDelete: (MetaKey) -> Unit) {
    LazyColumn(
        contentPadding = PaddingValues(16.dp),
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        item { MetaBanner() }
        if (!list.loaded) item { MutedText(stringResource(R.string.meta_not_loaded)) }
        if (list.keys.isEmpty()) item { MutedText(stringResource(R.string.meta_empty)) }
        items(list.keys, key = { it.key }) { key -> MetaKeyCard(key, enabled, onEdit, onDelete) }
        item {
            IchorOutlinedButton(onClick = onAdd, enabled = enabled, modifier = Modifier.fillMaxWidth()) {
                Text(stringResource(R.string.meta_add))
            }
        }
    }
}

@Composable
private fun MetaBanner() {
    val bad = LocalStatusColors.current.bad
    IchorCard(colors = CardDefaults.cardColors(containerColor = bad.copy(alpha = 0.15f)), modifier = Modifier.fillMaxWidth()) {
        Text(stringResource(R.string.meta_banner), color = bad, style = MaterialTheme.typography.bodySmall, modifier = Modifier.padding(12.dp))
    }
}

@Composable
private fun MetaKeyCard(key: MetaKey, enabled: Boolean, onEdit: (MetaKey) -> Unit, onDelete: (MetaKey) -> Unit) {
    val context = LocalContext.current
    IchorCard(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(start = 16.dp, top = 8.dp, bottom = 12.dp, end = 4.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Column(Modifier.weight(1f)) {
                    Text(key.hex, style = MaterialTheme.typography.titleSmall, fontFamily = FontFamily.Monospace)
                    MutedText(
                        listOf(key.name.ifEmpty { stringResource(R.string.meta_unknown_key) }, formatBytes(key.size.toLong())).joinToString("  ·  "),
                    )
                }
                if (key.value.isNotEmpty()) {
                    TooltipIconButton(Icons.Outlined.ContentCopy, stringResource(R.string.meta_copy, key.hex), onClick = {
                        copyToClipboard(context, key.hex, key.value, sensitive = true)
                    })
                }
                if (key.editable) {
                    TooltipIconButton(Icons.Outlined.Edit, stringResource(R.string.meta_edit, key.hex), enabled = enabled, onClick = { onEdit(key) })
                }
                if (key.writable) {
                    TooltipIconButton(Icons.Outlined.Delete, stringResource(R.string.meta_delete_key, key.hex), enabled = enabled, onClick = { onDelete(key) })
                }
            }
            when {
                key.secret -> MutedText(stringResource(R.string.meta_secret))
                else -> {
                    if (key.binary) MutedText(stringResource(R.string.meta_binary))
                    Text(key.value, style = MaterialTheme.typography.bodySmall, fontFamily = FontFamily.Monospace, maxLines = 12)
                }
            }
            if (!key.writable) MutedText(stringResource(R.string.meta_read_only))
        }
    }
}

/** Typing a key's value: [current] when editing one, else a key picked from [known] or typed in hex. */
@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun MetaEditDialog(current: MetaKey?, known: List<MetaKey>, onWrite: (Int, String) -> Unit, onDismiss: () -> Unit) {
    var keyText by rememberSaveable { mutableStateOf(current?.hex.orEmpty()) }
    var value by rememberSaveable { mutableStateOf(current?.value.orEmpty()) }
    val key = parseMetaKey(keyText)
    val knownKey = known.firstOrNull { it.key == key }
    val writable = key != null && key != 0 && knownKey?.writable != false
    IchorAlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(stringResource(R.string.meta_dialog_title)) },
        text = {
            Column(Modifier.verticalScroll(rememberScrollState()), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                OutlinedTextField(
                    value = keyText,
                    onValueChange = { keyText = it },
                    label = { Text(stringResource(R.string.meta_key)) },
                    supportingText = {
                        Text(
                            when {
                                keyText.isNotBlank() && key == null -> stringResource(R.string.meta_bad_key)
                                key != null && !writable -> stringResource(R.string.meta_read_only)
                                else -> knownKey?.name ?: ""
                            },
                        )
                    },
                    isError = keyText.isNotBlank() && !writable,
                    enabled = current == null,
                    singleLine = true,
                    keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Ascii, autoCorrectEnabled = false),
                    modifier = Modifier.fillMaxWidth(),
                )
                if (current == null) {
                    FlowRow(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                        known.filter { it.writable }.forEach { k ->
                            IchorFilterChip(selected = k.key == key, onClick = { keyText = k.hex }, label = { Text("${k.hex} ${k.name}") })
                        }
                    }
                }
                OutlinedTextField(
                    value = value,
                    onValueChange = { value = it },
                    label = { Text(stringResource(R.string.meta_value)) },
                    textStyle = MaterialTheme.typography.bodySmall.copy(fontFamily = FontFamily.Monospace),
                    keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Ascii, autoCorrectEnabled = false),
                    modifier = Modifier.fillMaxWidth().heightIn(min = 120.dp),
                )
            }
        },
        confirmButton = {
            IchorTextButton(onClick = { key?.let { onWrite(it, value) } }, enabled = writable && value.isNotEmpty()) {
                Text(stringResource(R.string.meta_write_action))
            }
        },
        dismissButton = { IchorTextButton(onClick = onDismiss) { Text(stringResource(R.string.common_cancel)) } },
    )
}
