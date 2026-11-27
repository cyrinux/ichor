package name.levis.ichor.ui.workloads

import name.levis.ichor.model.KubeWorkload
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class WorkloadsListDetailTest {
    private val web = KubeWorkload("Deployment", "shop", "web", desired = 3, ready = 3)
    private val db = KubeWorkload("StatefulSet", "shop", "db", desired = 1, ready = 1)

    @Test
    fun theKeyPicksTheWorkload() {
        assertEquals(db, pickedWorkload(listOf(web, db), "StatefulSet/shop/db"))
    }

    @Test
    fun thePaneFollowsARefreshOfThePickedWorkload() {
        val scaled = web.copy(desired = 5, ready = 4)
        assertEquals(scaled, pickedWorkload(listOf(scaled, db), web.key))
    }

    @Test
    fun theSameNameOfAnotherKindIsNotPicked() {
        val webSet = KubeWorkload("StatefulSet", "shop", "web")
        assertNull(pickedWorkload(listOf(webSet), web.key))
    }

    @Test
    fun aDeletedWorkloadShowsThePlaceholder() {
        assertNull(pickedWorkload(listOf(db), web.key))
    }

    @Test
    fun nothingPickedOrNotReadShowsThePlaceholder() {
        assertNull(pickedWorkload(listOf(web), null))
        assertNull(pickedWorkload(null, web.key))
    }
}
