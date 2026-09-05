package com.mycorrhizal.crm

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.mycorrhizal.crm.data.session.AppLockController
import com.mycorrhizal.crm.data.session.AppLockState
import com.mycorrhizal.crm.data.session.SessionManager
import com.mycorrhizal.crm.domain.repository.SessionState
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.stateIn
import javax.inject.Inject

/**
 * Root application state: whether a session exists (drives login vs main tree)
 * and whether that session must pass the local app-lock gate first (issue
 * #722). `appLockState` is only meaningful once [AppLockState.Resolving] has
 * cleared — the root never renders the authenticated tree while it is pending.
 */
@HiltViewModel
class MainViewModel @Inject constructor(
    sessionManager: SessionManager,
    appLockController: AppLockController,
) : ViewModel() {
    val session: StateFlow<SessionState> = sessionManager.observeSession()
        .stateIn(viewModelScope, SharingStarted.Eagerly, SessionState())

    val appLockState: StateFlow<AppLockState> = appLockController.state
        .stateIn(viewModelScope, SharingStarted.Eagerly, AppLockState.Resolving)
}
