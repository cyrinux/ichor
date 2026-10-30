package name.levis.ichor.ui.settings

import android.content.Context
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
import androidx.compose.material3.SegmentedButton
import androidx.compose.material3.SegmentedButtonDefaults
import androidx.compose.material3.SingleChoiceSegmentedButtonRow
import androidx.compose.material3.Text
import name.levis.ichor.ui.design.IchorTextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.Clipboard
import androidx.compose.ui.platform.LocalClipboard
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.LiveRegionMode
import androidx.compose.ui.semantics.liveRegion
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.launch
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.model.GitHost
import name.levis.ichor.model.GitHostCheck
import name.levis.ichor.model.GitHostKind
import name.levis.ichor.model.normalizeGitHost
import name.levis.ichor.model.parseGitHostAliases
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.ui.userMessage

/** Where the Check button is: never run, running, answered, or refused. */
private sealed interface CheckState {
    data object Idle : CheckState
    data object Running : CheckState
    data class Done(val check: GitHostCheck) : CheckState
    data class Failed(val message: String) : CheckState
}

/**
 * Adds a Git host, or changes the aliases, kind or token of [editing]: host (prefilled
 * `github.com`), the other names Argo CD or Flux reach it by, kind, token (masked, pasteable, never read back into the field) and Check, which asks the
 * forge with the typed token, or the stored one when none is typed.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
internal fun GitHostSheet(app: TalosApp, editing: GitHost?, onDismiss: () -> Unit) {
    val store = app.gitHostStore
    val context = LocalContext.current
    val clipboard = LocalClipboard.current
    val scope = rememberCoroutineScope()
    var host by remember { mutableStateOf(editing?.host ?: DEFAULT_HOST) }
    var kind by remember { mutableStateOf(editing?.kind ?: GitHostKind.AUTO) }
    var aliases by remember { mutableStateOf(editing?.aliases.orEmpty().joinToString(", ")) }
    var token by remember { mutableStateOf("") }
    var tokenSaved by remember { mutableStateOf(false) }
    var check by remember { mutableStateOf<CheckState>(CheckState.Idle) }

    // Only whether a token is stored: the token itself never reaches the screen.
    LaunchedEffect(editing) { tokenSaved = editing != null && store.token(editing.host).isNotEmpty() }

    val normalized = normalizeGitHost(host)
    val canCheck = normalized.isNotEmpty() && (token.isNotBlank() || tokenSaved) && check != CheckState.Running
    val canSave = normalized.isNotEmpty() && (token.isNotBlank() || tokenSaved)

    fun runCheck() {
        check = CheckState.Running
        scope.launch {
            check = try {
                CheckState.Done(app.gitOpsRepository.gitHostCheck(GitHost(normalized, kind), token.takeIf { it.isNotBlank() }))
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                CheckState.Failed(e.userMessage())
            }
        }
    }

    ModalBottomSheet(onDismissRequest = onDismiss) {
        Column(
            Modifier.verticalScroll(rememberScrollState()).padding(horizontal = 20.dp).padding(bottom = 16.dp).navigationBarsPadding(),
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            Text(stringResource(if (editing == null) R.string.git_hosts_add else R.string.git_hosts_title), style = MaterialTheme.typography.titleLarge)
            OutlinedTextField(
                value = host,
                onValueChange = {
                    host = it
                    check = CheckState.Idle
                },
                label = { Text(stringResource(R.string.git_host_field_host)) },
                singleLine = true,
                // The host keys its token: changing it is removing and adding again.
                readOnly = editing != null,
                keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Uri, autoCorrectEnabled = false),
                modifier = Modifier.fillMaxWidth(),
            )
            OutlinedTextField(
                value = aliases,
                onValueChange = { aliases = it },
                label = { Text(stringResource(R.string.git_host_field_aliases)) },
                supportingText = { Text(stringResource(R.string.git_host_aliases_hint)) },
                keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Uri, autoCorrectEnabled = false),
                modifier = Modifier.fillMaxWidth(),
            )
            Text(stringResource(R.string.git_host_field_kind), style = MaterialTheme.typography.labelLarge)
            SingleChoiceSegmentedButtonRow(Modifier.fillMaxWidth()) {
                GitHostKind.entries.forEachIndexed { index, k ->
                    SegmentedButton(
                        selected = k == kind,
                        onClick = {
                            kind = k
                            check = CheckState.Idle
                        },
                        shape = SegmentedButtonDefaults.itemShape(index, GitHostKind.entries.size),
                    ) { Text(gitHostKindLabel(k)) }
                }
            }
            OutlinedTextField(
                value = token,
                onValueChange = {
                    token = it
                    check = CheckState.Idle
                },
                label = { Text(stringResource(R.string.git_host_field_token)) },
                supportingText = if (tokenSaved && token.isEmpty()) ({ Text(stringResource(R.string.git_host_token_saved)) }) else null,
                singleLine = true,
                visualTransformation = PasswordVisualTransformation(),
                keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Password, autoCorrectEnabled = false),
                trailingIcon = {
                    IchorTextButton(onClick = {
                        scope.launch {
                            pastedText(clipboard, context)?.let {
                                token = it.trim()
                                check = CheckState.Idle
                            }
                        }
                    }) { Text(stringResource(R.string.git_host_paste)) }
                },
                modifier = Modifier.fillMaxWidth(),
            )
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                IchorOutlinedButton(onClick = ::runCheck, enabled = canCheck) {
                    Text(stringResource(if (check == CheckState.Running) R.string.git_host_checking else R.string.git_host_check))
                }
                if (check == CheckState.Running) CircularProgressIndicator(Modifier.size(20.dp), strokeWidth = 2.dp)
            }
            CheckResult(check)
            Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(8.dp, Alignment.End)) {
                IchorTextButton(onClick = onDismiss) { Text(stringResource(R.string.common_cancel)) }
                IchorButton(
                    onClick = {
                        store.set(normalized, kind, token, parseGitHostAliases(aliases, normalized))
                        (check as? CheckState.Done)?.let { store.rememberLogin(normalized, it.check.login) }
                        onDismiss()
                    },
                    enabled = canSave,
                ) { Text(stringResource(R.string.kb_save)) }
            }
        }
    }
}

/** The answer of the last Check, read out when it changes. */
@Composable
private fun CheckResult(state: CheckState) {
    val colors = LocalStatusColors.current
    val live = Modifier.semantics { liveRegion = LiveRegionMode.Polite }
    when (state) {
        CheckState.Idle, CheckState.Running -> Unit
        is CheckState.Done -> Column(live) {
            Text(stringResource(R.string.git_host_signed_in, state.check.login), color = colors.ok, style = MaterialTheme.typography.bodyMedium)
            if (!state.check.scopesOK && state.check.missing.isNotEmpty()) {
                Text(
                    stringResource(R.string.git_host_missing_scopes, state.check.missing.joinToString(", ")),
                    color = colors.warn,
                    style = MaterialTheme.typography.bodyMedium,
                )
            }
        }
        is CheckState.Failed -> Text(
            stringResource(R.string.git_host_check_failed, state.message),
            color = MaterialTheme.colorScheme.error,
            style = MaterialTheme.typography.bodyMedium,
            modifier = live,
        )
    }
}

/** The clipboard's text, null when it holds none. */
private suspend fun pastedText(clipboard: Clipboard, context: Context): String? {
    val clip = clipboard.getClipEntry()?.clipData ?: return null
    if (clip.itemCount == 0) return null
    return clip.getItemAt(0).coerceToText(context)?.toString()?.takeIf { it.isNotBlank() }
}

private const val DEFAULT_HOST = "github.com"
