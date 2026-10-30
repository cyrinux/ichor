package name.levis.ichor.ui.capture

import androidx.compose.ui.platform.testTag
import name.levis.ichor.ui.components.LoadFailureRow
import name.levis.ichor.ui.components.SMOKE_CAPTURE_START
import androidx.annotation.StringRes
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import name.levis.ichor.ui.design.IchorButton
import name.levis.ichor.ui.design.IchorFilterChip
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.CAPTURE_DURATIONS
import name.levis.ichor.model.CAPTURE_MAX_SIZES
import name.levis.ichor.model.CaptureOptions
import name.levis.ichor.model.CaptureProblem
import name.levis.ichor.model.FilterPreset
import name.levis.ichor.model.LinkInfo
import name.levis.ichor.model.NodeOverview
import name.levis.ichor.model.captureInterfaces
import name.levis.ichor.model.isUp
import name.levis.ichor.model.problem
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.SectionTitle
import name.levis.ichor.ui.components.ToggleRow
import name.levis.ichor.ui.theme.LocalStatusColors

@get:StringRes
private val FilterPreset.label: Int
    get() = when (this) {
        FilterPreset.DNS -> R.string.capture_preset_dns
        FilterPreset.ICMP -> R.string.capture_preset_icmp
        FilterPreset.HTTPS -> R.string.capture_preset_https
        FilterPreset.KUBERNETES_API -> R.string.capture_preset_kubernetes
    }

@get:StringRes
private val CaptureProblem.message: Int
    get() = when (this) {
        CaptureProblem.NO_INTERFACE -> R.string.capture_problem_interface
        CaptureProblem.BAD_FILTER -> R.string.capture_problem_filter
        CaptureProblem.BAD_DURATION, CaptureProblem.BAD_SIZE -> R.string.capture_problem_options
    }

/** "Second node": none, or one of [peers]; then its interface when it differs. */
@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun PeerPicker(peers: UiState<List<NodeOverview>>, options: CaptureOptions, onChange: ((CaptureOptions) -> CaptureOptions) -> Unit) {
    val nodes = (peers as? UiState.Loaded)?.data.orEmpty()
    if (nodes.isEmpty() && !options.paired) return
    SectionTitle(stringResource(R.string.capture_second_node))
    MutedText(stringResource(R.string.capture_second_node_desc))
    FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        IchorFilterChip(
            selected = !options.paired,
            onClick = { onChange { it.copy(peer = "", peerHost = "", peerIface = "") } },
            label = { Text(stringResource(R.string.capture_second_node_none)) },
        )
        // A prefilled node (from the KubeSpan map) shows even before the list loads.
        val shown = if (options.paired && nodes.none { it.node == options.peer }) {
            nodes + NodeOverview(node = options.peer, hostname = options.peerHost, reachable = true)
        } else {
            nodes
        }
        shown.forEach { n ->
            IchorFilterChip(
                selected = n.node == options.peer,
                onClick = { onChange { it.copy(peer = n.node, peerHost = n.hostname.ifBlank { n.node }) } },
                label = { Text(n.hostname.ifBlank { n.node }) },
            )
        }
    }
    if (options.paired) {
        OutlinedTextField(
            value = options.peerIface,
            onValueChange = { value -> onChange { it.copy(peerIface = value.trim()) } },
            label = { Text(stringResource(R.string.capture_second_iface, options.peerHost)) },
            placeholder = { Text(options.iface, fontFamily = FontFamily.Monospace) },
            supportingText = { Text(stringResource(R.string.capture_second_iface_same)) },
            singleLine = true,
            textStyle = MaterialTheme.typography.bodyMedium.copy(fontFamily = FontFamily.Monospace),
            modifier = Modifier.fillMaxWidth(),
        )
    }
}

@Composable
fun durationLabel(seconds: Long): String =
    if (seconds >= 60) stringResource(R.string.capture_minutes, (seconds / 60).toInt()) else stringResource(R.string.capture_seconds, seconds.toInt())

@Composable
fun sizeLabel(bytes: Long): String = stringResource(R.string.capture_mib, (bytes / (1024 * 1024)).toInt())

