package name.levis.ichor.ui.capture

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
import androidx.compose.material3.Button
import androidx.compose.material3.FilterChip
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
import name.levis.ichor.model.captureInterfaces
import name.levis.ichor.model.isUp
import name.levis.ichor.model.problem
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.asString
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
            is UiState.Failed -> Text(links.message.asString(), color = colors.bad, style = MaterialTheme.typography.bodySmall)
            is UiState.Loaded -> {
                val shown = captureInterfaces(links.data, showVirtual)
                val hidden = links.data.count { it.virtual }
                if (shown.isEmpty()) {
                    Text(stringResource(R.string.capture_no_interfaces), style = MaterialTheme.typography.bodySmall)
                }
                FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    shown.forEach { link ->
                        FilterChip(
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

        SectionTitle(stringResource(R.string.capture_filter))
        OutlinedTextField(
            value = options.filter,
            onValueChange = { value -> onChange { it.copy(filter = value) } },
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
                FilterChip(
                    selected = options.filter.trim() == preset.expression,
                    onClick = { onChange { it.copy(filter = if (it.filter.trim() == preset.expression) "" else preset.expression) } },
                    label = { Text(stringResource(preset.label)) },
                )
            }
        }

        SectionTitle(stringResource(R.string.capture_duration))
        FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            CAPTURE_DURATIONS.forEach { seconds ->
                FilterChip(
                    selected = options.maxSeconds == seconds,
                    onClick = { onChange { it.copy(maxSeconds = seconds) } },
                    label = { Text(durationLabel(seconds)) },
                )
            }
        }

        SectionTitle(stringResource(R.string.capture_max_size))
        FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            CAPTURE_MAX_SIZES.forEach { bytes ->
                FilterChip(
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
        Button(onClick = onStart, enabled = problem == null && !filterChecking, modifier = Modifier.fillMaxWidth()) {
            Text(stringResource(R.string.capture_start))
        }
    }
}
