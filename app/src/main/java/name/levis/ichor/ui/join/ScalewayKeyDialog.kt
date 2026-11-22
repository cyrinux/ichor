package name.levis.ichor.ui.join

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
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
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.launch
import name.levis.ichor.R
import name.levis.ichor.data.TalosJson
import name.levis.ichor.model.ScalewayCheck
import name.levis.ichor.model.ScalewayCredential
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.components.ErrorBox
import name.levis.ichor.ui.components.LoadingBox
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.copyToClipboard
import name.levis.ichor.ui.design.IchorAlertDialog
import name.levis.ichor.ui.design.IchorButton
import name.levis.ichor.ui.design.IchorOutlinedButton
import name.levis.ichor.ui.design.IchorTextButton
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.ui.uiStateOf

/** A label, the API key (the secret masked), the IAM policy to attach, a test, then sealed. */
@Composable
internal fun ScalewayKeyDialog(vm: ScalewayCreateViewModel, onDismiss: () -> Unit) {
    val colors = LocalStatusColors.current
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    var label by remember { mutableStateOf("") }
    var accessKey by remember { mutableStateOf("") }
    var secret by remember { mutableStateOf("") }
    var project by remember { mutableStateOf("") }
    var check by remember { mutableStateOf<UiState<ScalewayCheck>?>(null) }
    var saveFailed by remember { mutableStateOf(false) }
    val tested = (check as? UiState.Loaded)?.data
    val policyTitle = stringResource(R.string.ovh_copy_policy)

    fun credential() = TalosJson.encodeToString(
        ScalewayCredential.serializer(),
        ScalewayCredential(accessKey.trim(), secret.trim(), project.trim(), tested?.zone.orEmpty()),
    )

    fun runTest() {
        check = UiState.Loading
        scope.launch {
            val state = uiStateOf { vm.test(credential()) }
            (state as? UiState.Loaded)?.let { project = it.data.project }
            check = state
        }
    }

    IchorAlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(stringResource(R.string.scaleway_add_key)) },
        text = {
            Column(Modifier.verticalScroll(rememberScrollState()), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                Text(stringResource(R.string.scaleway_key_warning), color = colors.warn, style = MaterialTheme.typography.bodySmall)
                MutedText(stringResource(R.string.scaleway_policy_hint))
                IchorOutlinedButton(onClick = { scope.launch { copyToClipboard(context, policyTitle, vm.policy(project)) } }) { Text(policyTitle) }
                OutlinedTextField(
                    value = label,
                    onValueChange = { label = it },
                    label = { Text(stringResource(R.string.hetzner_project_label)) },
                    singleLine = true,
                    modifier = Modifier.fillMaxWidth(),
                )
                OutlinedTextField(
                    value = accessKey,
                    onValueChange = {
                        accessKey = it
                        check = null
                    },
                    label = { Text(stringResource(R.string.scaleway_access_key)) },
                    singleLine = true,
                    keyboardOptions = KeyboardOptions(autoCorrectEnabled = false),
                    modifier = Modifier.fillMaxWidth(),
                )
                OutlinedTextField(
                    value = secret,
                    onValueChange = {
                        secret = it
                        check = null
                    },
                    label = { Text(stringResource(R.string.scaleway_secret_key)) },
                    singleLine = true,
                    visualTransformation = PasswordVisualTransformation(),
                    keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Password, autoCorrectEnabled = false),
                    modifier = Modifier.fillMaxWidth(),
                )
                OutlinedTextField(
                    value = project,
                    onValueChange = {
                        project = it
                        check = null
                    },
                    label = { Text(stringResource(R.string.scaleway_project)) },
                    supportingText = { Text(stringResource(R.string.scaleway_project_hint)) },
                    singleLine = true,
                    keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Ascii, autoCorrectEnabled = false),
                    modifier = Modifier.fillMaxWidth(),
                )
                IchorOutlinedButton(onClick = ::runTest, enabled = accessKey.isNotBlank() && secret.isNotBlank() && check != UiState.Loading) {
                    Text(stringResource(R.string.hetzner_test))
                }
                when (val state = check) {
                    null -> Unit
                    UiState.Loading -> LoadingBox()
                    is UiState.Failed -> ErrorBox(state.message, ::runTest)
                    is UiState.Loaded -> {
                        val data = state.data
                        Text(
                            when {
                                !data.checked -> stringResource(R.string.scaleway_check_unknown, data.project)
                                data.canCreate -> stringResource(R.string.scaleway_check_ok, data.project)
                                else -> stringResource(R.string.scaleway_check_no)
                            },
                            color = if (data.checked && !data.canCreate) colors.warn else colors.ok,
                            style = MaterialTheme.typography.bodySmall,
                        )
                    }
                }
                if (saveFailed) Text(stringResource(R.string.hetzner_token_not_saved), color = colors.bad, style = MaterialTheme.typography.bodySmall)
            }
        },
        confirmButton = {
            IchorButton(
                onClick = {
                    scope.launch {
                        if (vm.save(label.trim(), credential())) onDismiss() else saveFailed = true
                    }
                },
                enabled = label.isNotBlank() && tested != null && (tested.canCreate || !tested.checked),
            ) { Text(stringResource(R.string.hetzner_save)) }
        },
        dismissButton = { IchorTextButton(onClick = onDismiss) { Text(stringResource(R.string.common_cancel)) } },
    )
}
