package name.levis.ichor.ui.nettools

import androidx.annotation.StringRes
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Share
import androidx.compose.material3.Button
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilterChip
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
import androidx.compose.material3.SuggestionChip
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import name.levis.ichor.R
import name.levis.ichor.data.activeSummary
import name.levis.ichor.model.NET_DNS_RECORDS
import name.levis.ichor.model.NetTargetProblem
import name.levis.ichor.model.NetTool
import name.levis.ichor.model.NetToolOptions
import name.levis.ichor.model.netTargetProblem
import name.levis.ichor.ui.app
import name.levis.ichor.ui.components.BackButton
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.SectionTitle
import name.levis.ichor.ui.components.TooltipIconButton
import name.levis.ichor.ui.components.pageContent
import name.levis.ichor.ui.components.shareText
import name.levis.ichor.ui.factory
import name.levis.ichor.ui.theme.LocalStatusColors

@get:StringRes
val NetTool.label: Int
    get() = when (this) {
        NetTool.DNS -> R.string.net_tools_dns
        NetTool.PING -> R.string.net_tools_ping
        NetTool.PORT -> R.string.net_tools_port
        NetTool.TRACE -> R.string.net_tools_trace
        NetTool.HTTP -> R.string.net_tools_http
    }

@get:StringRes
private val NetTool.hint: Int
    get() = when (this) {
        NetTool.DNS -> R.string.net_tools_hint_dns
        NetTool.PING, NetTool.TRACE -> R.string.net_tools_hint_host
        NetTool.PORT -> R.string.net_tools_hint_port
        NetTool.HTTP -> R.string.net_tools_hint_http
    }

@get:StringRes
private val NetTargetProblem.message: Int
    get() = when (this) {
        NetTargetProblem.EMPTY -> R.string.net_tools_target_empty
        NetTargetProblem.CHARACTERS -> R.string.net_tools_target_characters
        NetTargetProblem.HOST_PORT -> R.string.net_tools_target_host_port
        NetTargetProblem.URL -> R.string.net_tools_target_url
        NetTargetProblem.HOST -> R.string.net_tools_target_host
    }

private val PING_COUNTS = listOf(3, 5, 10)

