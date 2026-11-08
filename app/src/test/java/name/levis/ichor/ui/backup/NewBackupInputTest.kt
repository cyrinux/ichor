package name.levis.ichor.ui.backup

import name.levis.ichor.R
import name.levis.ichor.model.BackupFileFormat
import name.levis.ichor.model.BackupSeal
import name.levis.ichor.ui.UiText
import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class NewBackupInputTest {
    private val key = "age1qypqxpq9qcrsszg2pvxq6rs0zqg3yyc5z5tpwxqergd3c8g7rusqmwn7f2"
    private val passphrase = "correct horse battery"
    private val age = NewBackupInput(format = BackupFileFormat.AGE)
    private val keys = age.copy(ageMode = AgeSealMode.RECIPIENTS)

    private fun assertError(id: Int, args: Array<out Any>, actual: UiText?) {
        val res = actual as UiText.Res
        assertEquals(id, res.id)
        assertArrayEquals(args, res.args)
    }

    @Test
    fun ichorFormatIsTheDefaultAndNeedsTheSamePassphraseTwice() {
        val input = NewBackupInput(passphrase = passphrase, again = passphrase)
        assertEquals(BackupFileFormat.ICHOR, NewBackupInput().format)
        assertNull(newBackupError(input))
        assertEquals(BackupSeal.Ichor(passphrase), newBackupSeal(input))
        assertError(R.string.backup_err_passphrase_mismatch, emptyArray<Any>(), newBackupError(input.copy(again = "something else")))
        assertEquals(R.string.backup_err_passphrase_short, (newBackupError(NewBackupInput(passphrase = "short", again = "short")) as UiText.Res).id)
    }

    @Test
    fun agePassphraseIsCheckedLikeTheIchorOne() {
        val input = age.copy(passphrase = passphrase, again = passphrase, recipients = "ignored")
        assertNull(newBackupError(input))
        assertEquals(BackupSeal.AgePassphrase(passphrase), newBackupSeal(input))
        assertEquals(R.string.backup_err_passphrase_short, (newBackupError(age) as UiText.Res).id)
    }

    @Test
    fun ageRecipientsNeedOneValidKeyPerLine() {
        assertError(R.string.backup_err_age_recipients_empty, emptyArray<Any>(), newBackupError(keys.copy(recipients = " \n")))
        assertError(R.string.backup_err_age_recipient, arrayOf(2), newBackupError(keys.copy(recipients = "$key\nage1nope")))
        // The passphrase does not matter for recipients.
        val input = keys.copy(recipients = "\n $key \n", passphrase = passphrase)
        assertNull(newBackupError(input))
        assertEquals(BackupSeal.AgeRecipients(listOf(key)), newBackupSeal(input))
        // Comment lines are skipped; an upper-case key goes to the core in lower case.
        val commented = keys.copy(recipients = "# laptop\n${key.uppercase()}")
        assertNull(newBackupError(commented))
        assertEquals(BackupSeal.AgeRecipients(listOf(key)), newBackupSeal(commented))
    }

    @Test
    fun recipientsModeOnlyCountsForAge() {
        val input = NewBackupInput(ageMode = AgeSealMode.RECIPIENTS, passphrase = passphrase, again = passphrase, recipients = key)
        assertEquals(BackupSeal.Ichor(passphrase), newBackupSeal(input))
    }

    @Test
    fun toStringHidesThePassphrase() {
        assertEquals(false, passphrase in NewBackupInput(passphrase = passphrase, again = passphrase).toString())
    }
}
