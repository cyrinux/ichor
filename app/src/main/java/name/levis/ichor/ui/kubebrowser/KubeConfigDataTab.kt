package name.levis.ichor.ui.kubebrowser

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.KeyboardArrowRight
import androidx.compose.material.icons.outlined.ContentCopy
import androidx.compose.material.icons.outlined.ExpandLess
import androidx.compose.material.icons.outlined.ExpandMore
import androidx.compose.material.icons.outlined.Visibility
import androidx.compose.material.icons.outlined.VisibilityOff
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import name.levis.ichor.R
import name.levis.ichor.model.CellTone
import name.levis.ichor.model.ConfigCert
import name.levis.ichor.model.ConfigKey
import name.levis.ichor.model.ConfigRegistry
import name.levis.ichor.model.ConfigUse
import name.levis.ichor.model.KubeConfigData
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.components.ErrorBox
import name.levis.ichor.ui.components.LoadingBox
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.SectionTitle
import name.levis.ichor.ui.components.SkeletonStyle
import name.levis.ichor.ui.importconfig.certExpiry
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.util.formatBytes

/** A value longer than this is cut on screen (copy still takes all of it). */
private const val VALUE_PREVIEW_CHARS = 20_000

/** What the data tab does on a tap. */
class ConfigDataActions(
    /** Show the Secret's key (the caller asks the app lock first). */
    val onReveal: (String) -> Unit,
    val onHide: () -> Unit,
    val onCopy: (ConfigKey) -> Unit,
    val onPod: (String) -> Unit,
    val onRetry: () -> Unit,
)

/**
 * The data tab of a Secret or ConfigMap: each key with its size and what it looks like, its
 * certificate's expiry, the value on a tap (a Secret's one key at a time, [revealed]), a docker
 * config's registries, and the pods using it.
 */
