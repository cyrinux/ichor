package name.levis.ichor.model

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import java.time.LocalDate

class AgeBackupTest {
    // Made up: bech32 ("age" prefix, valid checksum) of the bytes 1..32 and 101..132, no one's key.
    private val key = "age1qypqxpq9qcrsszg2pvxq6rs0zqg3yyc5z5tpwxqergd3c8g7rusqmwn7f2"
    private val other = "age1v4nxw6rfdf4kcmtwdac8zunnw36hvamc09a8klra0elcpqvzswzqw7hsdz"

    @Test
    fun acceptsX25519Recipients() {
        assertEquals(62, key.length)
        assertTrue(isAgeRecipient(key))
        assertTrue(isAgeRecipient(other))
        // bech32 is single-case: all upper case is the same key.
        assertTrue(isAgeRecipient(key.uppercase()))
    }

    @Test
    fun refusesWhatIsNoX25519Recipient() {
        assertFalse(isAgeRecipient(""))
        assertFalse(isAgeRecipient("age1"))
        // One character short, one too many.
        assertFalse(isAgeRecipient(key.dropLast(1)))
        assertFalse(isAgeRecipient(key + "q"))
        // A typo breaks the checksum.
        assertFalse(isAgeRecipient(key.dropLast(1) + "3"))
        assertFalse(isAgeRecipient(key.replaceRange(10, 11, "z")))
        // Not in the bech32 alphabet: 1, b, i, o.
        assertFalse(isAgeRecipient(key.dropLast(1) + "b"))
        assertFalse(isAgeRecipient(key.dropLast(1) + "1"))
        assertFalse(isAgeRecipient(key.dropLast(1) + "o"))
        // Mixed case, another prefix, SSH, secret and plugin keys.
        assertFalse(isAgeRecipient("A" + key.drop(1)))
        assertFalse(isAgeRecipient("age2" + key.drop(4)))
        assertFalse(isAgeRecipient("AGE-SECRET-KEY-1" + "Q".repeat(46)))
        assertFalse(isAgeRecipient("age1pq1" + "q".repeat(55)))
        assertFalse(isAgeRecipient("age1tag1" + "q".repeat(54)))
        assertFalse(isAgeRecipient("ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIExample you@laptop"))
        assertFalse(isAgeRecipient("age1yubikey1" + "q".repeat(50)))
        assertFalse(isAgeRecipient(" $key"))
    }

    @Test
    fun recipientLinesSkipBlankLinesAndTrim() {
        assertEquals(listOf(key, other), ageRecipientLines("\n  $key  \r\n\n\t$other\n"))
        assertEquals(listOf(key), ageRecipientLines("# laptop\n$key\n  # created: 2026-10-02"))
        assertEquals(emptyList<String>(), ageRecipientLines(" \n\n"))
    }

    @Test
    fun firstInvalidLineIsOneBasedAndSkipsBlankAndCommentLines() {
        assertNull(firstInvalidAgeRecipientLine(""))
        assertNull(firstInvalidAgeRecipientLine("$key\n\n  $other \n"))
        assertNull(firstInvalidAgeRecipientLine("# public key: laptop\n$key"))
        assertEquals(3, firstInvalidAgeRecipientLine("$key\n\nnot a key\n$other"))
        assertEquals(1, firstInvalidAgeRecipientLine("age1short\nalso bad"))
    }

    @Test
    fun fileNameEndsWithTheFormatsExtension() {
        val day = LocalDate.of(2026, 10, 2)
        assertEquals("ichor-2026-10-02.ichorbackup", backupFileName(day, BackupFileFormat.ICHOR))
        assertEquals("ichor-2026-10-02.age", backupFileName(day, BackupFileFormat.AGE))
    }

    @Test
    fun sealsKnowTheirFormatAndHideThePassphrase() {
        assertEquals(BackupFileFormat.ICHOR, BackupSeal.Ichor("correct horse battery").format)
        assertEquals(BackupFileFormat.AGE, BackupSeal.AgePassphrase("correct horse battery").format)
        assertEquals(BackupFileFormat.AGE, BackupSeal.AgeRecipients(listOf(key)).format)
        assertFalse("correct horse" in BackupSeal.Ichor("correct horse battery").toString())
        assertFalse("correct horse" in BackupSeal.AgePassphrase("correct horse battery").toString())
    }

    @Test
    fun ageFilesLookLikeBackups() {
        assertTrue(looksLikeBackup("age-encryption.org/v1\n-> scrypt abc 18\n".toByteArray()))
        assertTrue(looksLikeBackup("-----BEGIN AGE ENCRYPTED FILE-----\nYWdlLWVuY3J5cHRpb24ub3JnL3YxCg==\n".toByteArray()))
        assertFalse(looksLikeBackup("age-encryption.org/v2\n".toByteArray()))
        assertFalse(looksLikeBackup("age-encryption.org".toByteArray()))
    }
}
