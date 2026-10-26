package name.levis.ichor.ui.operations

import name.levis.ichor.R
import name.levis.ichor.data.CARotationRunState
import name.levis.ichor.model.CARotationProgress
import org.junit.Assert.assertEquals
import org.junit.Test

class CARotationPresenterTest {
    @Test
    fun aRunningRotationShowsItsStepAndCanBeStopped() {
        val run = CARotationRunState("admin@prod", listOf(CARotationProgress(phase = "swap", step = 4, total = 5, message = "making the new CA the issuing one on cp-1")))
        val n = CARotationPresenter.progress(run)
        assertEquals(res(R.string.ca_rotate_notification_title, "admin@prod"), n.title)
        assertEquals(raw("making the new CA the issuing one on cp-1"), n.text)
        assertEquals(OperationProgress(3, 5), n.progress)
        assertEquals(listOf(OperationAction.STOP), n.actions)

        val stopping = CARotationPresenter.progress(run.copy(stopping = true))
        assertEquals(res(R.string.ca_rotate_stopping), stopping.text)
        assertEquals(emptyList<OperationAction>(), stopping.actions)

        assertEquals("nothing reported yet", OperationProgress.INDETERMINATE, CARotationPresenter.progress(CARotationRunState("admin@prod")).progress)
    }

    @Test
    fun anEndedRotationSaysHowItWent() {
        val done = CARotationPresenter.result(CARotationRunState("admin@prod", finished = true))
        assertEquals(res(R.string.ca_rotate_notification_done, "admin@prod"), done.title)

        val failed = CARotationPresenter.result(CARotationRunState("admin@prod", finished = true, error = "cp-2 does not answer"))
        assertEquals(res(R.string.ca_rotate_notification_failed, "admin@prod"), failed.title)
        assertEquals(raw("cp-2 does not answer"), failed.text)
    }
}
