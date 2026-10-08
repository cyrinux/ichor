package name.levis.ichor.security

import android.app.Activity
import android.nfc.NfcAdapter
import com.yubico.yubikit.android.YubiKitManager
import com.yubico.yubikit.android.transport.nfc.NfcConfiguration
import com.yubico.yubikit.android.transport.nfc.NfcNotAvailable
import com.yubico.yubikit.android.transport.usb.UsbConfiguration
import com.yubico.yubikit.core.YubiKeyConnection
import com.yubico.yubikit.core.YubiKeyDevice
import com.yubico.yubikit.core.application.CommandState
import com.yubico.yubikit.core.fido.FidoConnection
import com.yubico.yubikit.core.smartcard.SmartCardConnection
import com.yubico.yubikit.core.util.Callback
import com.yubico.yubikit.fido.Cose
import com.yubico.yubikit.fido.client.WebAuthnClient
import com.yubico.yubikit.fido.client.clientdata.ClientDataProvider
import com.yubico.yubikit.fido.webauthn.AttestationConveyancePreference
import com.yubico.yubikit.fido.webauthn.AuthenticatorAssertionResponse
import com.yubico.yubikit.fido.webauthn.AuthenticatorAttestationResponse
import com.yubico.yubikit.fido.webauthn.AuthenticatorSelectionCriteria
import com.yubico.yubikit.fido.webauthn.Extensions
import com.yubico.yubikit.fido.webauthn.PublicKeyCredentialCreationOptions
import com.yubico.yubikit.fido.webauthn.PublicKeyCredentialDescriptor
import com.yubico.yubikit.fido.webauthn.PublicKeyCredentialParameters
import com.yubico.yubikit.fido.webauthn.PublicKeyCredentialRequestOptions
import com.yubico.yubikit.fido.webauthn.PublicKeyCredentialRpEntity
import com.yubico.yubikit.fido.webauthn.PublicKeyCredentialType
import com.yubico.yubikit.fido.webauthn.PublicKeyCredentialUserEntity
import com.yubico.yubikit.fido.webauthn.ResidentKeyRequirement
import com.yubico.yubikit.fido.webauthn.SerializationType
import com.yubico.yubikit.fido.webauthn.UserVerificationRequirement
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.NonCancellable
import kotlinx.coroutines.job
import kotlinx.coroutines.suspendCancellableCoroutine
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.withContext
import java.security.SecureRandom
import java.util.Base64
import kotlin.coroutines.resume

/** The assertion came from a credential that is not enrolled (another key, or one removed). */
class SecurityKeyNotEnrolledException : Exception("This security key is not enrolled")

/** The key answered, but its signature did not check out. */
class SecurityKeyBadSignatureException(reason: String?) : Exception("Assertion rejected: $reason")

/**
 * Talks to a security key with Yubico's SDK: waits for one over NFC (tapped on the phone) or
 * USB (plugged in, or already there), then makes or asserts the app's FIDO2 credential on it.
 * Everything about what is stored and checked is in SecurityKeys.kt; this is the transport.
 *
 * One instance per prompt, made with the activity the NFC reader mode is enabled on: it must be
 * resumed while a key is awaited.
 */
class SecurityKeyClient(private val activity: Activity) {
    /** Whether a tap can be read on this phone, for the prompt's hint. */
    enum class Nfc { READY, OFF, NONE }

    val nfc: Nfc = NfcAdapter.getDefaultAdapter(activity)?.let { if (it.isEnabled) Nfc.READY else Nfc.OFF } ?: Nfc.NONE

    private val manager = YubiKitManager(activity.applicationContext)
    private val random = SecureRandom()

