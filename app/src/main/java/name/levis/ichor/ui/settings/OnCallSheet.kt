package name.levis.ichor.ui.settings

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Close
import name.levis.ichor.ui.design.IchorButton
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.InputChip
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
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.LiveRegionMode
import androidx.compose.ui.semantics.liveRegion
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.data.onCallRepository
import name.levis.ichor.model.OnCallConfig
import name.levis.ichor.model.OnCallIdentity
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.ui.userMessage

/** Where the Test button is. */
private sealed interface TestState {
    data object Idle : TestState
    data object Running : TestState
    data class Done(val who: OnCallIdentity) : TestState
    data class Failed(val message: String) : TestState
}

/**
 * The on-call integration of the cluster [fingerprint]: PagerDuty for now, its token (masked, never
 * read back into the field: left empty it keeps the stored one), the email it acts as, the region,
 * the services to watch, Test, and Delete to remove it from this device.
 */
@OptIn(ExperimentalMaterial3Api::class, ExperimentalLayoutApi::class)
@Composable
internal fun OnCallSheet(app: TalosApp, fingerprint: String, onDismiss: () -> Unit) {
    val scope = rememberCoroutineScope()
    var stored by remember { mutableStateOf(OnCallConfig()) }
    var token by remember { mutableStateOf("") }
    var from by remember { mutableStateOf("") }
    var region by remember { mutableStateOf(OnCallConfig.REGION_US) }
    var services by remember { mutableStateOf(emptyList<String>()) }
    var service by remember { mutableStateOf("") }
    var test by remember { mutableStateOf<TestState>(TestState.Idle) }

    LaunchedEffect(fingerprint) {
        val read = withContext(Dispatchers.IO) { app.onCallStore.read(fingerprint) }
        stored = read
        from = read.from
        region = read.region
        services = read.serviceIds
    }

    fun current() = OnCallConfig(token = token.trim().ifEmpty { stored.token }, from = from.trim(), serviceIds = services, region = region)
    fun addService() {
        val id = service.trim()
        if (id.isNotEmpty() && id !in services) services = services + id
        service = ""
    }

    ModalBottomSheet(onDismissRequest = onDismiss) {
        Column(
            Modifier.verticalScroll(rememberScrollState()).padding(horizontal = 20.dp).padding(bottom = 16.dp).navigationBarsPadding(),
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            Text(stringResource(R.string.oncall_title), style = MaterialTheme.typography.titleLarge)
            Text(stringResource(R.string.oncall_provider), style = MaterialTheme.typography.labelLarge)
            SingleChoiceSegmentedButtonRow(Modifier.fillMaxWidth()) {
                SegmentedButton(selected = true, onClick = {}, shape = SegmentedButtonDefaults.itemShape(0, 1)) {
                    Text(stringResource(R.string.oncall_pagerduty))
                }
            }
            OutlinedTextField(
                value = token,
                onValueChange = {
                    token = it
                    test = TestState.Idle
                },
                label = { Text(stringResource(R.string.oncall_token)) },
                supportingText = if (stored.token.isNotEmpty() && token.isEmpty()) ({ Text(stringResource(R.string.oncall_token_saved)) }) else null,
                singleLine = true,
                visualTransformation = PasswordVisualTransformation(),
                keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Password, autoCorrectEnabled = false),
                modifier = Modifier.fillMaxWidth(),
            )
            OutlinedTextField(
                value = from,
                onValueChange = { from = it },
                label = { Text(stringResource(R.string.oncall_from)) },
                supportingText = { Text(stringResource(R.string.oncall_from_hint)) },
                singleLine = true,
                keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Email, autoCorrectEnabled = false),
                modifier = Modifier.fillMaxWidth(),
            )
            Text(stringResource(R.string.oncall_region), style = MaterialTheme.typography.labelLarge)
            SingleChoiceSegmentedButtonRow(Modifier.fillMaxWidth()) {
                listOf(OnCallConfig.REGION_US to R.string.oncall_region_us, OnCallConfig.REGION_EU to R.string.oncall_region_eu).forEachIndexed { index, (value, label) ->
                    SegmentedButton(selected = region == value, onClick = { region = value }, shape = SegmentedButtonDefaults.itemShape(index, 2)) {
                        Text(stringResource(label))
                    }
                }
            }
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                OutlinedTextField(
                    value = service,
                    onValueChange = { service = it },
                    label = { Text(stringResource(R.string.oncall_service_ids)) },
                    supportingText = { Text(stringResource(R.string.oncall_service_hint)) },
                    singleLine = true,
                    keyboardOptions = KeyboardOptions(autoCorrectEnabled = false),
                    modifier = Modifier.weight(1f),
                )
                IchorTextButton(onClick = ::addService, enabled = service.isNotBlank()) { Text(stringResource(R.string.oncall_service_add)) }
            }
            FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                services.forEach { id ->
                    InputChip(
                        selected = false,
                        onClick = { services = services - id },
                        label = { Text(id) },
                        trailingIcon = {
                            Icon(Icons.Outlined.Close, stringResource(R.string.oncall_service_remove, id), Modifier.size(18.dp))
                        },
                    )
                }
            }
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                IchorOutlinedButton(
                    onClick = {
                        test = TestState.Running
                        scope.launch {
                            test = try {
                                TestState.Done(app.onCallRepository.test(current()))
                            } catch (e: CancellationException) {
                                throw e
                            } catch (e: Exception) {
                                TestState.Failed(e.userMessage())
                            }
                        }
                    },
                    enabled = current().token.isNotEmpty() && test != TestState.Running,
                ) {
                    Text(stringResource(if (test == TestState.Running) R.string.oncall_testing else R.string.oncall_test))
                }
                if (test == TestState.Running) CircularProgressIndicator(Modifier.size(20.dp), strokeWidth = 2.dp)
            }
            TestResult(test)
            Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(8.dp, Alignment.End)) {
                if (stored.token.isNotEmpty()) {
                    IchorTextButton(onClick = {
                        scope.launch {
                            withContext(Dispatchers.IO) { app.onCallStore.save(fingerprint, OnCallConfig()) }
                            onDismiss()
                        }
                    }) { Text(stringResource(R.string.common_delete)) }
                }
                IchorTextButton(onClick = onDismiss) { Text(stringResource(R.string.common_cancel)) }
                IchorButton(
                    onClick = {
                        val config = current()
                        scope.launch {
                            withContext(Dispatchers.IO) { app.onCallStore.save(fingerprint, config) }
                            onDismiss()
                        }
                    },
                ) { Text(stringResource(R.string.kb_save)) }
            }
        }
    }
}

/** The answer of the last Test, read out when it changes. */
@Composable
private fun TestResult(state: TestState) {
    val live = Modifier.semantics { liveRegion = LiveRegionMode.Polite }
    when (state) {
        TestState.Idle, TestState.Running -> Unit
        is TestState.Done -> Text(
            if (state.who.user.isEmpty()) stringResource(R.string.oncall_test_ok_account) else stringResource(R.string.oncall_test_ok_user, state.who.user),
            color = LocalStatusColors.current.ok,
            style = MaterialTheme.typography.bodyMedium,
            modifier = live,
        )
        is TestState.Failed -> Text(
            stringResource(R.string.oncall_test_failed, state.message),
            color = MaterialTheme.colorScheme.error,
            style = MaterialTheme.typography.bodyMedium,
            modifier = live,
        )
    }
}
