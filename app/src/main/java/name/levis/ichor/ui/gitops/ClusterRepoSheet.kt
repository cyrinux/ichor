package name.levis.ichor.ui.gitops

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import name.levis.ichor.ui.design.IchorButton
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import name.levis.ichor.ui.design.IchorOutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import name.levis.ichor.ui.design.IchorTextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.LiveRegionMode
import androidx.compose.ui.semantics.liveRegion
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.launch
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.data.activeSummary
import name.levis.ichor.model.ClusterRepo
import name.levis.ichor.model.GitRepoInfo
import name.levis.ichor.model.isDemo
import name.levis.ichor.model.normalizeClusterRepo
import name.levis.ichor.model.repoCrumb
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.ui.userMessage

/** Where the Check button is: never run, running, answered, or refused. */
private sealed interface RepoCheck {
    data object Idle : RepoCheck
    data object Running : RepoCheck
    data class Done(val info: GitRepoInfo) : RepoCheck
    data class Failed(val message: String) : RepoCheck
}

/** Whether the active cluster has a repository to open: one saved, or the demo's built-in one. */
@Composable
fun hasClusterRepo(): Boolean {
    val app = LocalContext.current.applicationContext as TalosApp
    val config by app.configRepository.config.collectAsStateWithLifecycle()
    val repos by app.clusterRepos.repos.collectAsStateWithLifecycle()
    val active = config?.activeSummary ?: return false
    return active.isDemo || active.fingerprint in repos
}

