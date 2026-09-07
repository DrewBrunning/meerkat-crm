import {
  Alert,
  Button,
  DialogActions,
  DialogContent,
  DialogTitle,
  Step,
  StepLabel,
  Stepper,
} from '@mui/material';
import type { ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { useDiscardGuard } from '../../hooks/useDiscardGuard';
import type {
  SourceImportStep,
  SourceImportWizard as Wizard,
} from '../../hooks/useSourceImportWizard';
import AppDialog from '../AppDialog';
import ConfirmDiscardDialog from '../ConfirmDiscardDialog';
import SourceImportProgress from './SourceImportProgress';
import SourceImportResult from './SourceImportResult';
import SourceImportReview from './SourceImportReview';

const STEP_ORDER: SourceImportStep[] = ['connect', 'fetching', 'review', 'importing', 'result'];

interface Props {
  open: boolean;
  onClose: () => void;
  // i18n key for the dialog title.
  titleKey: string;
  // Human name of the source ("Monica") for shared copy.
  sourceLabel: string;
  wizard: Wizard;
  // The source-specific step 0 (connect form + connect button). It calls
  // wizard.beginFetch(sessionId) once it has a session.
  connectStep: ReactNode;
  // Called after the user acknowledges the result screen.
  onComplete: () => void;
}

// The shared Dialog + Stepper shell for every source-import assistant
// (issue #549 Monica; issue #550 Meerkat reuses this with a file-upload
// connectStep). Steps 1–4 are fully generic and driven by the wizard hook.
export default function SourceImportWizard({
  open,
  onClose,
  titleKey,
  sourceLabel,
  wizard,
  connectStep,
  onComplete,
}: Props) {
  const { t } = useTranslation();
  const { step } = wizard;
  const activeStep = STEP_ORDER.indexOf(step);

  const handleCancel = () => {
    void wizard.cancel();
    wizard.reset();
    onClose();
  };

  const handleComplete = () => {
    wizard.reset();
    onComplete();
  };

  // Issue #557: 'fetching' and 'review' have a live backend session -- in
  // review's case, one the user may have spent real time configuring
  // per-row actions on. Cancelling from either discards it, so both routes
  // through the same confirm-before-discard guard the free-text dialogs use.
  // 'connect' has no session yet (beginFetch is what creates one) and
  // 'result' has nothing left to lose, so neither needs it.
  const hasLiveSession = step === 'fetching' || step === 'review';
  // Issue #557 + #805: the guard now also covers *in-app route navigation*
  // away from a live session -- the data-router blocker discards through the
  // same handleCancel the Cancel button uses (cancel the backend session,
  // reset the wizard), then lets the navigation go.
  const { guardedClose, confirmDialogProps, navigationGuardElement } = useDiscardGuard(
    hasLiveSession,
    {
      onNavigationDiscard: handleCancel,
    },
  );

  const handleCancelClick = () => {
    if (hasLiveSession) {
      guardedClose(handleCancel);
    } else {
      handleCancel();
    }
  };

  // Previously this Dialog's `onClose` was always `handleCancel`, so
  // pressing Escape mid-*import* cancelled the run in progress -- the same
  // silent-discard shape as the review-step bug above, just on the step that
  // already has its own explicit "Cancel import" vs "Close (keep running)"
  // buttons for exactly this decision. Escape there now matches "keep
  // running" instead of guessing "cancel".
  const handleDialogClose = () => {
    if (step === 'importing') {
      onClose();
      return;
    }
    handleCancelClick();
  };

  const photosPending = step === 'importing';

  return (
    <AppDialog open={open} onClose={handleDialogClose} maxWidth="md" fullWidth>
      <DialogTitle>{t(titleKey)}</DialogTitle>
      <DialogContent dividers>
        <Stepper activeStep={activeStep} sx={{ mb: 3 }}>
          {STEP_ORDER.map((key) => (
            <Step key={key}>
              <StepLabel>{t(`settings.sourceImport.steps.${key}`)}</StepLabel>
            </Step>
          ))}
        </Stepper>

        {wizard.error && (
          <Alert severity="error" sx={{ mb: 2 }}>
            {wizard.error}
          </Alert>
        )}

        {step === 'connect' && connectStep}
        {step === 'fetching' && (
          <SourceImportProgress status={wizard.status} sourceLabel={sourceLabel} />
        )}
        {step === 'review' && wizard.preview && (
          <SourceImportReview
            preview={wizard.preview}
            rowActions={wizard.rowActions}
            onRowAction={wizard.setRowAction}
            onSetAll={wizard.setAllActions}
          />
        )}
        {step === 'importing' && (
          <SourceImportProgress status={wizard.status} sourceLabel={sourceLabel} />
        )}
        {step === 'result' && wizard.result && (
          <SourceImportResult result={wizard.result} photosPending={photosPending} />
        )}
      </DialogContent>
      <DialogActions>
        {step === 'result' ? (
          <Button variant="contained" onClick={handleComplete}>
            {t('settings.sourceImport.done')}
          </Button>
        ) : step === 'importing' ? (
          <>
            <Button color="error" onClick={() => void wizard.cancelImport()} disabled={wizard.busy}>
              {t('settings.sourceImport.cancelImport')}
            </Button>
            <Button onClick={onClose}>{t('settings.sourceImport.closeKeepRunning')}</Button>
          </>
        ) : (
          <>
            <Button onClick={handleCancelClick}>{t('common.cancel')}</Button>
            {step === 'review' && (
              <Button
                variant="contained"
                onClick={() => void wizard.confirm()}
                disabled={wizard.busy}
              >
                {t('settings.sourceImport.confirmImport')}
              </Button>
            )}
          </>
        )}
      </DialogActions>
      {navigationGuardElement}
      <ConfirmDiscardDialog {...confirmDialogProps} />
    </AppDialog>
  );
}
