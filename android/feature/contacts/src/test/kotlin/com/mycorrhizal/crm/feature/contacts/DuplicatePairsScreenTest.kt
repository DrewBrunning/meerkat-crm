package com.mycorrhizal.crm.feature.contacts

import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import com.mycorrhizal.crm.model.network.ContactSummary
import com.mycorrhizal.crm.model.network.DuplicatePair
import com.mycorrhizal.crm.ui.theme.MycorrhizalTheme
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35])
@GraphicsMode(GraphicsMode.Mode.NATIVE)
class DuplicatePairsScreenTest {

    @get:Rule
    val composeTestRule = createComposeRule()

    private val pair = DuplicatePair(
        a = ContactSummary(id = 1, uid = "u-a", firstname = "Dana", lastname = "White"),
        b = ContactSummary(id = 2, uid = "u-b", firstname = "Dan", lastname = "White"),
        reasons = listOf("email", "phone"),
        confidence = 0.95,
    )

    private var mergedKeep = 0L
    private var mergedMerge = 0L
    private var mergedName: String? = null
    private var dismissed: DuplicatePair? = null

    private fun setContent(uiState: DuplicatePairsUiState) {
        mergedKeep = 0L
        mergedMerge = 0L
        mergedName = null
        dismissed = null
        composeTestRule.setContent {
            MycorrhizalTheme {
                DuplicatePairsScreenContent(
                    uiState = uiState,
                    onBack = {},
                    onMerge = { keep, merge, name ->
                        mergedKeep = keep
                        mergedMerge = merge
                        mergedName = name
                    },
                    onDismiss = { dismissed = it },
                    onLoad = {},
                )
            }
        }
    }

    @Test
    fun `empty scan shows the no-duplicates message`() {
        setContent(DuplicatePairsUiState(pairs = emptyList(), total = 0, isLoading = false))
        composeTestRule.onNodeWithTag("duplicates-empty").assertIsDisplayed()
        composeTestRule.onNodeWithText("No duplicate contacts found.").assertIsDisplayed()
    }

    @Test
    fun `populated scan shows both contacts, reason chips and actions`() {
        setContent(DuplicatePairsUiState(pairs = listOf(pair), total = 1, isLoading = false))
        composeTestRule.onNodeWithText("Dana White").assertIsDisplayed()
        composeTestRule.onNodeWithText("Dan White").assertIsDisplayed()
        composeTestRule.onNodeWithText("Same email").assertIsDisplayed()
        composeTestRule.onNodeWithText("Same phone").assertIsDisplayed()
        composeTestRule.onNodeWithText("Merge").assertIsDisplayed()
        composeTestRule.onNodeWithText("Not a duplicate").assertIsDisplayed()
    }

    @Test
    fun `merge opens the keeper dialog and confirms with the chosen keeper`() {
        setContent(DuplicatePairsUiState(pairs = listOf(pair), total = 1, isLoading = false))

        composeTestRule.onNodeWithText("Merge").performClick()
        composeTestRule.onNodeWithText("Which contact should be kept?").assertIsDisplayed()
        // Default is pair.a; switching to pair.b must invert keep/merge.
        composeTestRule.onNodeWithTag("keeper-2").performClick()
        composeTestRule.onNodeWithTag("merge-confirm").performClick()

        assertEquals(2L, mergedKeep)
        assertEquals(1L, mergedMerge)
        assertEquals("Dana White", mergedName)
    }

    @Test
    fun `dismiss requires a confirmation before it fires`() {
        setContent(DuplicatePairsUiState(pairs = listOf(pair), total = 1, isLoading = false))

        composeTestRule.onNodeWithText("Not a duplicate").performClick()
        composeTestRule.onNodeWithText("Mark as not a duplicate?").assertIsDisplayed()
        composeTestRule.onNodeWithTag("dismiss-confirm").performClick()

        assertEquals(pair.reviewKey, dismissed?.reviewKey)
    }

    @Test
    fun `first-page error with no rows offers a retry`() {
        var reloads = 0
        mergedKeep = 0
        mergedMerge = 0
        dismissed = null
        composeTestRule.setContent {
            MycorrhizalTheme {
                DuplicatePairsScreenContent(
                    uiState = DuplicatePairsUiState(pairs = emptyList(), total = 0, isLoading = false, error = "scan failed"),
                    onBack = {},
                    onMerge = { _, _, _ -> },
                    onDismiss = {},
                    onLoad = { reloads += 1 },
                )
            }
        }
        assertTrue(composeTestRule.onAllNodesWithText("scan failed").fetchSemanticsNodes().isNotEmpty())
        composeTestRule.onNodeWithText("Retry").performClick()
        assertEquals(1, reloads)
    }
}
