package name.levis.ichor.security

import android.content.Context
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Key
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
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
import androidx.compose.ui.semantics.LiveRegionMode
import androidx.compose.ui.semantics.liveRegion
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import com.yubico.yubikit.fido.client.ClientError
import com.yubico.yubikit.fido.client.PinRequiredClientError
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import name.levis.ichor.R
import name.levis.ichor.TalosApp

/** One "tap your security key" prompt: what for, and whether the fingerprint may be used instead. */
class SecurityKeyPromptRequest(val title: String, val subtitle: String?, val allowBiometric: Boolean) {
    private val result = CompletableDeferred<AuthResult>()

    fun resolve(value: AuthResult) {
        result.complete(value)
    }

    internal suspend fun await(): AuthResult = result.await()
}

/**
 * The prompts [authenticate] shows when a security key is enrolled: a request is posted here and
 * [SecurityKeyPromptHost], composed above everything in the activity, shows it and resolves it.
 */
class SecurityKeyPrompts {
    private val _request = MutableStateFlow<SecurityKeyPromptRequest?>(null)
    val request: StateFlow<SecurityKeyPromptRequest?> = _request.asStateFlow()

    suspend fun request(title: String, subtitle: String?, allowBiometric: Boolean): AuthResult {
        val request = SecurityKeyPromptRequest(title, subtitle, allowBiometric)
        // One prompt at a time: a check started while another waits replaces it.
        _request.value?.resolve(AuthResult.Failure("Cancelled"))
        _request.value = request
        try {
            return request.await()
        } finally {
            if (_request.value === request) _request.value = null
        }
    }
}

/** Shows the pending security-key prompt, if any (see [SecurityKeyPrompts]). */
@Composable
fun SecurityKeyPromptHost(app: TalosApp) {
    val request by app.keyPrompts.request.collectAsStateWithLifecycle()
    request?.let { SecurityKeyPromptDialog(app, it) }
}

/**
 * Waits for a key, checks its assertion and resolves [request]. In the required mode the first
 * successful tap of the process also unwraps the data key the stored configs are sealed with.
 */
@Composable
private fun SecurityKeyPromptDialog(app: TalosApp, request: SecurityKeyPromptRequest) {
    val context = LocalContext.current
    val activity = context.findFragmentActivity()
    val scope = rememberCoroutineScope()
    val enrolment by app.appLock.securityKeys.collectAsStateWithLifecycle()
    var touching by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<String?>(null) }
    var attempt by remember { mutableIntStateOf(0) }
    val nfc = remember(activity) { activity?.let { SecurityKeyClient(it).nfc } ?: SecurityKeyClient.Nfc.NONE }

    fun cancel() = request.resolve(AuthResult.Failure(context.getString(R.string.security_key_cancelled)))

    LaunchedEffect(request, attempt) {
        val keys = enrolment
        if (keys == null || activity == null) return@LaunchedEffect cancel()
        error = null
        touching = false
        val client = SecurityKeyClient(activity)
        try {
            client.withKey { device ->
                touching = true
                val assertion = client.assert(device, keys, wantSecret = keys.required)
                if (keys.required && app.appLock.dek() == null) app.appLock.provideDek(keys.unwrapDek(assertion))
            }
            request.resolve(AuthResult.Success)
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            touching = false
            error = securityKeyMessage(context, e)
        }
    }

    AlertDialog(
        onDismissRequest = ::cancel,
        icon = { Icon(Icons.Outlined.Key, contentDescription = null, modifier = Modifier.size(32.dp)) },
        title = { Text(request.title, textAlign = TextAlign.Center) },
        text = {
            Column(horizontalAlignment = Alignment.CenterHorizontally) {
                request.subtitle?.let {
                    Text(it, textAlign = TextAlign.Center)
                    Spacer(Modifier.height(8.dp))
                }
                Text(
                    stringResource(
                        when (nfc) {
                            SecurityKeyClient.Nfc.READY -> R.string.security_key_prompt_hint_nfc
                            SecurityKeyClient.Nfc.OFF -> R.string.security_key_prompt_hint_nfc_off
                            SecurityKeyClient.Nfc.NONE -> R.string.security_key_prompt_hint_no_nfc
                        },
                    ),
                    textAlign = TextAlign.Center,
                )
                Spacer(Modifier.height(16.dp))
                when {
                    error != null -> Text(
                        error.orEmpty(),
                        color = MaterialTheme.colorScheme.error,
                        textAlign = TextAlign.Center,
                        modifier = Modifier.semantics { liveRegion = LiveRegionMode.Polite },
                    )
                    touching -> Row(verticalAlignment = Alignment.CenterVertically) {
                        CircularProgressIndicator(Modifier.size(18.dp), strokeWidth = 2.dp)
                        Text(stringResource(R.string.security_key_touch), modifier = Modifier.padding(start = 12.dp))
                    }
                    else -> CircularProgressIndicator(Modifier.size(24.dp), strokeWidth = 2.dp)
                }
            }
        },
        confirmButton = {
            if (error != null) {
                TextButton(onClick = { attempt++ }) { Text(stringResource(R.string.common_retry)) }
            } else if (request.allowBiometric && activity != null) {
                TextButton(onClick = {
                    scope.launch { request.resolve(biometricPrompt(activity, request.title, request.subtitle)) }
                }) { Text(stringResource(R.string.security_key_use_biometric)) }
            }
        },
        dismissButton = {
            if (error != null && request.allowBiometric && activity != null) {
                TextButton(onClick = {
                    scope.launch { request.resolve(biometricPrompt(activity, request.title, request.subtitle)) }
                }) { Text(stringResource(R.string.security_key_use_biometric)) }
            } else {
                TextButton(onClick = ::cancel) { Text(stringResource(R.string.common_cancel)) }
            }
        },
    )
}

/** What went wrong with a security key, as shown to the user. */
fun securityKeyMessage(context: Context, e: Throwable): String = when (e) {
    is SecurityKeyUnwrapException -> context.getString(
        when (e.reason) {
            SecurityKeyUnwrapException.Reason.NO_SECRET -> R.string.security_key_error_no_secret
            SecurityKeyUnwrapException.Reason.NOT_WRAPPED -> R.string.security_key_error_not_wrapped
            SecurityKeyUnwrapException.Reason.UV_CHANGED -> R.string.security_key_error_uv_changed
            SecurityKeyUnwrapException.Reason.WRONG_KEY, SecurityKeyUnwrapException.Reason.DAMAGED -> R.string.security_key_error_wrong_key
        },
    )
    is SecurityKeyNotEnrolledException -> context.getString(R.string.security_key_error_not_enrolled)
    is SecurityKeyBadSignatureException -> context.getString(R.string.security_key_error_wrong_key)
    is PinRequiredClientError -> context.getString(R.string.security_key_error_pin_required)
    is ClientError -> when (e.errorCode) {
        // The key has no credential of ours (another key, or a reset one).
        ClientError.Code.DEVICE_INELIGIBLE -> context.getString(R.string.security_key_error_not_enrolled)
        ClientError.Code.TIMEOUT -> context.getString(R.string.security_key_error_timeout)
        else -> context.getString(R.string.security_key_error_io, e.message ?: e.errorCode.toString())
    }
    else -> context.getString(R.string.security_key_error_io, e.message ?: e.javaClass.simpleName)
}