    /**
     * Runs [block] with the next key tapped or plugged in. Discovery (NFC reader mode and USB)
     * is on only until [block] returns: an NFC key is only usable while it stays on the phone.
     *
     * A command cannot be interrupted once sent: a prompt cancelled mid-exchange leaves its
     * thread blocked until the key answers or [NFC_TIMEOUT_MS] passes, and the NFC service is
     * busy meanwhile. Turning the reader mode on again from the main thread then blocks it (an
     * ANR), so a new prompt first waits, suspended, for the previous command to end.
     */
    suspend fun <T> withKey(block: suspend (YubiKeyDevice) -> T): T {
        busy.withLock { }
        val device = withContext(Dispatchers.Main.immediate) {
            suspendCancellableCoroutine<YubiKeyDevice> { cont ->
                val deliver = Callback<YubiKeyDevice> { device -> if (cont.isActive) cont.resume(device) }
                try {
                    manager.startNfcDiscovery(NfcConfiguration().timeout(NFC_TIMEOUT_MS), activity, deliver)
                    nfcOwner = this@SecurityKeyClient
                } catch (_: NfcNotAvailable) {
                    // No NFC, or off: USB only.
                }
                manager.startUsbDiscovery(UsbConfiguration(), deliver)
                cont.invokeOnCancellation { stopDiscovery() }
            }
        }
        try {
            return busy.withLock { block(device) }
        } finally {
            withContext(NonCancellable + Dispatchers.Main.immediate) { stopDiscovery() }
        }
    }

    /**
     * The reader mode belongs to the activity, not to this client: a prompt whose command ended
     * after a newer prompt started must not turn the newer one's reader mode off.
     */
    private fun stopDiscovery() {
        if (nfcOwner === this) {
            nfcOwner = null
            runCatching { manager.stopNfcDiscovery(activity) }
        }
        runCatching { manager.stopUsbDiscovery() }
    }

    /**
     * Makes the app's credential on [device]: a non-resident ES256 key for [SECURITY_KEY_RP_ID]
     * with the hmac-secret extension enabled, user verification discouraged. The keys already in
     * [enrolment] are excluded, so the same key cannot be added twice. A key that only makes
     * credentials with its FIDO PIN throws PinRequiredClientError: call again with [pin].
     */
    suspend fun enrol(device: YubiKeyDevice, enrolment: SecurityKeyEnrolment, label: String, pin: CharArray? = null): EnrolledKey =
        withClient(device) { client, state ->
            val options = PublicKeyCredentialCreationOptions(
                PublicKeyCredentialRpEntity(RP_NAME, SECURITY_KEY_RP_ID),
                PublicKeyCredentialUserEntity(USER_NAME, USER_ID, USER_NAME),
                random(32),
                listOf(PublicKeyCredentialParameters(PublicKeyCredentialType.PUBLIC_KEY, ALG_ES256)),
                null,
                enrolment.keys.map { PublicKeyCredentialDescriptor(PublicKeyCredentialType.PUBLIC_KEY, it.credentialIdBytes) },
                AuthenticatorSelectionCriteria(null, ResidentKeyRequirement.DISCOURAGED, UserVerificationRequirement.DISCOURAGED),
                AttestationConveyancePreference.NONE,
                Extensions.fromMap(mapOf(PRF to emptyMap<String, Any>())),
            )
            val credential = client.makeCredential(ClientDataProvider.fromHash(random(32)), options, SECURITY_KEY_RP_ID, pin, null, state)
            val response = credential.response as AuthenticatorAttestationResponse
            val attested = checkNotNull(response.authenticatorData.attestedCredentialData) { "no credential in the attestation" }
            val publicKey = checkNotNull(Cose.getPublicKey(attested.cosePublicKey)) { "no public key in the attestation" }
            EnrolledKey(
                credentialId = Base64.getUrlEncoder().withoutPadding().encodeToString(attested.credentialId),
                publicKey = Base64.getEncoder().encodeToString(publicKey.encoded),
                label = label,
                enrolledAt = System.currentTimeMillis(),
            )
        }

