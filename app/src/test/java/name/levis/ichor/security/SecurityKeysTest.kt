package name.levis.ichor.security

import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import java.security.KeyPairGenerator
import java.security.MessageDigest
import java.security.SecureRandom
import java.security.Signature
import java.security.spec.ECGenParameterSpec
import java.util.Base64

class SecurityKeysTest {

    private val random = SecureRandom()

    private fun enrolled(publicKey: ByteArray, id: ByteArray = ByteArray(16).also(random::nextBytes)) = EnrolledKey(
        credentialId = Base64.getUrlEncoder().withoutPadding().encodeToString(id),
        publicKey = Base64.getEncoder().encodeToString(publicKey),
        label = "Security key",
        enrolledAt = 1_700_000_000_000,
    )

    @Test
    fun enrolmentRoundTripsThroughJson() {
        val record = SecurityKeyEnrolment.create().withKey(enrolled(ByteArray(91)))
        val decoded = SecurityKeyEnrolment.decode(record.encode())
        assertEquals(record, decoded)
        assertEquals(32, record.saltBytes.size)
        assertFalse("UNLOCK mode never requires a tap", record.required)
        assertTrue(record.copy(mode = SecurityKeyMode.REQUIRED).required)
        assertFalse("REQUIRED without keys cannot be required", SecurityKeyEnrolment.create().copy(mode = SecurityKeyMode.REQUIRED).required)
    }

    @Test
    fun decodeToleratesGarbageAndUnknownFields() {
        assertNull(SecurityKeyEnrolment.decode(null))
        assertNull(SecurityKeyEnrolment.decode(""))
        assertNull(SecurityKeyEnrolment.decode("not json"))
        val json = """{"keys":[],"salt":"AAAA","mode":"UNLOCK","future":1}"""
        assertNotNull(SecurityKeyEnrolment.decode(json))
    }

    @Test
    fun keysAreReplacedByCredentialIdAndRemoved() {
        val key = enrolled(ByteArray(91))
        val record = SecurityKeyEnrolment.create().withKey(key).withKey(key.copy(label = "Renamed"))
        assertEquals(1, record.keys.size)
        assertEquals("Renamed", record.keys.single().label)
        assertEquals(key, record.key(key.credentialIdBytes)?.copy(label = key.label))
        assertTrue(record.without(key.credentialId).keys.isEmpty())
    }

    @Test
    fun dekWrapsAndUnwrapsWithTheSameSecretOnly() {
        val dek = DekWrap.newDek()
        val secret = ByteArray(32).also(random::nextBytes)
        val wrapped = DekWrap.wrap(dek, secret)
        assertEquals(12 + 32 + 16, wrapped.size)
        assertArrayEquals(dek, DekWrap.unwrap(wrapped, secret))
        val other = ByteArray(32).also(random::nextBytes)
        try {
            DekWrap.unwrap(wrapped, other)
            throw AssertionError("a different secret must not unwrap")
        } catch (_: SecurityKeyUnwrapException) {
        }
        try {
            DekWrap.unwrap(ByteArray(5), secret)
            throw AssertionError("a damaged blob must not unwrap")
        } catch (_: SecurityKeyUnwrapException) {
        }
        // Two wraps of the same DEK differ (random nonce) but both unwrap.
        assertFalse(wrapped.contentEquals(DekWrap.wrap(dek, secret)))
    }

    @Test
    fun enrolmentUnwrapsTheDekFromAMatchingAssertionOnly() {
        val dek = DekWrap.newDek()
        val secret = ByteArray(32).also(random::nextBytes)
        val plain = enrolled(ByteArray(91))
        val key = plain.wrapping(dek, SecurityKeyAssertion(plain, secret, uv = false))
        val record = SecurityKeyEnrolment.create().withKey(key).copy(mode = SecurityKeyMode.REQUIRED)

        assertArrayEquals(dek, record.unwrapDek(SecurityKeyAssertion(key, secret, uv = false)))
        fun reasonOf(assertion: SecurityKeyAssertion) = try {
            record.unwrapDek(assertion)
            null
        } catch (e: SecurityKeyUnwrapException) {
            e.reason
        }
        assertEquals(SecurityKeyUnwrapException.Reason.NO_SECRET, reasonOf(SecurityKeyAssertion(key, null, false)))
        assertEquals(SecurityKeyUnwrapException.Reason.NOT_WRAPPED, reasonOf(SecurityKeyAssertion(plain, secret, false)))
        assertEquals(SecurityKeyUnwrapException.Reason.UV_CHANGED, reasonOf(SecurityKeyAssertion(key, secret, uv = true)))
        assertEquals(SecurityKeyUnwrapException.Reason.WRONG_KEY, reasonOf(SecurityKeyAssertion(key, ByteArray(32), false)))
        assertEquals(SecurityKeyUnwrapException.Reason.DAMAGED, reasonOf(SecurityKeyAssertion(key.copy(wrappedDek = "AAAA"), secret, false)))
    }

