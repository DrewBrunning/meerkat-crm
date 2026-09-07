package com.mycorrhizal.crm.feature.settings

import androidx.compose.ui.test.assertHasClickAction
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performScrollTo
import com.mycorrhizal.crm.domain.repository.ContactRepository
import com.mycorrhizal.crm.domain.repository.ExportRepository
import com.mycorrhizal.crm.domain.repository.RelationshipEdgeRepository
import com.mycorrhizal.crm.ui.theme.MycorrhizalTheme
import io.mockk.coEvery
import io.mockk.coVerify
import io.mockk.mockk
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode

// The DataScreen composable had no UI test — DataViewModelTest covers the
// view-model only. This pins the "Suggest addresses" primary action (a filled
// Button, matching the sibling "Suggest relationships") and its wiring to the
// address scan, plus the full-dataset export rows (issue #710). The screen's
// body is a verticalScroll Column (like SettingsScreen), so every row is
// always composed and performScrollTo() reliably brings it into view.
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35])
@GraphicsMode(GraphicsMode.Mode.NATIVE)
class DataScreenTest {

    @get:Rule
    val composeTestRule = createComposeRule()

    private val contactRepository = mockk<ContactRepository>()
    private val relationshipEdgeRepository = mockk<RelationshipEdgeRepository>()
    private val exportRepository = mockk<ExportRepository>()

    private fun setScreen() {
        val viewModel = DataViewModel(contactRepository, relationshipEdgeRepository, exportRepository)
        composeTestRule.setContent {
            MycorrhizalTheme {
                DataScreen(onBack = {}, viewModel = viewModel)
            }
        }
    }

    @Test
    fun `suggest addresses is a displayed, clickable action`() {
        setScreen()

        composeTestRule.onNodeWithText("Suggest addresses")
            .performScrollTo()
            .assertIsDisplayed()
            .assertHasClickAction()
    }

    @Test
    fun `tapping suggest addresses runs the address scan`() {
        coEvery { contactRepository.suggestContactAddresses() } returns Result.success(emptyList())
        setScreen()

        composeTestRule.onNodeWithText("Suggest addresses").performScrollTo().performClick()
        composeTestRule.waitForIdle()

        coVerify { contactRepository.suggestContactAddresses() }
    }

    @Test
    fun `the export section offers every dataset format`() {
        setScreen()

        composeTestRule.onNodeWithText("Export CSV backup").performScrollTo().assertIsDisplayed()
        composeTestRule.onNodeWithText("Export vCard 4.0").performScrollTo().assertIsDisplayed()
        composeTestRule.onNodeWithText("Export vCard 3.0").performScrollTo().assertIsDisplayed()
        composeTestRule.onNodeWithText("Export JSContact").performScrollTo().assertIsDisplayed()
        composeTestRule.onNodeWithText("Export audit log").performScrollTo().assertIsDisplayed()
    }

    @Test
    fun `tapping export csv fetches the full csv backup`() {
        coEvery { exportRepository.exportDataCsv() } returns Result.success("csv".toByteArray())
        setScreen()

        composeTestRule.onNodeWithText("Export CSV backup").performScrollTo().performClick()
        composeTestRule.waitForIdle()

        coVerify { exportRepository.exportDataCsv() }
    }
}
