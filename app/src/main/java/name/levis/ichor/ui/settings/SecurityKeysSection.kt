package name.levis.ichor.ui.settings

import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Delete
import androidx.compose.material.icons.outlined.Key
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Switch
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
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import com.yubico.yubikit.fido.client.AuthInvalidClientError
import com.yubico.yubikit.fido.client.ClientError
import com.yubico.yubikit.fido.client.PinRequiredClientError
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.launch
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.security.AuthResult
import name.levis.ichor.security.DekWrap
import name.levis.ichor.security.EnrolledKey
import name.levis.ichor.security.FidoPin
import name.levis.ichor.security.SECURITY_KEY_MAX
import name.levis.ichor.security.SecurityKeyClient
import name.levis.ichor.security.SecurityKeyEnrolment
import name.levis.ichor.security.SecurityKeyMode
import name.levis.ichor.security.authenticate
import name.levis.ichor.security.findFragmentActivity
import name.levis.ichor.security.securityKeyMessage
import name.levis.ichor.security.wrapping
import name.levis.ichor.ui.components.MutedText
import java.text.DateFormat
import java.util.Date

/**
 * The enrolled security keys (Settings → Security, see SecurityKeys.kt): list, add one (a spare
 * too), remove one, and the "Require the key" mode that seals the stored configs with them.
 * Each change is checked first, like the other lock settings ([authThen]).
 */
@Composable
fun SecurityKeysRows(app: TalosApp, authThen: (title: String, action: () -> Unit) -> Unit, onError: (String?) -> Unit) {
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    val enrolment by app.appLock.securityKeys.collectAsStateWithLifecycle()
    val keys = enrolment?.keys.orEmpty()
    var flow by remember { mutableStateOf<KeyFlow?>(null) }
    var releasing by remember { mutableStateOf(false) }

    /** Turns the requirement off: a tap first (the prompt asks for it), then the files are unsealed. */
    fun release() {
        val current = enrolment ?: return
        val activity = context.findFragmentActivity() ?: return
        releasing = true
        scope.launch {
            try {
                when (val result = authenticate(activity, context.getString(R.string.security_key_require))) {
                    is AuthResult.Failure -> onError(result.message)
                    AuthResult.Success -> {
                        onError(null)
                        // Unsealed while the data key is still held; a failure puts the requirement back.
                        app.appLock.setSecurityKeys(current.copy(mode = SecurityKeyMode.UNLOCK, keys = current.keys.map { it.copy(wrappedDek = null) }))
                        try {
                            app.resealCredentialStores()
                            app.appLock.clearDek()
                        } catch (e: Exception) {
                            app.appLock.setSecurityKeys(current)
                            onError(securityKeyMessage(context, e))
                        }
                    }
                }
            } finally {
                releasing = false
            }
        }
    }

    Column(Modifier.padding(start = 16.dp, end = 16.dp, bottom = 16.dp)) {
        Text(stringResource(R.string.security_key_section_title), style = MaterialTheme.typography.titleSmall)
        MutedText(stringResource(R.string.security_key_section_desc))
        keys.forEach { key ->
            Row(Modifier.fillMaxWidth().padding(top = 8.dp), verticalAlignment = Alignment.CenterVertically) {
                Icon(Icons.Outlined.Key, contentDescription = null, tint = MaterialTheme.colorScheme.primary)
                Column(Modifier.weight(1f).padding(start = 12.dp)) {
                    Text(key.label, style = MaterialTheme.typography.bodyMedium)
                    MutedText(stringResource(R.string.security_key_enrolled_on, DateFormat.getDateInstance(DateFormat.MEDIUM).format(Date(key.enrolledAt))))
                }
                IconButton(onClick = {
                    val current = enrolment ?: return@IconButton
                    if (current.required && current.keys.size == 1) {
                        onError(context.getString(R.string.security_key_remove_last_required))
                        return@IconButton
                    }
                    authThen(context.getString(R.string.security_key_remove, key.label)) {
                        app.appLock.setSecurityKeys(current.without(key.credentialId))
                    }
                }) {
                    Icon(Icons.Outlined.Delete, contentDescription = stringResource(R.string.security_key_remove, key.label))
                }
            }
        }
        if (keys.size < SECURITY_KEY_MAX) {
            Spacer(Modifier.height(8.dp))
            OutlinedButton(
                onClick = { authThen(context.getString(R.string.security_key_add)) { flow = KeyFlow.Enrol } },
                modifier = Modifier.fillMaxWidth(),
            ) { Text(stringResource(if (keys.isEmpty()) R.string.security_key_add else R.string.security_key_add_spare)) }
        }
        if (keys.isNotEmpty()) {
            Row(Modifier.fillMaxWidth().padding(top = 8.dp), verticalAlignment = Alignment.CenterVertically) {
                Column(Modifier.weight(1f)) {
                    Text(stringResource(R.string.security_key_require), style = MaterialTheme.typography.titleSmall)
                    MutedText(stringResource(R.string.security_key_require_desc))
                }
                Switch(
                    checked = enrolment?.required == true,
                    onCheckedChange = { on ->
                        if (on) authThen(context.getString(R.string.security_key_require)) { flow = KeyFlow.Require }
                        else release()
                    },
                    enabled = !releasing,
                    modifier = Modifier.padding(start = 12.dp),
                )
            }
        }
    }

    flow?.let { current ->
        SecurityKeyFlowDialog(app, current, onDone = { flow = null })
    }
}