/**
 * The repository of the cluster [fingerprint] ([label] names it): its address, the branch
 * (blank for the default one), the Talos and Kubernetes directories, and Check, which asks
 * the forge with the host's saved token for the default branch and the token's push right.
 * Without a token for the address's host, says so with a way to Settings → Git hosts
 * ([onGitHosts]).
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ClusterRepoSheet(app: TalosApp, fingerprint: String, label: String, onDismiss: () -> Unit, onGitHosts: (() -> Unit)?) {
    val saved = app.clusterRepos.repos.collectAsStateWithLifecycle().value[fingerprint]
    val hosts by app.gitHostStore.hosts.collectAsStateWithLifecycle()
    val activeFingerprint = app.configRepository.config.collectAsStateWithLifecycle().value?.activeSummary?.fingerprint
    val scope = rememberCoroutineScope()
    var url by remember { mutableStateOf(saved?.repoURL.orEmpty()) }
    var branch by remember { mutableStateOf(saved?.branch.orEmpty()) }
    var talosDir by remember { mutableStateOf(saved?.talosDir.orEmpty()) }
    var kubeDir by remember { mutableStateOf(saved?.kubernetesDir.orEmpty()) }
    var check by remember { mutableStateOf<RepoCheck>(RepoCheck.Idle) }

    val repo = normalizeClusterRepo(ClusterRepo(url, branch, talosDir, kubeDir))
    val host = repo?.host.orEmpty()
    val hostListed = host.isNotEmpty() && hosts.any { it.host == host || it.matches(host) }

    fun runCheck(r: ClusterRepo) {
        check = RepoCheck.Running
        scope.launch {
            check = try {
                RepoCheck.Done(app.gitOpsRepository.repoInfo(r))
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                RepoCheck.Failed(e.userMessage())
            }
        }
    }

    // Any change makes the last answer stale.
    fun edited(set: () -> Unit) {
        set()
        check = RepoCheck.Idle
    }

    ModalBottomSheet(onDismissRequest = onDismiss) {
        Column(
            Modifier.verticalScroll(rememberScrollState()).padding(horizontal = 20.dp).padding(bottom = 16.dp).navigationBarsPadding(),
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            Text(stringResource(R.string.repo_settings_title), style = MaterialTheme.typography.titleLarge)
            MutedText(stringResource(R.string.repo_settings_body, label))
            val uri = KeyboardOptions(keyboardType = KeyboardType.Uri, autoCorrectEnabled = false)
            OutlinedTextField(
                value = url,
                onValueChange = { edited { url = it } },
                label = { Text(stringResource(R.string.repo_field_url)) },
                placeholder = { Text("https://github.com/example-org/cluster-config") },
                supportingText = if (host.isNotEmpty()) ({ Text(stringResource(R.string.repo_host, host)) }) else null,
                singleLine = true,
                keyboardOptions = uri,
                modifier = Modifier.fillMaxWidth(),
            )
            if (host.isNotEmpty() && !hostListed) {
                Text(stringResource(R.string.repo_no_git_host, host), color = LocalStatusColors.current.warn, style = MaterialTheme.typography.bodyMedium)
                onGitHosts?.let { IchorTextButton(onClick = it) { Text(stringResource(R.string.repo_open_git_hosts)) } }
            }
            OutlinedTextField(
                value = branch,
                onValueChange = { edited { branch = it } },
                label = { Text(stringResource(R.string.repo_field_branch)) },
                supportingText = { Text(stringResource(R.string.repo_branch_hint)) },
                singleLine = true,
                keyboardOptions = uri,
                modifier = Modifier.fillMaxWidth(),
            )
            OutlinedTextField(
                value = talosDir,
                onValueChange = { edited { talosDir = it } },
                label = { Text(stringResource(R.string.repo_field_talos_dir)) },
                placeholder = { Text("talos") },
                singleLine = true,
                keyboardOptions = uri,
                modifier = Modifier.fillMaxWidth(),
            )
            OutlinedTextField(
                value = kubeDir,
                onValueChange = { edited { kubeDir = it } },
                label = { Text(stringResource(R.string.repo_field_kubernetes_dir)) },
                placeholder = { Text("kubernetes") },
                singleLine = true,
                keyboardOptions = uri,
                modifier = Modifier.fillMaxWidth(),
            )
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                IchorOutlinedButton(onClick = { repo?.let(::runCheck) }, enabled = repo != null && check != RepoCheck.Running) {
                    Text(stringResource(if (check == RepoCheck.Running) R.string.git_host_checking else R.string.git_host_check))
                }
                if (check == RepoCheck.Running) CircularProgressIndicator(Modifier.size(20.dp), strokeWidth = 2.dp)
            }
            RepoCheckResult(check)
            // The active cluster only: a proposal is recorded in the active cluster's audit log.
            if (activeFingerprint == fingerprint) app.gitOpsRepository.activeClusterRepo()?.let { UpdateBotRow(app, it) }
            MutedText(stringResource(R.string.repo_edits_are_proposals))
            Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(8.dp, Alignment.End)) {
                if (saved != null) {
                    IchorTextButton(onClick = {
                        app.clusterRepos.set(fingerprint, null)
                        onDismiss()
                    }) { Text(stringResource(R.string.common_delete), color = MaterialTheme.colorScheme.error) }
                }
                IchorTextButton(onClick = onDismiss) { Text(stringResource(R.string.common_cancel)) }
                IchorButton(
                    onClick = {
                        app.clusterRepos.set(fingerprint, repo)
                        onDismiss()
                    },
                    enabled = repo != null,
                ) { Text(stringResource(R.string.kb_save)) }
            }
        }
    }
}

/** The answer of the last Check, read out when it changes. */
@Composable
private fun RepoCheckResult(state: RepoCheck) {
    val colors = LocalStatusColors.current
    val live = Modifier.semantics { liveRegion = LiveRegionMode.Polite }
    when (state) {
        RepoCheck.Idle, RepoCheck.Running -> Unit
        is RepoCheck.Done -> Column(live) {
            Text(stringResource(R.string.repo_check_ok, repoCrumb(state.info), state.info.defaultBranch), color = colors.ok, style = MaterialTheme.typography.bodyMedium)
            Text(
                stringResource(if (state.info.canPush) R.string.repo_check_can_push else R.string.repo_check_read_only),
                color = if (state.info.canPush) colors.ok else colors.warn,
                style = MaterialTheme.typography.bodyMedium,
            )
        }
        is RepoCheck.Failed -> Text(
            stringResource(R.string.git_host_check_failed, state.message),
            color = MaterialTheme.colorScheme.error,
            style = MaterialTheme.typography.bodyMedium,
            modifier = live,
        )
    }
}
