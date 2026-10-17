package name.levis.ichor.data

import androidx.annotation.StringRes
import name.levis.ichor.R
import android.content.pm.PackageManager
import android.os.Build
import android.os.Looper
import android.security.keystore.KeyInfo
import android.security.keystore.KeyProperties
import android.security.keystore.StrongBoxUnavailableException
import name.levis.ichor.security.DekHolder
import name.levis.ichor.security.SealedFile
import name.levis.ichor.security.SecurityKeyRequiredException
import name.levis.ichor.ui.LocalizedException
import name.levis.ichor.ui.UiText
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.delay
import java.io.File
import javax.crypto.SecretKey

/** Where the key protecting the config lives, strongest first. */
enum class KeyProtection(@StringRes val label: Int) {
    STRONGBOX(R.string.settings_key_strongbox),
    TEE(R.string.settings_key_tee),
    SOFTWARE(R.string.settings_key_software),
}

/**
 * Stores secrets in a file encrypted with AES-256-GCM. The key is generated inside the Android
 * Keystore and never leaves it: in the StrongBox secure element when the device has one
 * (Android's counterpart of the Secure Enclave), otherwise in the TEE. It wraps a random data
 * key that encrypts the content ([EnvelopeFormat] v2); files written before are read too
 * (v1, the content directly under the Keystore key) and rewritten by [upgrade].
 *
 * With a security key required ([outer], see SecurityKeys.kt) that payload is sealed once more
 * with the data key only a tap of the key unwraps ([SealedFile]): the file then reads only
 * after a key was tapped this run, and [read] throws [SecurityKeyRequiredException] before.
 *
 * [onTeeFallback] runs when a new key went to the TEE although the device has StrongBox (the
 * chip kept refusing), so Settings can say so; [rekeyToStrongBox] moves such a key to the chip.
 *
 * The key lives under one of two aliases, [keyAlias] or `"$keyAlias.next"`, because a Keystore
 * alias cannot be renamed: a move makes the new key under the other one. A one-line marker
 * next to the file (`<file>.alias`) records the alias in use; none means [keyAlias]. The file
 * is decrypted with the recorded alias first, then the other, so a kill between the swap of
 * the file and the marker still reads (and the marker is then fixed, see [adopt]).
 */