/** A multi-tap operation on the keys, run in [SecurityKeyFlowDialog]. */
enum class KeyFlow { Enrol, Require }

/** What a flow tells the dialog while it runs. */
private class FlowUi(
    val step: (String) -> Unit,
    val askPin: suspend () -> CharArray,
)

/**
 * Walks the user through the taps a [flow] needs (instruction, progress, errors, the key's FIDO
 * PIN when a key insists on it for new credentials) and applies the result to the app lock.
 */
@Composable
private fun SecurityKeyFlowDialog(app: TalosApp, flow: KeyFlow, onDone: () -> Unit) {
    val context = LocalContext.current
    val activity = context.findFragmentActivity()
    var instruction by remember { mutableStateOf("") }
    var error by remember { mutableStateOf<String?>(null) }
    var attempt by remember { mutableIntStateOf(0) }
    var done by remember { mutableStateOf(false) }
    var pinRequest by remember { mutableStateOf<CompletableDeferred<CharArray>?>(null) }
    var pin by remember { mutableStateOf("") }

    fun submitPin() {
        pinRequest?.complete(pin.toCharArray())
        pin = ""
    }

    LaunchedEffect(flow, attempt) {
        if (activity == null) return@LaunchedEffect onDone()
        error = null
        val client = SecurityKeyClient(activity)
        val ui = FlowUi(
            step = { instruction = it },
            askPin = {
                val request = CompletableDeferred<CharArray>()
                pinRequest = request
                try {
                    request.await()
                } finally {
                    pinRequest = null
                }
            },
        )
        try {
            when (flow) {
                KeyFlow.Enrol -> enrolFlow(app, client, context, ui)
                KeyFlow.Require -> requireFlow(app, client, context, ui)
            }
            done = true
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            error = if (e is ClientError && e.errorCode == ClientError.Code.DEVICE_INELIGIBLE && flow == KeyFlow.Enrol) {
                context.getString(R.string.security_key_already_enrolled)
            } else {
                securityKeyMessage(context, e)
            }
        }
    }

    AlertDialog(
        onDismissRequest = { if (done || error != null) onDone() },
        icon = { Icon(Icons.Outlined.Key, contentDescription = null, modifier = Modifier.size(32.dp)) },
        title = { Text(stringResource(if (flow == KeyFlow.Enrol) R.string.security_key_flow_enrol_title else R.string.security_key_flow_require_title), textAlign = TextAlign.Center) },
        text = {
            Column(horizontalAlignment = Alignment.CenterHorizontally) {
                when {
                    done -> Text(stringResource(R.string.security_key_flow_done), textAlign = TextAlign.Center)
                    error != null -> Text(error.orEmpty(), color = MaterialTheme.colorScheme.error, textAlign = TextAlign.Center)
                    pinRequest != null -> {
                        Text(stringResource(R.string.security_key_flow_pin_desc), textAlign = TextAlign.Center)
                        Spacer(Modifier.height(12.dp))
                        // A FIDO PIN is any text, letters included: the full keyboard, not digits only.
                        OutlinedTextField(
                            value = pin,
                            onValueChange = { pin = FidoPin.clean(it) },
                            label = { Text(stringResource(R.string.security_key_flow_pin_label)) },
                            singleLine = true,
                            visualTransformation = PasswordVisualTransformation(),
                            keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Password, autoCorrectEnabled = false, imeAction = ImeAction.Done),
                            keyboardActions = KeyboardActions(onDone = { if (FidoPin.valid(pin)) submitPin() }),
                        )
                    }
                    else -> {
                        Text(instruction, textAlign = TextAlign.Center)
                        Spacer(Modifier.height(16.dp))
                        CircularProgressIndicator(Modifier.size(24.dp), strokeWidth = 2.dp)
                    }
                }
            }
        },
        confirmButton = {
            when {
                done -> TextButton(onClick = onDone) { Text(stringResource(R.string.common_ok)) }
                error != null -> TextButton(onClick = { attempt++ }) { Text(stringResource(R.string.common_retry)) }
                pinRequest != null -> TextButton(onClick = ::submitPin, enabled = FidoPin.valid(pin)) {
                    Text(stringResource(R.string.common_ok))
                }
            }
        },
        dismissButton = {
            if (!done) TextButton(onClick = onDone) { Text(stringResource(R.string.common_cancel)) }
        },
    )
}

