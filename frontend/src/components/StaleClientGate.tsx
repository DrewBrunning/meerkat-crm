import {
  Button,
  DialogActions,
  DialogContent,
  DialogTitle,
  Stack,
  Typography,
} from '@mui/material';
import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  type BlockNotice,
  dismissBlockedNotice,
  onStaleClientNotice,
  reloadBlockedClient,
} from '../staleClient/detector';
import AppDialog from './AppDialog';

// StaleClientGate is the blocking half of the WEB-01 (issue #475) mechanism.
// When the stale-client detector decides this tab's build is genuinely
// incompatible with the server (below its declared floor, or speaking a
// different api_contract_version), a reload is required — this is the web
// equivalent of Android's force-update screen.
//
// Three shapes, mirroring the severity ladder in docs/client-compatibility-
// policy.md:
//
//   - clean + a reload already triggered automatically -> render nothing; the
//     navigation ends the page and a dialog would only flash.
//   - clean but the one automatic attempt this session already ran and did not
//     converge -> a non-dismissible dialog with a single "Reload now" action.
//     The auto-loop is intentionally broken here: the server keeps advertising
//     an unsatisfiable floor, so the user gets a clear message and a button,
//     not an endless silent reload.
//   - a form is dirty -> a consent dialog. Losing what the user is typing is
//     data loss (recommended action #6), so "Reload now" is explicit; "Later"
//     suppresses the block for a while and the detector resumes once the
//     dialog is closed or saved.
//
// Mounted at the root (src/index.tsx) outside the routed <ErrorBoundary>,
// like <SessionExpiredGate>, so a crashed page cannot take the gate down.
export default function StaleClientGate() {
  const { t } = useTranslation();
  const [notice, setNotice] = useState<BlockNotice>({ kind: 'none' });

  useEffect(() => onStaleClientNotice(setNotice), []);

  if (notice.kind !== 'blocked') {
    return null;
  }
  if (notice.autoReloading && !notice.dirty) {
    // The automatic reload is in flight; the navigation ends this page.
    return null;
  }

  const dirty = notice.dirty;

  return (
    <AppDialog
      open
      maxWidth="sm"
      fullWidth
      // Non-dismissible unless there is unsaved work to protect: the clean
      // block cannot be waved away, because the server has stopped serving
      // this client. The dirty block keeps Escape/backdrop as "Later".
      onClose={dirty ? dismissBlockedNotice : undefined}
      aria-labelledby="stale-client-blocked-title"
    >
      <DialogTitle id="stale-client-blocked-title">{t('app.incompatible.title')}</DialogTitle>
      <DialogContent>
        <Stack spacing={2}>
          <Typography variant="body2">
            {t(dirty ? 'app.incompatible.unsavedMessage' : 'app.incompatible.blockedMessage')}
          </Typography>
        </Stack>
      </DialogContent>
      <DialogActions>
        {dirty && <Button onClick={dismissBlockedNotice}>{t('app.incompatible.later')}</Button>}
        <Button variant="contained" onClick={reloadBlockedClient}>
          {t('app.incompatible.reload')}
        </Button>
      </DialogActions>
    </AppDialog>
  );
}
