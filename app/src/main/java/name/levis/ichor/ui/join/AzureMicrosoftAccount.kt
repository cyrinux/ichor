package name.levis.ichor.ui.join

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.launch
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put
import name.levis.ichor.R
import name.levis.ichor.model.AzureCheck
import name.levis.ichor.model.AzureSignInChoice
import name.levis.ichor.model.AzureSignInFinish
import name.levis.ichor.model.AzureSignInStart
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.UiText
import name.levis.ichor.ui.components.ErrorBox
import name.levis.ichor.ui.components.LoadingBox
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.design.IchorButton
import name.levis.ichor.ui.design.IchorFilterChip
import name.levis.ichor.ui.kubeauth.Waiting
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.ui.uiStateOf

/** What a test of the principal or the sign-in says: which subscription, and whether it may create VMs in [resourceGroup] ("" = the subscription). */
@Composable
internal fun AzureCheckLine(data: AzureCheck, resourceGroup: String) {
    val colors = LocalStatusColors.current
    val name = data.subscriptionName.ifBlank { data.subscription }
    val where = resourceGroup.ifBlank { name }
    Text(
        when {
            !data.checked -> stringResource(R.string.azure_check_unknown, name, where)
            data.canCreate -> stringResource(R.string.azure_check_ok, name, where)
            else -> stringResource(R.string.azure_check_no, name, where)
        },
        color = if (data.checked && !data.canCreate) colors.warn else colors.ok,
        style = MaterialTheme.typography.bodySmall,
    )
}

/** Where the Microsoft-account side of the account dialog is. */
private sealed interface AccountStep {
    data object Form : AccountStep
    data object Working : AccountStep
    data class Device(val start: AzureSignInStart) : AccountStep
    data class Subscriptions(val finish: AzureSignInFinish) : AccountStep
    data class Failed(val message: UiText) : AccountStep
    data class Chosen(val choice: AzureSignInChoice, val check: UiState<AzureCheck>) : AccountStep
}

/**
 * The Microsoft-account side of the account dialog, as `az login --use-device-code`: an optional
 * tenant and client id, the code to enter in any browser, the subscription to take, then the same
 * test as a principal. [onReady] gets the credential JSON to seal and its test once they pass,
 * null meanwhile. The refresh token stays in that JSON, never on screen.
 */
@OptIn(ExperimentalLayoutApi::class)
@Composable
internal fun MicrosoftAccountSide(vm: AzureCreateViewModel, onReady: (credential: String?, check: AzureCheck?) -> Unit) {
    val scope = rememberCoroutineScope()
    var tenant by remember { mutableStateOf("") }
    var clientId by remember { mutableStateOf("") }
    var cloud by remember { mutableStateOf(AzureCloud.PUBLIC) }
    var step by remember { mutableStateOf<AccountStep>(AccountStep.Form) }
    // The sheet closed or the dialog switched to a principal: the wait for the browser ends.
    DisposableEffect(Unit) { onDispose { vm.cancelSignIn() } }

    fun choose(credential: String, subscriptionId: String) {
        step = AccountStep.Working
        scope.launch {
            step = when (val chosen = uiStateOf { vm.signInChoose(credential, subscriptionId) }) {
                is UiState.Loaded -> {
                    val check = uiStateOf { vm.test(chosen.data.credential) }
                    val tested = (check as? UiState.Loaded)?.data
                    if (tested != null) onReady(tested.credential.ifBlank { chosen.data.credential }, tested)
                    AccountStep.Chosen(chosen.data, check)
                }
                is UiState.Failed -> AccountStep.Failed(chosen.message)
                else -> AccountStep.Form
            }
        }
    }

    fun signIn() {
        onReady(null, null)
        step = AccountStep.Working
        val request = buildJsonObject {
            put("tenantId", tenant.trim())
            put("clientId", clientId.trim())
            put("environment", cloud.environment)
        }.toString()
        scope.launch {
            val start = uiStateOf { vm.signInStart(request) }
            if (start !is UiState.Loaded) {
                step = AccountStep.Failed((start as? UiState.Failed)?.message ?: UiText.Raw(""))
                return@launch
            }
            step = AccountStep.Device(start.data)
            val finish = uiStateOf { vm.signInFinish(start.data.credential) }
            // Cancelled meanwhile: the form is back already.
            if (step !is AccountStep.Device) return@launch
            when {
                finish is UiState.Loaded && finish.data.subscriptions.size == 1 -> choose(finish.data.credential, finish.data.subscriptions.first().id)
                finish is UiState.Loaded -> step = AccountStep.Subscriptions(finish.data)
                else -> step = AccountStep.Failed((finish as? UiState.Failed)?.message ?: UiText.Raw(""))
            }
        }
    }

    MutedText(stringResource(R.string.azure_signin_hint))
    when (val current = step) {
        AccountStep.Form -> {
            FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                AzureCloud.entries.forEach { c ->
                    IchorFilterChip(selected = c == cloud, onClick = { cloud = c }, label = { Text(c.label) })
                }
            }
            IdField(tenant, R.string.azure_signin_tenant, secret = false) { tenant = it }
            IdField(clientId, R.string.azure_signin_client_id, secret = false) { clientId = it }
            IchorButton(onClick = ::signIn, modifier = Modifier.fillMaxWidth()) { Text(stringResource(R.string.kube_signin_action)) }
        }
        AccountStep.Working -> LoadingBox()
        is AccountStep.Device -> Waiting(current.start.prompt, onCancel = {
            vm.cancelSignIn()
            step = AccountStep.Form
        }, onCode = {})
        is AccountStep.Subscriptions -> {
            Text(stringResource(R.string.azure_signin_choose_subscription), style = MaterialTheme.typography.labelLarge)
            FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                current.finish.subscriptions.forEach { s ->
                    IchorFilterChip(selected = false, onClick = { choose(current.finish.credential, s.id) }, label = { Text(s.label) })
                }
            }
        }
        is AccountStep.Failed -> ErrorBox(current.message, onRetry = { step = AccountStep.Form })
        is AccountStep.Chosen -> {
            Text(stringResource(R.string.azure_signin_signed_in, current.choice.account), style = MaterialTheme.typography.bodySmall)
            when (val check = current.check) {
                is UiState.Loaded -> AzureCheckLine(check.data, "")
                is UiState.Failed -> ErrorBox(check.message, onRetry = { step = AccountStep.Form })
                else -> Unit
            }
            MutedText(stringResource(R.string.azure_signin_session_note))
        }
    }
}