/**
 * Adds a key: one tap makes the credential (the key's PIN if it insists, wrong PIN retried);
 * with the requirement on, a second tap of the same key wraps the data key for it.
 */
private suspend fun enrolFlow(app: TalosApp, client: SecurityKeyClient, context: android.content.Context, ui: FlowUi) {
    val lock = app.appLock
    val enrolment = lock.securityKeys.value ?: SecurityKeyEnrolment.create()
    val label = context.getString(if (enrolment.keys.isEmpty()) R.string.security_key_label else R.string.security_key_label_spare)
    ui.step(context.getString(R.string.security_key_flow_tap_new))
    var pin: CharArray? = null
    var key: EnrolledKey? = null
    while (key == null) {
        try {
            key = client.withKey { device -> client.enrol(device, enrolment, label, pin) }
        } catch (_: PinRequiredClientError) {
            pin = ui.askPin()
            ui.step(context.getString(R.string.security_key_flow_tap_new))
        } catch (e: AuthInvalidClientError) {
            if (e.authType != AuthInvalidClientError.AuthType.PIN) throw e
            ui.step(context.getString(R.string.security_key_flow_pin_wrong, e.retries))
            pin = ui.askPin()
            ui.step(context.getString(R.string.security_key_flow_tap_new))
        }
    }
    pin?.fill('\u0000')
    var added: EnrolledKey = checkNotNull(key)
    if (enrolment.required) {
        val dek = checkNotNull(lock.dek()) { "no data key held while the key is required" }
        val withNew = enrolment.withKey(added)
        while (true) {
            ui.step(context.getString(R.string.security_key_flow_tap_again))
            val assertion = client.withKey { device -> client.assert(device, withNew, wantSecret = true) }
            if (assertion.key.credentialId == added.credentialId) {
                added = added.wrapping(dek, assertion)
                break
            }
            ui.step(context.getString(R.string.security_key_flow_wrong_key, assertion.key.label, added.label))
        }
    }
    lock.setSecurityKeys(enrolment.withKey(added))
}

/**
 * Turns the requirement on: a new data key, wrapped for each enrolled key by a tap of it, then
 * the stored configs are sealed with it. A failure while sealing puts everything back.
 */
private suspend fun requireFlow(app: TalosApp, client: SecurityKeyClient, context: android.content.Context, ui: FlowUi) {
    val lock = app.appLock
    val enrolment = lock.securityKeys.value ?: return
    if (enrolment.required) return
    val dek = DekWrap.newDek()
    var updated = enrolment
    enrolment.keys.forEachIndexed { index, key ->
        ui.step(context.getString(R.string.security_key_flow_tap_nth, index + 1, enrolment.keys.size, key.label))
        while (true) {
            val assertion = client.withKey { device -> client.assert(device, enrolment, wantSecret = true) }
            if (assertion.key.credentialId == key.credentialId) {
                updated = updated.withKey(key.wrapping(dek, assertion))
                break
            }
            ui.step(context.getString(R.string.security_key_flow_wrong_key, assertion.key.label, key.label))
        }
    }
    ui.step(context.getString(R.string.security_key_flow_sealing))
    lock.provideDek(dek)
    lock.setSecurityKeys(updated.copy(mode = SecurityKeyMode.REQUIRED))
    try {
        app.resealCredentialStores()
    } catch (e: Exception) {
        // Back to Keystore-only files while the data key is still held, then forget it.
        lock.setSecurityKeys(enrolment)
        runCatching { app.resealCredentialStores() }
        lock.clearDek()
        throw e
    }
}
