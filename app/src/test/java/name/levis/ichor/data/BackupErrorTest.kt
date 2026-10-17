package name.levis.ichor.data

import name.levis.ichor.R
import name.levis.ichor.ui.UiText
import name.levis.ichorgo.Ichorgo
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class BackupErrorTest {
    private fun resId(message: String) = (backupException(message)?.text as UiText.Res).id

    @Test
    fun recipientOnlyAgeFileSaysItNeedsTheAgeKey() {
        assertEquals("backup-needs-identity", Ichorgo.BackupErrNeedsIdentity)
        assertEquals(R.string.backup_err_needs_identity, resId(Ichorgo.BackupErrNeedsIdentity + ": no passphrase recipient"))
    }

    @Test
    fun knownCodesKeepTheirMessages() {
        assertTrue(backupException(Ichorgo.BackupErrWrongPassphrase) is BackupPassphraseException)
        assertEquals(R.string.backup_err_invalid, resId(Ichorgo.BackupErrInvalidContent + ": mixed recipients"))
        assertEquals(R.string.backup_err_not_backup, resId(Ichorgo.BackupErrNotBackup))
        assertNull(backupException("something else"))
    }
}