    @Test
    fun wrappingKeyIsDeterministicHkdf() {
        val secret = ByteArray(32) { it.toByte() }
        assertArrayEquals(DekWrap.wrappingKey(secret), DekWrap.wrappingKey(secret))
        assertFalse(DekWrap.wrappingKey(secret).contentEquals(DekWrap.wrappingKey(ByteArray(32))))
        assertEquals(32, DekWrap.wrappingKey(secret).size)
    }

    @Test
    fun sealedFileOpensWithTheDekAndIsRecognised() {
        val dek = DekWrap.newDek()
        val inner = "keystore ciphertext".toByteArray()
        val sealed = SealedFile.seal(inner, dek)
        assertTrue(SealedFile.isSealed(sealed))
        assertFalse(SealedFile.isSealed(inner))
        assertFalse("too short to be sealed", SealedFile.isSealed(SealedFile.MAGIC + ByteArray(12)))
        assertArrayEquals(inner, SealedFile.open(sealed, dek))
        try {
            SealedFile.open(sealed, DekWrap.newDek())
            throw AssertionError("another DEK must not open the file")
        } catch (_: javax.crypto.AEADBadTagException) {
        }
    }

    @Test
    fun assertionVerifiesSignatureFlagsAndRpId() {
        val generator = KeyPairGenerator.getInstance("EC").apply { initialize(ECGenParameterSpec("secp256r1")) }
        val pair = generator.generateKeyPair()
        val key = enrolled(pair.public.encoded)
        val rpIdHash = MessageDigest.getInstance("SHA-256").digest(SECURITY_KEY_RP_ID.toByteArray())
        val clientDataHash = ByteArray(32).also(random::nextBytes)

        fun authData(flags: Int, hash: ByteArray = rpIdHash) = hash + byteArrayOf(flags.toByte()) + byteArrayOf(0, 0, 0, 7)
        fun sign(authData: ByteArray): ByteArray = Signature.getInstance("SHA256withECDSA").run {
            initSign(pair.private)
            update(authData)
            update(clientDataHash)
            sign()
        }

        val up = authData(0x01)
        val ok = AssertionVerifier.verify(key, up, clientDataHash, sign(up))
        assertTrue(ok.reason, ok.valid)
        assertFalse(ok.uv)

        val upUv = authData(0x05)
        val withUv = AssertionVerifier.verify(key, upUv, clientDataHash, sign(upUv))
        assertTrue(withUv.valid)
        assertTrue("the UV flag is reported", withUv.uv)

        assertFalse("no user presence", AssertionVerifier.verify(key, authData(0x04), clientDataHash, sign(authData(0x04))).valid)
        assertFalse("other rp", AssertionVerifier.verify(key, authData(0x01, ByteArray(32)), clientDataHash, sign(authData(0x01, ByteArray(32)))).valid)
        assertFalse("other challenge", AssertionVerifier.verify(key, up, ByteArray(32), sign(up)).valid)
        assertFalse("other key", AssertionVerifier.verify(enrolled(generator.generateKeyPair().public.encoded), up, clientDataHash, sign(up)).valid)
        assertFalse("garbage signature", AssertionVerifier.verify(key, up, clientDataHash, ByteArray(70)).valid)
        assertFalse("short data", AssertionVerifier.verify(key, ByteArray(10), clientDataHash, sign(up)).valid)
        assertFalse("bad stored key", AssertionVerifier.verify(key.copy(publicKey = "AAAA"), up, clientDataHash, sign(up)).valid)
    }

    @Test
    fun `a FIDO PIN takes any character and counts code points`() {
        assertTrue(FidoPin.valid("abc1"))
        assertTrue(FidoPin.valid("p@ss wörd"))
        assertFalse(FidoPin.valid("123"))
        // Four emoji are four code points (eight UTF-16 units).
        assertTrue(FidoPin.valid("\uD83D\uDD11".repeat(4)))
        assertFalse(FidoPin.valid("\uD83D\uDD11".repeat(3)))
        assertTrue(FidoPin.valid("a".repeat(63)))
        assertFalse(FidoPin.valid("a".repeat(64)))
    }

    @Test
    fun `a pasted FIDO PIN loses its line breaks only`() {
        assertEquals("Secret 42", FidoPin.clean("Secret 42\n"))
        assertEquals("ab cd", FidoPin.clean("\r\nab cd\r\n"))
    }
}