@OptIn(ExperimentalLayoutApi::class)
@Composable
fun CaptureSetup(
    links: UiState<List<LinkInfo>>,
    options: CaptureOptions,
    filterError: String,
    filterChecking: Boolean,
    onChange: ((CaptureOptions) -> CaptureOptions) -> Unit,
    onStart: () -> Unit,
    /** The other nodes a second capture can run on (null: not offered). */
    peers: UiState<List<NodeOverview>>? = null,
    /** Reads the interfaces again after a failed read (null: no Retry). */
    onRetryLinks: (() -> Unit)? = null,
) {
    val colors = LocalStatusColors.current
    var showVirtual by rememberSaveable { mutableStateOf(false) }
    val problem = options.problem(filterError)
    Column(
        Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        SectionTitle(stringResource(R.string.capture_interface))
        when (links) {
            UiState.Loading -> Text(stringResource(R.string.capture_loading_interfaces), style = MaterialTheme.typography.bodySmall)
            is UiState.Failed -> LoadFailureRow(links.message, onRetryLinks, compact = true)
            is UiState.Loaded -> {
                val shown = captureInterfaces(links.data, showVirtual)
                val hidden = links.data.count { it.virtual }
                if (shown.isEmpty()) {
                    Text(stringResource(R.string.capture_no_interfaces), style = MaterialTheme.typography.bodySmall)
                }
                FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    shown.forEach { link ->
                        IchorFilterChip(
                            selected = link.name == options.iface,
                            onClick = { onChange { it.copy(iface = link.name) } },
                            label = {
                                Text(
                                    link.name,
                                    fontFamily = FontFamily.Monospace,
                                    color = if (link.isUp) MaterialTheme.colorScheme.onSurface else MaterialTheme.colorScheme.onSurfaceVariant,
                                )
                            },
                        )
                    }
                }
                if (hidden > 0) {
                    ToggleRow(
                        title = pluralStringResource(R.plurals.capture_show_virtual, hidden, hidden),
                        checked = showVirtual,
                        onChange = { showVirtual = it },
                    )
                }
            }
        }

        peers?.let { PeerPicker(it, options, onChange) }

        SectionTitle(stringResource(R.string.capture_filter))
        OutlinedTextField(
            value = options.filter,
            onValueChange = { value -> onChange { it.copy(filter = value) } },
            label = { Text(stringResource(R.string.capture_filter)) },
            placeholder = { Text(stringResource(R.string.capture_filter_hint)) },
            isError = options.filter.isNotBlank() && filterError.isNotEmpty(),
            supportingText = if (options.filter.isNotBlank() && filterError.isNotEmpty()) {
                { Text(stringResource(R.string.capture_filter_invalid, filterError)) }
            } else {
                { Text(stringResource(R.string.capture_filter_talos_excluded)) }
            },
            singleLine = true,
            textStyle = MaterialTheme.typography.bodyMedium.copy(fontFamily = FontFamily.Monospace),
            modifier = Modifier.fillMaxWidth(),
        )
        FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            FilterPreset.entries.forEach { preset ->
                IchorFilterChip(
                    selected = options.filter.trim() == preset.expression,
                    onClick = { onChange { it.copy(filter = if (it.filter.trim() == preset.expression) "" else preset.expression) } },
                    label = { Text(stringResource(preset.label)) },
                )
            }
        }

        SectionTitle(stringResource(R.string.capture_duration))
        FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            CAPTURE_DURATIONS.forEach { seconds ->
                IchorFilterChip(
                    selected = options.maxSeconds == seconds,
                    onClick = { onChange { it.copy(maxSeconds = seconds) } },
                    label = { Text(durationLabel(seconds)) },
                )
            }
        }

        SectionTitle(stringResource(if (options.paired) R.string.capture_max_size_each else R.string.capture_max_size))
        FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            CAPTURE_MAX_SIZES.forEach { bytes ->
                IchorFilterChip(
                    selected = options.maxBytes == bytes,
                    onClick = { onChange { it.copy(maxBytes = bytes) } },
                    label = { Text(sizeLabel(bytes)) },
                )
            }
        }

        ToggleRow(
            title = stringResource(R.string.capture_promiscuous),
            description = stringResource(R.string.capture_promiscuous_desc),
            checked = options.promiscuous,
            onChange = { value -> onChange { it.copy(promiscuous = value) } },
        )

        Text(stringResource(R.string.capture_sensitive), style = MaterialTheme.typography.bodySmall, color = colors.warn)
        if (problem != null && links is UiState.Loaded) {
            MutedText(stringResource(problem.message))
        }
        IchorButton(onClick = onStart, enabled = problem == null && !filterChecking, modifier = Modifier.fillMaxWidth().testTag(SMOKE_CAPTURE_START)) {
            Text(stringResource(R.string.capture_start))
        }
    }
}
