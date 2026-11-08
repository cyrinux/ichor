package name.levis.ichor.ui.flux

import name.levis.ichor.model.FluxAction
import org.junit.Assert.assertEquals
import org.junit.Test

class FluxRowActionsTest {
    @Test
    fun aRunningObjectReconcilesAndSuspends() {
        assertEquals(
            FluxRowGate(toggle = FluxAction.SUSPEND, canReconcile = true, canToggle = true),
            fluxRowGate(suspended = false, busy = false, denied = false),
        )
    }

    @Test
    fun aSuspendedObjectOnlyResumes() {
        assertEquals(
            FluxRowGate(toggle = FluxAction.RESUME, canReconcile = false, canToggle = true),
            fluxRowGate(suspended = true, busy = false, denied = false),
        )
    }

    @Test
    fun aRequestInFlightLocksBothActions() {
        assertEquals(
            FluxRowGate(toggle = FluxAction.SUSPEND, canReconcile = false, canToggle = false),
            fluxRowGate(suspended = false, busy = true, denied = false),
        )
    }

    @Test
    fun aDeniedAccountLocksBothActions() {
        assertEquals(
            FluxRowGate(toggle = FluxAction.RESUME, canReconcile = false, canToggle = false),
            fluxRowGate(suspended = true, busy = false, denied = true),
        )
    }
}