class SecureStore internal constructor(
    private val file: File,
    private val keyAlias: String,
    private val strongBoxAvailable: Boolean,
    private val outer: DekHolder?,
    private val onTeeFallback: () -> Unit,
    private val keys: KeySlots,
    /** Runs before each step of [rekeyToStrongBox]: tests make one fail or stop the run there. */
    private val beforeStep: (RekeyStep) -> Unit = {},
) {

    constructor(
        file: File,
        keyAlias: String = "talosconfig",
        strongBoxAvailable: Boolean = false,
        outer: DekHolder? = null,
        onTeeFallback: () -> Unit = {},
    ) : this(file, keyAlias, strongBoxAvailable, outer, onTeeFallback, AndroidKeySlots)

    private val dir: File = file.absoluteFile.parentFile
    private val nextAlias = "$keyAlias.next"
    private val aliasFile = File(dir, "${file.name}.alias")
    private val nextFile = File(dir, "${file.name}.next")

    fun write(plaintext: ByteArray) {
        val inner = EnvelopeFormat.encrypt(plaintext, key())
        val holder = outer
        val payload = if (holder != null && holder.sealing()) {
            // Never written unsealed while the key is required: a missing DEK is a bug upstream.
            SealedFile.seal(inner, holder.dek() ?: throw SecurityKeyRequiredException())
        } else {
            inner
        }
        val tmp = File(dir, "${file.name}.tmp")
        tmp.writeBytes(payload)
        if (!tmp.renameTo(file)) {
            tmp.delete()
            throw IllegalStateException("Could not persist encrypted config")
        }
    }

    /**
     * Makes sure the Keystore key answers before a write that must not fail halfway (an import,
     * a restored backup). A secure element can drop its connection after days of uptime and
     * refuse every call with UNKNOWN_ERROR until its service reconnects: the key is tried with
     * a real wrap and unwrap, again after each of [delays] (ms). A new key goes to StrongBox
     * when the chip answers in time, else to the TEE; an existing StrongBox key cannot move,
     * so [KeystoreUnavailableException] says the chip does not answer.
     */
    suspend fun prepare(delays: List<Long> = CHIP_RETRY_DELAYS) {
        if (existingKey() != null) {
            try {
                retrying(delays) { probe(storedKey()) }
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                throw KeystoreUnavailableException(e)
            }
            return
        }
        val strongBox = strongBoxAvailable && Build.VERSION.SDK_INT >= Build.VERSION_CODES.P
        val alias = recordedAlias()
        strongBoxOrTee(
            strongBox = if (strongBox) ({ keys.generate(alias, strongBox = true).also(::probe) }) else null,
            tee = { keys.generate(alias, strongBox = false).also(::probe).also { if (strongBox) onTeeFallback() } },
            delays = delays,
            // A StrongBox key left half made would be picked up by the next write.
            discard = { keys.delete(alias) },
        )
    }

    private fun probe(key: SecretKey) {
        EnvelopeFormat.decrypt(EnvelopeFormat.encrypt(PROBE, key), key)
    }

    fun read(): ByteArray? {
        if (!file.exists()) return null
        return decryptStored(unseal(file.readBytes())).first
    }

    /**
     * Rewrites a file stored before the envelope format (v1) as v2, sealed as the current mode
     * asks; true when it did. A v2 file, or none, is left alone. Throws like [read].
     */
    fun upgrade(): Boolean {
        if (!file.exists()) return false
        val payload = unseal(file.readBytes())
        if (EnvelopeFormat.isEnvelope(payload)) return false
        write(decryptStored(payload).first)
        return true
    }

    /** Never a new key here: it could not decrypt the file, and would replace the one that can. */
    private fun storedKey(): SecretKey = checkNotNull(existingKey()) { "The key of the stored config is missing" }

    /**
     * The Keystore [payload] decrypted, with the alias whose key did it: the recorded alias
     * first, then the other one (a move killed after the file was swapped, see [rekeyToStrongBox]).
     * Throws the failure of the recorded key when neither opens it.
     */
    private fun decryptStored(payload: ByteArray): Pair<ByteArray, String> {
        val recorded = recordedAlias()
        var failure: Exception? = null
        for (alias in listOf(recorded, otherAlias(recorded))) {
            try {
                val key = keys.get(alias) ?: continue
                val plaintext = EnvelopeFormat.decrypt(payload, key)
                if (alias != recorded) adopt(alias)
                return plaintext to alias
            } catch (e: Exception) {
                failure?.addSuppressed(e) ?: run { failure = e }
            }
        }
        throw failure ?: IllegalStateException("The key of the stored config is missing")
    }

    /**
     * Finishes a move a kill cut short: the file opened with [alias], not the recorded one, so
     * [alias] is recorded and the other key dropped (only once the directory is on disk, so a
     * power cut cannot bring the old file back without its key). Best effort: the read works
     * either way.
     */
    private fun adopt(alias: String) {
        runCatching {
            replaceSynced(aliasFile, alias.encodeToByteArray())
            if (syncDir(dir)) keys.delete(otherAlias(alias))
        }
    }

    /** The alias the marker records; [keyAlias] without one, or with anything else in it. */
    private fun recordedAlias(): String =
        runCatching { aliasFile.takeIf { it.exists() }?.readText()?.trim() }.getOrNull()
            ?.takeIf { it == nextAlias } ?: keyAlias

    private fun otherAlias(alias: String): String = if (alias == keyAlias) nextAlias else keyAlias

    /** The alias of the key in use, for tests: the recorded one when its key exists, else the other. */
    internal fun aliasInUse(): String? = recordedAlias().let { listOf(it, otherAlias(it)) }.firstOrNull { keys.get(it) != null }

    /**
     * The Keystore payload of a stored file: opened with the DEK when sealed. A sealed file with
     * no DEK this run is unreadable while the key is required; a payload that only starts like a
     * sealed one (a Keystore IV can) is taken as is when opening it fails. Returns [payload]
     * itself when it was not opened, a new array when it was.
     */
    private fun unseal(payload: ByteArray): ByteArray {
        if (!SealedFile.isSealed(payload)) return payload
        val dek = outer?.dek()
        if (dek != null) {
            try {
                return SealedFile.open(payload, dek)
            } catch (_: javax.crypto.AEADBadTagException) {
                // Not sealed with this DEK: a Keystore IV that happens to start with the magic.
            }
        } else if (outer?.sealing() == true) {
            throw SecurityKeyRequiredException()
        }
        return payload
    }

    /** Whether the stored file is sealed with the security-key DEK (false when none is stored). */
    fun isSealed(): Boolean = file.exists() && SealedFile.isSealed(file.readBytes())

    /**
     * Writes the stored value again as the current mode asks: sealed once the key is required,
     * Keystore-only once it no longer is (the DEK must still be held then). Nothing stored: no-op.
     */
    fun reseal() {
        read()?.let { write(it) }
    }

    fun clear() {
        file.delete()
        nextFile.delete()
        aliasFile.delete()
        // Drop the key too, so a later import gets a fresh one at the best available level.
        keys.delete(keyAlias)
        keys.delete(nextAlias)
    }

    /**
     * Moves the key of the stored file from the TEE into StrongBox, keeping what it holds; true
     * when the file is now under a new StrongBox key. Never the security-key seal: a sealed file
     * is sealed again with the same DEK. A failure, or a kill, at any step leaves a file that
     * reads: until the swap ([RekeyStep.RENAME]) the old file and key are untouched, and a
     * failure there removes what the move made; after it, [read] tries the recorded alias, then
     * the other, so the file reads whichever of the old key and the marker went first. A chip
     * that keeps refusing (tried again after each of [delays], ms) gives false, and one that
     * refuses the parameters ([StrongBoxUnavailableException]) false at once: nothing changed.
     *
     * No stored file: nothing to move, and a key left without one is dropped with the marker
     * (true). A sealed file with no security key tapped this run throws
     * [SecurityKeyRequiredException] before anything changed.
     */
    suspend fun rekeyToStrongBox(
        delays: List<Long> = CHIP_RETRY_DELAYS,
        pause: suspend (Long) -> Unit = { delay(it) },
    ): Boolean {
        if (!file.exists()) return dropStrayKeys()
        if (!strongBoxAvailable) return false
        return try {
            beforeStep(RekeyStep.READ)
            val raw = file.readBytes()
            val inner = unseal(raw)
            // Sealed again as it was: unseal returns a new array only when the DEK opened it.
            val dek = if (inner !== raw) outer?.dek() else null
            val (plaintext, oldAlias) = decryptStored(inner)
            try {
                moveKey(plaintext, oldAlias, otherAlias(oldAlias), dek, delays, pause)
            } finally {
                plaintext.fill(0)
            }
        } catch (e: CancellationException) {
            throw e
        } catch (e: SecurityKeyRequiredException) {
            // Thrown by the read, before any change: the caller asks for the key, not the chip.
            throw e
        } catch (_: Exception) {
            false
        }
    }

    /** No file to protect: its keys and marker go, so the next write makes a fresh key. */
    private fun dropStrayKeys(): Boolean = try {
        aliasFile.delete()
        nextFile.delete()
        keys.delete(keyAlias)
        keys.delete(nextAlias)
        true
    } catch (_: Exception) {
        false
    }

    private suspend fun moveKey(
        plaintext: ByteArray,
        oldAlias: String,
        newAlias: String,
        dek: ByteArray?,
        delays: List<Long>,
        pause: suspend (Long) -> Unit,
    ): Boolean {
        try {
            beforeStep(RekeyStep.CLEAR_LEFTOVER_KEY)
            // The file opened with oldAlias: a key under newAlias is left from a move cut short.
            keys.delete(newAlias)
            beforeStep(RekeyStep.GENERATE)
            val key = retrying(delays, pause, retryOn = { it !is StrongBoxUnavailableException }) {
                keys.generate(newAlias, strongBox = true).also(::probe)
            }
            beforeStep(RekeyStep.WRITE_NEXT)
            val inner = EnvelopeFormat.encrypt(plaintext, key)
            writeSynced(nextFile, dek?.let { SealedFile.seal(inner, it) } ?: inner)
            beforeStep(RekeyStep.VERIFY_NEXT)
            val written = nextFile.readBytes()
            val back = EnvelopeFormat.decrypt(dek?.let { SealedFile.open(written, it) } ?: written, key)
            try {
                check(back.contentEquals(plaintext)) { "The re-keyed config does not read back" }
            } finally {
                back.fill(0)
            }
            beforeStep(RekeyStep.RENAME)
            check(nextFile.renameTo(file)) { "Could not swap the re-keyed config" }
        } catch (e: Exception) {
            // Before the swap: the old file and key are untouched, drop what the move made.
            runCatching { nextFile.delete() }
            runCatching { keys.delete(newAlias) }
            throw e
        }
        return finishMove(oldAlias, newAlias)
    }

    /**
     * After the swap the file reads only with the new key: nothing is undone. The old key goes
     * once the rename is on disk (kept when that cannot be told), then the marker records the
     * new alias; a kill between the two leaves a marker naming a missing key, and [read] falls
     * through to the new one and records it. True once the marker says the new alias; a
     * failure here before that still reads, and the next [read] fixes the marker.
     */
    private fun finishMove(oldAlias: String, newAlias: String): Boolean {
        try {
            beforeStep(RekeyStep.SYNC_DIR)
            val durable = syncDir(dir)
            beforeStep(RekeyStep.DELETE_OLD_KEY)
            if (durable) keys.delete(oldAlias)
            beforeStep(RekeyStep.RECORD_ALIAS)
            replaceSynced(aliasFile, newAlias.encodeToByteArray())
            beforeStep(RekeyStep.CLEAN_UP)
            File(dir, "${aliasFile.name}.tmp").delete()
            File(dir, "${file.name}.tmp").delete()
        } catch (e: CancellationException) {
            throw e
        } catch (_: Exception) {
            return recordedAlias() == newAlias
        }
        return true
    }

    /** Protection level of the key of the stored file, or null when nothing is stored. */
    fun protection(): KeyProtection? {
        if (!file.exists()) return null
        val key = existingKey() ?: return null
        val info = javax.crypto.SecretKeyFactory.getInstance(key.algorithm, ANDROID_KEYSTORE)
            .getKeySpec(key, KeyInfo::class.java) as KeyInfo
        return if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) {
            when (info.securityLevel) {
                KeyProperties.SECURITY_LEVEL_STRONGBOX -> KeyProtection.STRONGBOX
                KeyProperties.SECURITY_LEVEL_TRUSTED_ENVIRONMENT -> KeyProtection.TEE
                else -> KeyProtection.SOFTWARE
            }
        } else {
            @Suppress("DEPRECATION")
            if (info.isInsideSecureHardware) KeyProtection.TEE else KeyProtection.SOFTWARE
        }
    }

    /** The key in use: under the recorded alias, else the other one (see [decryptStored]). */
    private fun existingKey(): SecretKey? {
        val recorded = recordedAlias()
        return keys.get(recorded) ?: keys.get(otherAlias(recorded))
    }

    /**
     * The key, made on the first write. A busy chip is tried again after each of
     * [CHIP_RETRY_DELAYS] like [prepare] does, but only off the main thread: there it gets a
     * single try and never a wait, so no write from the UI blocks it longer than before.
     */
    private fun key(): SecretKey {
        existingKey()?.let { return it }
        val onMain = Looper.myLooper() == Looper.getMainLooper()
        val alias = recordedAlias()
        return chipKeyOrTee(
            strongBox = strongBoxAvailable && Build.VERSION.SDK_INT >= Build.VERSION_CODES.P,
            generate = { strongBox -> keys.generate(alias, strongBox) },
            delays = if (onMain) emptyList() else CHIP_RETRY_DELAYS,
            // A StrongBox key left half made would be picked up by the next write.
            discard = { keys.delete(alias) },
            onTeeFallback = onTeeFallback,
        )
    }

    private companion object {
        const val ANDROID_KEYSTORE = "AndroidKeyStore"
        val PROBE = "ichor".encodeToByteArray()
    }
}

