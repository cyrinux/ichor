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
import name.levis.ichor.model.AWSCheck
import name.levis.ichor.model.AWSSignInChoice
import name.levis.ichor.model.AWSSignInFinish
import name.levis.ichor.model.AWSSignInStart
import name.levis.ichor.model.SsoRoleChoice
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.UiText
import name.levis.ichor.ui.components.ErrorBox
import name.levis.ichor.ui.components.LoadingBox
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.design.IchorButton
import name.levis.ichor.ui.design.IchorFilterChip
import name.levis.ichor.ui.kubeauth.Waiting
import name.levis.ichor.ui.uiStateOf

/** Where the IAM Identity Center side of the account dialog is. */
private sealed interface SsoStep {
    data object Form : SsoStep
    data object Working : SsoStep
    data class Device(val start: AWSSignInStart) : SsoStep
    data class Roles(val finish: AWSSignInFinish) : SsoStep
    data class Failed(val message: UiText) : SsoStep
    data class Chosen(val choice: AWSSignInChoice, val check: UiState<AWSCheck>) : SsoStep
}

/**
 * The IAM Identity Center side of the account dialog, as `aws sso login`: the start URL and
 * regions, the device code to approve in the browser, the account and role to take, then the
 * same test as access keys. [onReady] gets the credential JSON to seal and its test once they
 * pass, null meanwhile. The tokens stay in that JSON, never on screen.
 */
@OptIn(ExperimentalLayoutApi::class)
@Composable
internal fun IdentityCenterSide(vm: AWSCreateViewModel, onReady: (credential: String?, check: AWSCheck?) -> Unit) {
    val scope = rememberCoroutineScope()
    var startUrl by remember { mutableStateOf("") }
    var ssoRegion by remember { mutableStateOf("") }
    var region by remember { mutableStateOf("") }
    var step by remember { mutableStateOf<SsoStep>(SsoStep.Form) }
    // The sheet closed or the dialog switched to keys: the wait for the browser ends.
    DisposableEffect(Unit) { onDispose { vm.cancelSignIn() } }

    fun choose(credential: String, role: SsoRoleChoice) {
        step = SsoStep.Working
        scope.launch {
            step = when (val chosen = uiStateOf { vm.signInChoose(credential, role) }) {
                is UiState.Loaded -> {
                    val check = uiStateOf { vm.test(chosen.data.credential) }
                    val tested = (check as? UiState.Loaded)?.data
                    if (tested != null) onReady(tested.credential.ifBlank { chosen.data.credential }, tested)
                    SsoStep.Chosen(chosen.data, check)
                }
                is UiState.Failed -> SsoStep.Failed(chosen.message)
                else -> SsoStep.Form
            }
        }
    }

    fun signIn() {
        onReady(null, null)
        step = SsoStep.Working
        val request = buildJsonObject {
            put("ssoStartUrl", startUrl.trim())
            put("ssoRegion", ssoRegion.trim())
            put("region", region.trim())
        }.toString()
        scope.launch {
            val start = uiStateOf { vm.signInStart(request) }
            if (start !is UiState.Loaded) {
                step = SsoStep.Failed((start as? UiState.Failed)?.message ?: UiText.Raw(""))
                return@launch
            }
            step = SsoStep.Device(start.data)
            val finish = uiStateOf { vm.signInFinish(start.data.credential) }
            // Cancelled meanwhile: the form is back already.
            if (step !is SsoStep.Device) return@launch
            when {
                finish is UiState.Loaded && finish.data.roles.size == 1 -> choose(finish.data.credential, finish.data.roles.first())
                finish is UiState.Loaded -> step = SsoStep.Roles(finish.data)
                else -> step = SsoStep.Failed((finish as? UiState.Failed)?.message ?: UiText.Raw(""))
            }
        }
    }

    MutedText(stringResource(R.string.aws_sso_hint))
    when (val current = step) {
        SsoStep.Form -> {
            KeyField(startUrl, R.string.kube_field_aws_sso_start_url, secret = false) { startUrl = it }
            KeyField(ssoRegion, R.string.kube_field_aws_sso_region, secret = false) { ssoRegion = it }
            KeyField(region, R.string.aws_sso_ec2_region, secret = false) { region = it }
            IchorButton(onClick = ::signIn, enabled = startUrl.isNotBlank() && ssoRegion.isNotBlank(), modifier = Modifier.fillMaxWidth()) {
                Text(stringResource(R.string.kube_signin_action))
            }
        }
        SsoStep.Working -> LoadingBox()
        is SsoStep.Device -> Waiting(current.start.prompt, onCancel = {
            vm.cancelSignIn()
            step = SsoStep.Form
        }, onCode = {})
        is SsoStep.Roles -> {
            Text(stringResource(R.string.kube_signin_aws_choose_role), style = MaterialTheme.typography.labelLarge)
            FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                current.finish.roles.forEach { role ->
                    IchorFilterChip(selected = false, onClick = { choose(current.finish.credential, role) }, label = { Text(role.label) })
                }
            }
        }
        is SsoStep.Failed -> ErrorBox(current.message, onRetry = { step = SsoStep.Form })
        is SsoStep.Chosen -> {
            Text(stringResource(R.string.aws_sso_signed_in, current.choice.arn), style = MaterialTheme.typography.bodySmall)
            when (val check = current.check) {
                is UiState.Loaded -> CheckLine(check.data)
                is UiState.Failed -> ErrorBox(check.message, onRetry = { step = SsoStep.Form })
                else -> Unit
            }
            MutedText(stringResource(R.string.aws_sso_session_note))
        }
    }
}
