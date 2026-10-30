package name.levis.ichor.ui.runbooks

import androidx.compose.foundation.ExperimentalFoundationApi
import androidx.compose.foundation.combinedClickable
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Add
import androidx.compose.material.icons.outlined.Delete
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.ListItem
import androidx.compose.material3.MaterialTheme
import name.levis.ichor.ui.design.IchorScaffold
import androidx.compose.material3.Text
import name.levis.ichor.ui.design.IchorTopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.launch
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.model.RunbookItem
import name.levis.ichor.ui.components.BackButton
import name.levis.ichor.ui.components.ConfirmDialog
import name.levis.ichor.ui.components.EmptyText
import name.levis.ichor.ui.components.InfoHint
import name.levis.ichor.ui.components.LoadingBox
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.RUNBOOKS_DOCS_URL
import name.levis.ichor.ui.components.TooltipIconButton
import name.levis.ichor.ui.components.pageContent
import name.levis.ichor.ui.userMessage

/**
 * The runbooks kept for the cluster on screen: each with its step and destructive counts;
 * a tap opens its plan, a long press removes it, + imports one, (i) explains the format and
 * links to its documentation.
 */
@OptIn(ExperimentalMaterial3Api::class, ExperimentalFoundationApi::class)
@Composable
fun RunbooksScreen(onBack: () -> Unit, onOpen: (id: String) -> Unit) {
    val app = LocalContext.current.applicationContext as TalosApp
    val repo = app.runbookRepository
    val scope = rememberRunbookScope()
    val coroutines = rememberCoroutineScope()
    var items by remember { mutableStateOf<List<RunbookItem>?>(null) }
    var error by remember { mutableStateOf<String?>(null) }
    var importing by remember { mutableStateOf(false) }
    var removing by remember { mutableStateOf<RunbookItem?>(null) }
    var generation by remember { mutableIntStateOf(0) }

    LaunchedEffect(scope, generation) {
        if (scope == null) return@LaunchedEffect
        try {
            items = repo.list(scope)
            error = null
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            error = e.userMessage()
        }
    }

    IchorScaffold(
        topBar = {
            IchorTopAppBar(
                title = { Text(stringResource(R.string.runbooks_title)) },
                navigationIcon = { BackButton(onBack) },
                actions = {
                    InfoHint(stringResource(R.string.runbooks_title), stringResource(R.string.runbooks_help), link = RUNBOOKS_DOCS_URL)
                    TooltipIconButton(Icons.Outlined.Add, stringResource(R.string.runbooks_import), enabled = scope != null, onClick = { importing = true })
                },
            )
        },
    ) { padding ->
        val list = items
        when {
            error != null -> EmptyText(error.orEmpty(), Modifier.pageContent(padding))
            list == null -> LoadingBox(Modifier.pageContent(padding))
            list.isEmpty() -> EmptyText(stringResource(R.string.runbooks_empty), Modifier.pageContent(padding))
            else -> LazyColumn(Modifier.pageContent(padding)) {
                items(list, key = { it.id }) { item ->
                    RunbookRow(item, Modifier.combinedClickable(
                        onClickLabel = stringResource(R.string.runbook_open_plan),
                        onClick = { if (item.problem.isEmpty()) onOpen(item.id) },
                        onLongClickLabel = stringResource(R.string.runbooks_remove),
                        onLongClick = { removing = item },
                    ))
                    HorizontalDivider()
                }
            }
        }
    }

    if (importing && scope != null) {
        RunbookImportSheet(repo, scope, onImported = { generation++ }, onDismiss = { importing = false })
    }
    removing?.let { item ->
        ConfirmDialog(
            title = stringResource(R.string.runbooks_remove_title, item.name),
            text = stringResource(R.string.runbooks_remove_text),
            confirm = stringResource(R.string.runbooks_remove),
            destructive = true,
            onConfirm = {
                removing = null
                coroutines.launch {
                    runCatching { scope?.let { repo.remove(it, item.id) } }
                    generation++
                }
            },
            onDismiss = { removing = null },
        )
    }
}

@Composable
private fun RunbookRow(item: RunbookItem, modifier: Modifier) {
    ListItem(
        modifier = modifier,
        headlineContent = { Text(item.name) },
        supportingContent = {
            Column {
                if (item.description.isNotEmpty()) MutedText(item.description)
                if (item.problem.isNotEmpty()) {
                    Text(stringResource(R.string.runbooks_problem, item.problem), color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.bodySmall)
                } else {
                    MutedText(stringResource(R.string.runbooks_counts, item.steps.toString(), item.destructive.toString()))
                }
            }
        },
        trailingContent = if (item.problem.isNotEmpty()) {
            { androidx.compose.material3.Icon(Icons.Outlined.Delete, contentDescription = null) }
        } else {
            null
        },
    )
}