/** How long to wait before each new try of the secure element, in ms: its service reconnects in seconds. */
private val CHIP_RETRY_DELAYS = listOf(250L, 1000L, 3000L)

/**
 * A new key from [strongBox], tried again after each of [delays] (ms), else from [tee]; null
 * [strongBox] (no chip) goes to [tee] at once. [discard] runs before the fallback, to drop what
 * a failed try may have left. A failure of [tee] is thrown with the StrongBox one suppressed.
 */
internal suspend fun <K> strongBoxOrTee(
    strongBox: (() -> K)?,
    tee: () -> K,
    delays: List<Long>,
    pause: suspend (Long) -> Unit = { delay(it) },
    discard: () -> Unit = {},
): K {
    if (strongBox == null) return tee()
    val chipFailure = try {
        return retrying(delays, pause, retryOn = { it !is StrongBoxUnavailableException }) { strongBox() }
    } catch (e: CancellationException) {
        throw e
    } catch (e: Exception) {
        e
    }
    runCatching(discard)
    return try {
        tee()
    } catch (e: Exception) {
        e.addSuppressed(chipFailure)
        throw e
    }
}

/**
 * A new key from [generate] in StrongBox when [strongBox], tried again after each of [delays]
 * (ms, waited with [pause] on the calling thread), else in the TEE. A
 * [StrongBoxUnavailableException] (unsupported parameters) goes to the TEE at once. Unlike
 * [strongBoxOrTee] it does not suspend: [SecureStore.write] does not either.
 */
