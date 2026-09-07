package com.mycorrhizal.crm.feature.contacts

import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.assertIsOff
import androidx.compose.ui.test.assertIsOn
import androidx.compose.ui.test.assertIsToggleable
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performScrollTo
import com.mycorrhizal.crm.model.network.ContactMergeAssociationCounts
import com.mycorrhizal.crm.model.network.ContactMergeFieldConflict
import com.mycorrhizal.crm.model.network.ContactMergePreviewResponse
import com.mycorrhizal.crm.model.network.ContactMergeResolution
import com.mycorrhizal.crm.model.network.ContactSummary
import com.mycorrhizal.crm.ui.theme.MycorrhizalTheme
import org.junit.Assert.assertEquals
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35])
@GraphicsMode(GraphicsMode.Mode.NATIVE)
class MergeContactsScreenTest {

    @get:Rule
    val composeTestRule = createComposeRule()

    private fun setContent(
        uiState: MergeUiState,
        onPick: (ContactSummary) -> Unit = {},
        onResolve: (String, String) -> Unit = { _, _ -> },
    ) {
        composeTestRule.setContent {
            MycorrhizalTheme {
                MergeContactsScreenContent(
                    uiState = uiState,
                    onBack = {},
                    onPick = onPick,
                    onResolve = onResolve,
                )
            }
        }
    }

    @Test
    fun `the target picker is a search field, not a raw id box`() {
        setContent(MergeUiState(keepId = 1))

        // The reported gap was a "type the numeric ID" field; the M23 surface is search.
        composeTestRule.onNodeWithText("Pick the other contact").assertIsDisplayed()
    }

    @Test
    fun `typing a query surfaces search results and picking one forwards it`() {
        var picked: ContactSummary? = null
        setContent(
            MergeUiState(
                keepId = 1,
                searchQuery = "Dana",
                searchResults = listOf(
                    ContactSummary(id = 5, uid = "uid-5", fn = "Dana White"),
                    ContactSummary(id = 7, uid = "uid-7", fn = "Dana Brown"),
                ),
            ),
            onPick = { picked = it },
        )

        composeTestRule.onNodeWithText("Dana White").assertIsDisplayed()
        composeTestRule.onNodeWithText("Dana White").performClick()

        assertEquals(5, picked?.id)
    }

    @Test
    fun `the picked target name is shown under the picker`() {
        setContent(
            MergeUiState(
                keepId = 1,
                mergeId = 5,
                pickedOther = ContactSummary(id = 5, uid = "uid-5", fn = "Dana White"),
            ),
        )

        composeTestRule.onNodeWithText("Will merge Dana White into the kept contact.").assertIsDisplayed()
    }

    @Test
    fun `a picked target shows a spinner while the preview is in flight`() {
        setContent(
            MergeUiState(
                keepId = 1,
                mergeId = 5,
                pickedOther = ContactSummary(id = 5, uid = "uid-5", fn = "Dana White"),
                isLoading = true,
            ),
        )

        composeTestRule.onNodeWithTag("merge-preview-loading").assertIsDisplayed()
    }

    @Test
    fun `preview shows the full association breakdown beyond notes and edges`() {
        setContent(
            MergeUiState(
                keepId = 1,
                mergeId = 2,
                preview = ContactMergePreviewResponse(
                    keepId = 1, mergeId = 2,
                    resolution = ContactMergeResolution(),
                    associationCounts = ContactMergeAssociationCounts(
                        notes = 3,
                        relationshipEdges = 2,
                        circleMemberships = 1,
                        householdMemberships = 1,
                        tags = 4,
                        lifeEvents = 2,
                        fieldValues = 1,
                        contactSyncLinks = 1,
                        attachments = 1,
                    ),
                ),
            ),
        )

        composeTestRule.onNodeWithText("Will move to the kept contact:").performScrollTo().assertIsDisplayed()
        composeTestRule.onNodeWithText("3 notes").performScrollTo().assertIsDisplayed()
        composeTestRule.onNodeWithText("2 relationship edges").performScrollTo().assertIsDisplayed()
        // The categories this ticket said were missing from Android's breakdown:
        composeTestRule.onNodeWithText("1 circle memberships").performScrollTo().assertIsDisplayed()
        composeTestRule.onNodeWithText("1 household memberships").performScrollTo().assertIsDisplayed()
        composeTestRule.onNodeWithText("4 tags").performScrollTo().assertIsDisplayed()
        composeTestRule.onNodeWithText("2 life events").performScrollTo().assertIsDisplayed()
        composeTestRule.onNodeWithText("1 custom field values").performScrollTo().assertIsDisplayed()
        composeTestRule.onNodeWithText("1 sync links").performScrollTo().assertIsDisplayed()
        composeTestRule.onNodeWithText("1 attachments").performScrollTo().assertIsDisplayed()
    }

    @Test
    fun `a preview with nothing to move says so instead of a blank list`() {
        setContent(
            MergeUiState(
                keepId = 1,
                mergeId = 2,
                preview = ContactMergePreviewResponse(
                    keepId = 1, mergeId = 2,
                    resolution = ContactMergeResolution(),
                    associationCounts = ContactMergeAssociationCounts(),
                ),
            ),
        )

        composeTestRule.onNodeWithText("Nothing to move.").performScrollTo().assertIsDisplayed()
    }

    @Test
    fun `a conflict option is named by its value and toggles on tap`() {
        var resolvedField: String? = null
        var resolvedValue: String? = null
        setContent(
            MergeUiState(
                keepId = 1,
                mergeId = 2,
                preview = ContactMergePreviewResponse(
                    keepId = 1, mergeId = 2,
                    resolution = ContactMergeResolution(
                        conflicts = listOf(
                            ContactMergeFieldConflict(
                                field = "nickname",
                                label = "Nickname",
                                keeperValue = "Andy",
                                loserValue = "Drew",
                            ),
                        ),
                    ),
                    associationCounts = ContactMergeAssociationCounts(),
                ),
            ),
            onResolve = { field, value -> resolvedField = field; resolvedValue = value },
        )

        // #199: Modifier.toggleable on the row merges the option value into
        // the checkbox's own accessible name -- previously an unnamed
        // Checkbox sat in a row whose own .clickable carried no role/state at
        // all, so TalkBack announced neither.
        composeTestRule.onNodeWithText("Drew").performScrollTo().assertIsToggleable().assertIsOff().performClick()
        assertEquals("nickname", resolvedField)
        assertEquals("Drew", resolvedValue)
    }

    @Test
    fun `a preselected duplicate-review merge echoes the other party name`() {
        // T93: the duplicate review navigates here with BOTH contacts already
        // chosen, so the context line names the merge target even though the
        // search picker has nothing picked yet.
        composeTestRule.setContent {
            MycorrhizalTheme {
                MergeContactsScreenContent(
                    uiState = MergeUiState(keepId = 1, mergeId = 2),
                    onBack = {},
                    otherName = "Dan White",
                )
            }
        }

        composeTestRule.onNodeWithText("Will merge Dan White into the kept contact.").assertIsDisplayed()
    }
}