/**
 * Network checks run from [node] in a privileged netshoot container (os:admin, like the debug
 * shell): DNS, ping, a TCP port, the path, an HTTP(S) URL. One at a time; each result reads
 * as fields, with the raw output underneath.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun NetToolsScreen(
    node: String,
    hostname: String,
    onBack: () -> Unit,
    vm: NetToolsViewModel = viewModel(
        key = "nettools-$node",
        factory = factory {
            NetToolsViewModel(app.talosRepository, app.netToolTargets, node, app.configRepository.config.value?.activeSummary?.fingerprint.orEmpty())
        },
    ),
) {
    val context = LocalContext.current
    val run by vm.run.collectAsStateWithLifecycle()
    val recent by vm.recent.collectAsStateWithLifecycle()
    var tool by rememberSaveable { mutableStateOf(NetTool.DNS) }
    var target by rememberSaveable { mutableStateOf("") }
    var record by rememberSaveable { mutableStateOf("A") }
    var server by rememberSaveable { mutableStateOf("") }
    var count by rememberSaveable { mutableIntStateOf(3) }
    var touched by rememberSaveable { mutableStateOf(false) }
    val problem = netTargetProblem(tool, target)
    // Said once something is typed: an empty field is not an error yet.
    val shown = problem?.takeIf { touched || target.isNotEmpty() }

    fun start() {
        touched = true
        val options = when (tool) {
            NetTool.DNS -> NetToolOptions(record = record, server = server.trim())
            NetTool.PING -> NetToolOptions(count = count)
            else -> NetToolOptions()
        }
        vm.start(tool, target, options)
    }

    Scaffold(
        topBar = {
            TopAppBar(
                title = {
                    Column {
                        Text(stringResource(R.string.node_menu_net_tools))
                        Text(hostname, style = MaterialTheme.typography.labelMedium)
                    }
                },
                navigationIcon = { BackButton(onBack) },
                actions = {
                    run?.result?.let { result ->
                        TooltipIconButton(Icons.Outlined.Share, stringResource(R.string.net_tools_share), onClick = {
                            shareText(context, netToolShareText(context, hostname, result), context.getString(R.string.net_tools_share))
                        })
                    }
                },
            )
        },
    ) { padding ->
        Column(
            Modifier.pageContent(padding).fillMaxSize().verticalScroll(rememberScrollState()).padding(16.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            val current = run
            if (current != null) {
                RunView(current, onCancel = vm::cancel, onAgain = vm::clear)
                return@Column
            }
            MutedText(stringResource(R.string.net_tools_intro))
            Row(Modifier.horizontalScroll(rememberScrollState()), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                NetTool.entries.forEach { t ->
                    FilterChip(selected = t == tool, onClick = { tool = t }, label = { Text(stringResource(t.label)) })
                }
            }
            OutlinedTextField(
                value = target,
                onValueChange = { target = it },
                label = { Text(stringResource(R.string.net_tools_target)) },
                placeholder = { Text(stringResource(tool.hint), fontFamily = FontFamily.Monospace) },
                isError = shown != null,
                supportingText = shown?.let { p -> { Text(stringResource(p.message)) } },
                singleLine = true,
                keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Uri, imeAction = ImeAction.Go),
                modifier = Modifier.fillMaxWidth(),
            )
            if (recent.isNotEmpty()) {
                Row(Modifier.horizontalScroll(rememberScrollState()), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    recent.forEach { r -> SuggestionChip(onClick = { target = r }, label = { Text(r, fontFamily = FontFamily.Monospace) }) }
                }
            }
            when (tool) {
                NetTool.DNS -> {
                    SectionTitle(stringResource(R.string.net_tools_record))
                    Row(Modifier.horizontalScroll(rememberScrollState()), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                        NET_DNS_RECORDS.forEach { r -> FilterChip(selected = r == record, onClick = { record = r }, label = { Text(r) }) }
                    }
                    OutlinedTextField(
                        value = server,
                        onValueChange = { server = it },
                        label = { Text(stringResource(R.string.net_tools_resolver)) },
                        placeholder = { Text("10.96.0.10", fontFamily = FontFamily.Monospace) },
                        supportingText = { Text(stringResource(R.string.net_tools_resolver_desc)) },
                        singleLine = true,
                        modifier = Modifier.fillMaxWidth(),
                    )
                }
                NetTool.PING -> {
                    SectionTitle(stringResource(R.string.net_tools_count))
                    Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                        PING_COUNTS.forEach { c -> FilterChip(selected = c == count, onClick = { count = c }, label = { Text("$c") }) }
                    }
                }
                else -> Unit
            }
            MutedText(stringResource(R.string.net_tools_privileged))
            Button(onClick = ::start, enabled = problem == null, modifier = Modifier.fillMaxWidth()) {
                Text(stringResource(R.string.net_tools_run))
            }
        }
    }
}

@Composable
private fun RunView(run: NetToolRunState, onCancel: () -> Unit, onAgain: () -> Unit) {
    val colors = LocalStatusColors.current
    Text("${stringResource(run.tool.label)} · ${run.target}", style = MaterialTheme.typography.titleMedium)
    val result = run.result
    when {
        run.running -> {
            LinearProgressIndicator(Modifier.fillMaxWidth())
            OutputLines(run.lines.takeLast(OUTPUT_TAIL))
            OutlinedButton(onClick = onCancel, modifier = Modifier.fillMaxWidth()) { Text(stringResource(R.string.common_cancel)) }
        }
        result != null -> {
            NetToolResultCard(result)
            RawOutput(result.raw)
            Button(onClick = onAgain, modifier = Modifier.fillMaxWidth()) { Text(stringResource(R.string.net_tools_again)) }
        }
        else -> {
            Text(run.error.orEmpty().ifEmpty { stringResource(R.string.net_tools_no_answer) }, color = colors.bad)
            if (run.lines.isNotEmpty()) OutputLines(run.lines.takeLast(OUTPUT_TAIL))
            Button(onClick = onAgain, modifier = Modifier.fillMaxWidth()) { Text(stringResource(R.string.net_tools_again)) }
        }
    }
}

/** The last output lines shown while a check runs. */
private const val OUTPUT_TAIL = 30

@Composable
private fun OutputLines(lines: List<String>) {
    if (lines.isEmpty()) return
    Text(
        lines.joinToString("\n"),
        style = MaterialTheme.typography.bodySmall,
        fontFamily = FontFamily.Monospace,
        modifier = Modifier.fillMaxWidth(),
    )
}