@Composable
fun ConfigDataContent(
    state: UiState<KubeConfigData>,
    secret: Boolean,
    revealed: ConfigKey?,
    revealing: String?,
    actions: ConfigDataActions,
    modifier: Modifier = Modifier,
) {
    when (state) {
        UiState.Loading -> Box(modifier) { LoadingBox(style = SkeletonStyle.TEXT) }
        is UiState.Failed -> Box(modifier) { ErrorBox(state.message, actions.onRetry) }
        is UiState.Loaded -> {
            val d = state.data
            val now = remember(d) { System.currentTimeMillis() / 1000 }
            Column(modifier.verticalScroll(rememberScrollState()).padding(16.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
                if (d.type.isNotEmpty()) MutedText(stringResource(R.string.kb_data_type, d.type))
                if (revealed != null) ErrorCard(stringResource(R.string.kb_revealed_warning))
                SectionTitle(stringResource(R.string.kb_data_keys))
                if (d.keys.isEmpty()) MutedText(stringResource(R.string.kb_data_no_keys))
                d.withRevealed(revealed).forEach { k ->
                    if (secret) {
                        SecretKeyRow(k, now, loading = revealing == k.key, actions)
                    } else {
                        ConfigMapKeyRow(k, now, actions.onCopy)
                    }
                }
                if (d.registries.isNotEmpty()) {
                    SectionTitle(stringResource(R.string.kb_data_registries))
                    d.registries.forEach { RegistryRow(it) }
                }
                UsedBy(d, actions.onPod)
            }
        }
    }
}

/**
 * The data tab bound to [vm]: read on first show; [onReveal] asks the app lock then reveals,
 * [onPod] opens a pod. A Secret's value is copied as sensitive (hidden from the clipboard
 * preview and history).
 */
@Composable
fun ConfigDataTab(vm: KubeConfigDataViewModel, onReveal: (String) -> Unit, onPod: (String) -> Unit, modifier: Modifier = Modifier) {
    val context = LocalContext.current
    val state by vm.data.collectAsStateWithLifecycle()
    val revealed by vm.revealed.collectAsStateWithLifecycle()
    val revealing by vm.revealing.collectAsStateWithLifecycle()
    LaunchedEffect(vm) { if (vm.data.value == UiState.Loading) vm.refresh() }
    val actions = ConfigDataActions(
        onReveal = onReveal,
        onHide = vm::hide,
        onCopy = { k -> copyWithToast(context, k.key, k.value, sensitive = vm.ref.isSecret) },
        onPod = onPod,
        onRetry = vm::refresh,
    )
    ConfigDataContent(state, vm.ref.isSecret, revealed, revealing, actions, modifier)
}

/** "1.2 KiB · JSON". */
@Composable
private fun keyFacts(k: ConfigKey): String {
    val hint = when (k.hint) {
        ConfigKey.HINT_JSON -> "JSON"
        ConfigKey.HINT_PEM -> "PEM"
        ConfigKey.HINT_BINARY -> stringResource(R.string.kb_data_hint_binary)
        else -> stringResource(R.string.kb_data_hint_text)
    }
    return "${formatBytes(k.size)}  ·  $hint"
}

@Composable
private fun KeyHeader(k: ConfigKey, modifier: Modifier = Modifier) {
    Column(modifier) {
        Text(k.key, style = MaterialTheme.typography.bodyMedium, fontFamily = FontFamily.Monospace, maxLines = 2, overflow = TextOverflow.Ellipsis)
        MutedText(keyFacts(k), maxLines = 1)
    }
}

/** A Secret's key: hidden until tapped (app lock first), then its value with Copy and Hide. */
@Composable
private fun SecretKeyRow(k: ConfigKey, now: Long, loading: Boolean, actions: ConfigDataActions) {
    val label = stringResource(if (k.revealed) R.string.kb_data_hide_value else R.string.kb_data_show_value)
    Column(Modifier.fillMaxWidth().padding(vertical = 4.dp)) {
        Row(
            Modifier.fillMaxWidth()
                .clickable(enabled = !loading, onClickLabel = label, role = Role.Button) { if (k.revealed) actions.onHide() else actions.onReveal(k.key) }
                .padding(vertical = 4.dp),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            KeyHeader(k, Modifier.weight(1f))
            when {
                loading -> CircularProgressIndicator(Modifier.size(20.dp), strokeWidth = 2.dp)
                else -> Icon(if (k.revealed) Icons.Outlined.VisibilityOff else Icons.Outlined.Visibility, contentDescription = label)
            }
        }
        k.cert?.let { CertLines(it, now) }
        if (k.revealed) ValueBlock(k, actions.onCopy)
    }
}

/** A ConfigMap's key: its value expands on a tap, no lock (the YAML shows it too). */
@Composable
private fun ConfigMapKeyRow(k: ConfigKey, now: Long, onCopy: (ConfigKey) -> Unit) {
    var open by rememberSaveable(k.key) { mutableStateOf(false) }
    val label = stringResource(if (open) R.string.kb_data_hide_value else R.string.kb_data_show_value)
    Column(Modifier.fillMaxWidth().padding(vertical = 4.dp)) {
        Row(
            Modifier.fillMaxWidth().clickable(onClickLabel = label, role = Role.Button) { open = !open }.padding(vertical = 4.dp),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            KeyHeader(k, Modifier.weight(1f))
            Icon(if (open) Icons.Outlined.ExpandLess else Icons.Outlined.ExpandMore, contentDescription = label)
        }
        k.cert?.let { CertLines(it, now) }
        if (open && k.revealed) ValueBlock(k, onCopy)
    }
}

/** The certificate's expiry, tinted when near or past, and whom it is for. */
@Composable
private fun CertLines(c: ConfigCert, now: Long) {
    val color = when (c.tone(now)) {
        CellTone.BAD -> LocalStatusColors.current.bad
        CellTone.WARN -> LocalStatusColors.current.warn
        else -> LocalStatusColors.current.muted
    }
    Text(
        "${stringResource(R.string.common_label_cert_expires)}: ${certExpiry(c.notAfter)}",
        style = MaterialTheme.typography.bodySmall,
        color = color,
    )
    val names = c.dnsNames.ifEmpty { listOf(c.subject) }.filter { it.isNotEmpty() }
    if (names.isNotEmpty()) MutedText(names.joinToString(", "), maxLines = 2, overflow = TextOverflow.Ellipsis)
    if (c.count > 1) MutedText(stringResource(R.string.kb_data_cert_chain, c.count))
}

/** The value, selectable, cut when huge; Copy takes all of it. */
@Composable
private fun ValueBlock(k: ConfigKey, onCopy: (ConfigKey) -> Unit) {
    Column(Modifier.fillMaxWidth().padding(top = 4.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
        if (k.base64) MutedText(stringResource(R.string.kb_data_base64))
        SelectionContainer {
            Text(
                if (k.value.length > VALUE_PREVIEW_CHARS) k.value.take(VALUE_PREVIEW_CHARS) + "…" else k.value,
                style = MaterialTheme.typography.bodySmall,
                fontFamily = FontFamily.Monospace,
            )
        }
        TextButton(onClick = { onCopy(k) }) {
            Icon(Icons.Outlined.ContentCopy, contentDescription = null, modifier = Modifier.padding(end = 6.dp).size(18.dp))
            Text(stringResource(R.string.kb_data_copy_value))
        }
    }
}

@Composable
private fun RegistryRow(r: ConfigRegistry) {
    Column(Modifier.fillMaxWidth().padding(vertical = 2.dp)) {
        Text(r.registry, style = MaterialTheme.typography.bodyMedium, fontFamily = FontFamily.Monospace, maxLines = 1, overflow = TextOverflow.Ellipsis)
        if (r.username.isNotEmpty()) MutedText(stringResource(R.string.kb_data_registry_user, r.username), maxLines = 1)
    }
}

@Composable
private fun UsedBy(d: KubeConfigData, onPod: (String) -> Unit) {
    SectionTitle(stringResource(R.string.kb_data_used_by))
    when {
        d.usedByUnknown -> MutedText(stringResource(R.string.kb_data_used_by_unknown))
        d.usedBy.isEmpty() -> MutedText(stringResource(R.string.kb_data_used_by_none))
        else -> d.usedBy.forEach { UseRow(it, onPod) }
    }
}

/** A pod using the object, how it does (Kubernetes' field names), opening the pod. */
@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun UseRow(u: ConfigUse, onPod: (String) -> Unit) {
    Row(
        Modifier.fillMaxWidth().clickable(role = Role.Button) { onPod(u.pod) }.padding(vertical = 6.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(4.dp)) {
            Text(u.pod, style = MaterialTheme.typography.bodyMedium, fontFamily = FontFamily.Monospace, maxLines = 1, overflow = TextOverflow.Ellipsis)
            FlowRow(horizontalArrangement = Arrangement.spacedBy(6.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                u.via.forEach { ToneLabel(it, LocalStatusColors.current.muted, mono = true) }
            }
        }
        Icon(Icons.AutoMirrored.Outlined.KeyboardArrowRight, contentDescription = null)
    }
}