internal fun <K> chipKeyOrTee(
    strongBox: Boolean,
    generate: (strongBox: Boolean) -> K,
    delays: List<Long>,
    pause: (Long) -> Unit = { Thread.sleep(it) },
    discard: () -> Unit = {},
    onTeeFallback: () -> Unit = {},
): K {
    if (!strongBox) return generate(false)
    for (attempt in 0..delays.size) {
        try {
            return generate(true)
        } catch (e: java.security.ProviderException) {
            // Unsupported parameters: no point in waiting (checked like strongBoxOrTee does).
            if (e is StrongBoxUnavailableException) break
            // UNKNOWN_ERROR while the chip's connection is down: it reconnects in seconds.
            delays.getOrNull(attempt)?.let(pause)
        }
    }
    runCatching(discard)
    return generate(false).also { onTeeFallback() }
}

/** The Keystore key of the stored config does not answer, even after a few tries. */
class KeystoreUnavailableException(cause: Exception) : LocalizedException(UiText.Res(R.string.keystore_unavailable)) {
    init {
        initCause(cause)
    }
}

fun hasStrongBox(pm: PackageManager): Boolean =
    Build.VERSION.SDK_INT >= Build.VERSION_CODES.P && pm.hasSystemFeature(PackageManager.FEATURE_STRONGBOX_KEYSTORE)
