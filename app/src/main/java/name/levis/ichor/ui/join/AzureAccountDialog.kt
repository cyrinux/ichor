package name.levis.ichor.ui.join

import androidx.annotation.StringRes
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.text.input.VisualTransformation
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.launch
import name.levis.ichor.R
import name.levis.ichor.data.TalosJson
import name.levis.ichor.model.AzureCheck
import name.levis.ichor.model.AzureCredential
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.components.ErrorBox
import name.levis.ichor.ui.components.LoadingBox
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.copyToClipboard
import name.levis.ichor.ui.design.IchorAlertDialog
import name.levis.ichor.ui.design.IchorButton
import name.levis.ichor.ui.design.IchorFilterChip
import name.levis.ichor.ui.design.IchorOutlinedButton
import name.levis.ichor.ui.design.IchorTextButton
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.ui.uiStateOf

/**
 * The principal's ids, its secret (masked), its cloud and resource group, or a Microsoft-account
 * sign-in; the role to give it, a test, then sealed under the subscription id.
 */
@OptIn(ExperimentalLayoutApi::class)
@Composable
internal fun PrincipalDialog(vm: AzureCreateViewModel, onDismiss: () -> Unit) {
    val colors = LocalStatusColors.current
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    var microsoftAccount by remember { mutableStateOf(false) }
    var accountReady by remember { mutableStateOf<Pair<String, AzureCheck>?>(null) }
    var tenantId by remember { mutableStateOf("") }
    var clientId by remember { mutableStateOf("") }
    var secret by remember { mutableStateOf("") }
    var subscriptionId by remember { mutableStateOf("") }
    var resourceGroup by remember { mutableStateOf("") }
    var cloud by remember { mutableStateOf(AzureCloud.PUBLIC) }
    var check by remember { mutableStateOf<UiState<AzureCheck>?>(null) }
    var saveFailed by remember { mutableStateOf(false) }
    val tested = (check as? UiState.Loaded)?.data
    val roleTitle = stringResource(R.string.azure_copy_role)

    fun credential() = TalosJson.encodeToString(
        AzureCredential.serializer(),
        AzureCredential(tenantId.trim(), clientId.trim(), secret.trim(), subscriptionId.trim(), cloud.environment, resourceGroup.trim()),
    )

    fun runTest() {
        check = UiState.Loading
        scope.launch { check = uiStateOf { vm.test(credential()) } }
    }

    IchorAlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(stringResource(R.string.azure_add_account)) },
        text = {
            Column(Modifier.verticalScroll(rememberScrollState()), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    listOf(false to R.string.azure_signin_option_principal, true to R.string.azure_signin_option_account).forEach { (account, label) ->
                        IchorFilterChip(
                            selected = microsoftAccount == account,
                            onClick = {
                                microsoftAccount = account
                                accountReady = null
                            },
                            label = { Text(stringResource(label)) },
                        )
                    }
                }
                MutedText(stringResource(R.string.azure_role_hint))
                IchorOutlinedButton(onClick = { scope.launch { copyToClipboard(context, roleTitle, vm.role(subscriptionId.trim(), resourceGroup.trim())) } }) {
                    Text(roleTitle)
                }
                if (microsoftAccount) {
                    MicrosoftAccountSide(vm) { credential, check -> accountReady = if (credential != null && check != null) credential to check else null }
                    if (saveFailed) NotSaved()
                    return@Column
                }
                Text(stringResource(R.string.azure_warning), color = colors.warn, style = MaterialTheme.typography.bodySmall)
                Text(stringResource(R.string.azure_cloud), style = MaterialTheme.typography.labelLarge)
                FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    AzureCloud.entries.forEach { c ->
                        IchorFilterChip(
                            selected = c == cloud,
                            onClick = {
                                cloud = c
                                check = null
                            },
                            label = { Text(c.label) },
                        )
                    }
                }
                IdField(tenantId, R.string.azure_tenant_id, secret = false) {
                    tenantId = it
                    check = null
                }
                IdField(clientId, R.string.azure_client_id, secret = false) {
                    clientId = it
                    check = null
                }
                IdField(secret, R.string.azure_client_secret, secret = true) {
                    secret = it
                    check = null
                }
                IdField(subscriptionId, R.string.azure_subscription_id, secret = false) {
                    subscriptionId = it
                    check = null
                }
                IdField(resourceGroup, R.string.azure_resource_group, secret = false) {
                    resourceGroup = it
                    check = null
                }
                MutedText(stringResource(R.string.azure_resource_group_hint))
                IchorOutlinedButton(
                    onClick = ::runTest,
                    enabled = tenantId.isNotBlank() && clientId.isNotBlank() && secret.isNotBlank() && subscriptionId.isNotBlank() && check != UiState.Loading,
                ) {
                    Text(stringResource(R.string.hetzner_test))
                }
                when (val state = check) {
                    null -> Unit
                    UiState.Loading -> LoadingBox()
                    is UiState.Failed -> ErrorBox(state.message, ::runTest)
                    is UiState.Loaded -> AzureCheckLine(state.data, resourceGroup.trim())
                }
                if (saveFailed) NotSaved()
            }
        },
        confirmButton = {
            // What the test passed: the principal, or the sign-in (renewed by the core when it says so).
            val ready = if (microsoftAccount) accountReady else tested?.let { credential() to it }
            val readyCheck = ready?.second
            IchorButton(
                onClick = {
                    val (json, passed) = ready ?: return@IchorButton
                    scope.launch {
                        if (vm.save(passed.subscription, json)) onDismiss() else saveFailed = true
                    }
                },
                enabled = readyCheck != null && readyCheck.subscription.isNotBlank() && (readyCheck.canCreate || !readyCheck.checked),
            ) { Text(stringResource(R.string.hetzner_save)) }
        },
        dismissButton = { IchorTextButton(onClick = onDismiss) { Text(stringResource(R.string.common_cancel)) } },
    )
}

/** The store could not seal the account. */
@Composable
private fun NotSaved() {
    Text(stringResource(R.string.hetzner_token_not_saved), color = LocalStatusColors.current.bad, style = MaterialTheme.typography.bodySmall)
}

/** One field of the principal dialog; the secret is masked and never suggested. */
@Composable
internal fun IdField(value: String, @StringRes label: Int, secret: Boolean, onChange: (String) -> Unit) {
    OutlinedTextField(
        value = value,
        onValueChange = onChange,
        label = { Text(stringResource(label)) },
        singleLine = true,
        visualTransformation = if (secret) PasswordVisualTransformation() else VisualTransformation.None,
        keyboardOptions = KeyboardOptions(keyboardType = if (secret) KeyboardType.Password else KeyboardType.Ascii, autoCorrectEnabled = false),
        modifier = Modifier.fillMaxWidth(),
    )
}
