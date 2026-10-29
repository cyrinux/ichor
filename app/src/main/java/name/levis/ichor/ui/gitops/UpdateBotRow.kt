package name.levis.ichor.ui.gitops

import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.LiveRegionMode
import androidx.compose.ui.semantics.liveRegion
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.launch
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.model.ClusterRepo
import name.levis.ichor.model.GitProposalDefaults
import name.levis.ichor.model.GitProposalRecord
import name.levis.ichor.model.GitProposalResult
import name.levis.ichor.model.GitSource
import name.levis.ichor.model.UpdateBotConfig
import name.levis.ichor.ui.cancellableCatching
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.ui.uiText
import name.levis.ichor.ui.userMessage

/** Whether the cluster repository has a dependency-update bot: still asking, refused, or answered. */
private sealed interface BotCheck {
    data object Loading : BotCheck
    data class Failed(val message: String) : BotCheck
    data class Loaded(val source: GitSource, val config: UpdateBotConfig) : BotCheck
}

/**
 * Dependency updates in the Cluster repository sheet: whether a bot is configured on [repo]
 * (its file named), else a configuration drafted for it to review and propose.
 */
@Composable
fun UpdateBotRow(app: TalosApp, repo: ClusterRepo) {
    var check by remember(repo) { mutableStateOf<BotCheck>(BotCheck.Loading) }
    var attempt by remember(repo) { mutableIntStateOf(0) }
    var reviewing by remember(repo) { mutableStateOf(false) }
    LaunchedEffect(repo, attempt) {
        check = BotCheck.Loading
        check = cancellableCatching { app.gitOpsRepository.updateBotConfig(repo) }.fold(
            onSuccess = { (src, cfg) -> BotCheck.Loaded(src, cfg) },
            onFailure = { BotCheck.Failed(it.userMessage()) },
        )
    }
    val colors = LocalStatusColors.current
    Column(Modifier.semantics { liveRegion = LiveRegionMode.Polite }, verticalArrangement = Arrangement.spacedBy(4.dp)) {
        Text(stringResource(R.string.repo_update_bot_title), style = MaterialTheme.typography.titleSmall)
        when (val c = check) {
            BotCheck.Loading -> Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                CircularProgressIndicator(Modifier.size(16.dp), strokeWidth = 2.dp)
                MutedText(stringResource(R.string.repo_update_bot_checking))
            }
            is BotCheck.Failed -> {
                Text(stringResource(R.string.repo_update_bot_failed, c.message), color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.bodyMedium)
                TextButton(onClick = { attempt++ }) { Text(stringResource(R.string.common_retry)) }
            }
            is BotCheck.Loaded -> when {
                c.config.configured -> Text(
                    stringResource(R.string.repo_update_bot_present, c.config.present.joinToString(", ") { it.path }),
                    color = colors.ok,
                    style = MaterialTheme.typography.bodyMedium,
                )
                c.config.proposable -> {
                    MutedText(stringResource(R.string.repo_update_bot_none))
                    TextButton(onClick = { reviewing = true }) { Text(stringResource(R.string.repo_update_bot_propose)) }
                }
                else -> MutedText(c.config.notes.firstOrNull().orEmpty())
            }
        }
    }
    val loaded = check as? BotCheck.Loaded
    if (reviewing && loaded != null) UpdateBotDraftSheet(app, loaded.source, loaded.config, onDismiss = { reviewing = false })
}

/**
 * The drafted configuration of [cfg] for [src]: why each part is there, the file itself (read
 * only), then the pull request sheet and its result.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun UpdateBotDraftSheet(app: TalosApp, src: GitSource, cfg: UpdateBotConfig, onDismiss: () -> Unit) {
    val scope = rememberCoroutineScope()
    val gitOps = app.gitOpsRepository
    val title = stringResource(R.string.repo_update_bot_sheet_title)
    var form by remember { mutableStateOf<ProposeForm?>(null) }
    var formOpen by remember { mutableStateOf(false) }
    var result by remember { mutableStateOf<GitProposalResult?>(null) }

    fun openForm() {
        if (form != null) {
            formOpen = true
            return
        }
        scope.launch {
            val d = cancellableCatching { gitOps.repoProposalDefaults(src.repoURL, cfg.draft.path) }.getOrDefault(GitProposalDefaults())
            form = ProposeForm(title, d.message.ifBlank { title }, cfg.proposalBody, d.branch, base = src.tracked, baseFixed = true)
            formOpen = true
        }
    }

    fun propose() {
        val f = form ?: return
        if (f.sending) return
        form = f.copy(sending = true, error = null)
        scope.launch {
            cancellableCatching { gitOps.proposeUpdateBot(src, cfg, f.title, f.message, f.body, f.branch, f.draft) }.fold(
                onSuccess = { r ->
                    gitOps.rememberProposal(
                        GitProposalRecord(
                            repoURL = src.repoURL, path = cfg.draft.path, title = f.title, url = r.url, number = r.number,
                            branch = r.branch, commit = r.commit, forge = r.forge, createdAt = System.currentTimeMillis(),
                        ),
                    )
                    formOpen = false
                    result = r
                },
                onFailure = { e -> form = form?.copy(sending = false, error = e.uiText()) },
            )
        }
    }

    ModalBottomSheet(onDismissRequest = { if (form?.sending != true) onDismiss() }) {
        val done = result
        if (done != null) {
            // ProposeResult scrolls itself.
            ProposeResult(done, tool = "", onDone = onDismiss, modifier = Modifier.navigationBarsPadding())
        } else {
            Column(
                Modifier.verticalScroll(rememberScrollState()).padding(horizontal = 20.dp).padding(bottom = 16.dp).navigationBarsPadding(),
                verticalArrangement = Arrangement.spacedBy(10.dp),
            ) {
                Text(title, style = MaterialTheme.typography.titleLarge)
                MutedText(stringResource(R.string.repo_update_bot_sheet_body, cfg.draft.path))
                cfg.notes.forEach { Text("• $it", style = MaterialTheme.typography.bodyMedium) }
                Surface(color = MaterialTheme.colorScheme.surfaceVariant, shape = MaterialTheme.shapes.small, modifier = Modifier.fillMaxWidth()) {
                    Text(
                        cfg.draft.content,
                        fontFamily = FontFamily.Monospace,
                        style = MaterialTheme.typography.bodySmall,
                        softWrap = false,
                        modifier = Modifier.horizontalScroll(rememberScrollState()).padding(10.dp),
                    )
                }
                Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(8.dp, Alignment.End)) {
                    TextButton(onClick = onDismiss) { Text(stringResource(R.string.common_cancel)) }
                    Button(onClick = ::openForm) { Text(stringResource(R.string.repo_update_bot_continue)) }
                }
            }
        }
    }

    val f = form
    if (f != null && formOpen) {
        ProposeSheet(
            form = f,
            tracked = src.tracked,
            reloading = false,
            onChange = { transform -> form = form?.let { transform(it).copy(error = null, stale = false) } },
            onPropose = ::propose,
            onReload = {},
            onDismiss = { if (form?.sending != true) formOpen = false },
        )
    }
}