    /**
     * Asks [device] for an assertion over a fresh challenge with one of [enrolment]'s credentials
     * (user presence only: a touch, no PIN) and checks it. With [wantSecret], also the
     * hmac-secret output for the enrolment's salt (the "prf" extension), which the required mode
     * unwraps the data key with.
     */
    suspend fun assert(device: YubiKeyDevice, enrolment: SecurityKeyEnrolment, wantSecret: Boolean): SecurityKeyAssertion =
        withClient(device) { client, state ->
            val clientDataHash = random(32)
            val salt = Base64.getUrlEncoder().withoutPadding().encodeToString(enrolment.saltBytes)
            val extensions = if (wantSecret) Extensions.fromMap(mapOf(PRF to mapOf("eval" to mapOf("first" to salt)))) else null
            val options = PublicKeyCredentialRequestOptions(
                random(32),
                null,
                SECURITY_KEY_RP_ID,
                enrolment.keys.map { PublicKeyCredentialDescriptor(PublicKeyCredentialType.PUBLIC_KEY, it.credentialIdBytes) },
                UserVerificationRequirement.DISCOURAGED,
                extensions,
            )
            val credential = client.getAssertion(ClientDataProvider.fromHash(clientDataHash), options, SECURITY_KEY_RP_ID, null, state)
            val key = enrolment.key(credential.rawId) ?: throw SecurityKeyNotEnrolledException()
            val response = credential.response as AuthenticatorAssertionResponse
            val result = AssertionVerifier.verify(key, response.authenticatorData, clientDataHash, response.signature)
            if (!result.valid) throw SecurityKeyBadSignatureException(result.reason)
            SecurityKeyAssertion(key, if (wantSecret) prfOutput(credential.clientExtensionResults?.toMap(SerializationType.JSON)) else null, result.uv)
        }

    /** The first prf result of [results] (base64url in the JSON form, raw bytes otherwise), null when absent. */
    private fun prfOutput(results: Map<String, Any?>?): ByteArray? {
        val prf = results?.get(PRF) as? Map<*, *> ?: return null
        return when (val first = (prf["results"] as? Map<*, *>)?.get("first")) {
            is ByteArray -> first
            is String -> Base64.getUrlDecoder().decode(first)
            else -> null
        }
    }

    /**
     * Opens the key (FIDO over USB HID when it offers it, else the smart card interface: NFC, or
     * USB CCID) and runs [block] off the main thread. Cancelling the coroutine cancels the
     * command on the key, so a prompt dismissed while it waits for a touch does not hang.
     */
    private suspend fun <T> withClient(device: YubiKeyDevice, block: (WebAuthnClient, CommandState) -> T): T =
        withContext(Dispatchers.IO) {
            val state = CommandState()
            val handle = coroutineContext.job.invokeOnCompletion { if (it is CancellationException) state.cancel() }
            try {
                val connection: YubiKeyConnection =
                    if (device.supportsConnection(FidoConnection::class.java)) device.openConnection(FidoConnection::class.java)
                    else device.openConnection(SmartCardConnection::class.java)
                connection.use { WebAuthnClient.create(it, null, null).use { client -> block(client, state) } }
            } finally {
                handle.dispose()
            }
        }

    private fun random(size: Int): ByteArray = ByteArray(size).also(random::nextBytes)

    private companion object {
        const val RP_NAME = "Ichor"
        const val USER_NAME = "ichor"
        val USER_ID = "ichor".toByteArray()
        const val ALG_ES256 = -7
        const val PRF = "prf"
        /**
         * How long one NFC command may take (the IsoDep transceive timeout, not a wait for a
         * tap): a key that stops answering fails within this, as in Yubico's own FIDO UI.
         */
        const val NFC_TIMEOUT_MS = 5_000

        /** Held while a command runs on a key, by any prompt of the process; see [withKey]. */
        val busy = Mutex()

        /** The client whose NFC reader mode is on, if any. */
        @Volatile
        var nfcOwner: SecurityKeyClient? = null
    }
}
