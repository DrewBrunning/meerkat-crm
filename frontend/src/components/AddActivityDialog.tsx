import {
  Autocomplete,
  Box,
  Button,
  Chip,
  CircularProgress,
  DialogActions,
  DialogContent,
  DialogTitle,
  TextField,
} from '@mui/material';
import { useCallback, useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { type Contact, getContacts } from '../api/contacts';
import { useDiscardGuard } from '../hooks/useDiscardGuard';
import { readSessionDraft, useSessionDraft } from '../hooks/useSessionDraft';
import AppDialog from './AppDialog';
import ConfirmDiscardDialog from './ConfirmDiscardDialog';

interface AddActivityDialogProps {
  open: boolean;
  onClose: () => void;
  onSave: (activity: {
    title: string;
    description: string;
    location: string;
    date: string;
    contact_ids: number[];
  }) => Promise<void>;
  preselectedContactId?: number;
}

interface ActivityDraft {
  title: string;
  description: string;
  location: string;
  date: string;
}

export default function AddActivityDialog({
  open,
  onClose,
  onSave,
  preselectedContactId,
}: AddActivityDialogProps) {
  const { t } = useTranslation();
  const [title, setTitle] = useState('');
  const [description, setDescription] = useState('');
  const [location, setLocation] = useState('');
  const [date, setDate] = useState(new Date().toISOString().split('T')[0]);
  const [selectedContacts, setSelectedContacts] = useState<Contact[]>([]);
  const [contacts, setContacts] = useState<Contact[]>([]);
  const [searchInput, setSearchInput] = useState('');
  const [error, setError] = useState('');
  const [saving, setSaving] = useState(false);
  const [loading, setLoading] = useState(false);

  // Issue #557: scoped per preselected contact so a draft started from one
  // contact's page never bleeds into another's. The selected-contacts list
  // itself is deliberately not part of the draft -- when preselectedContactId
  // is set, it's loaded async and would otherwise race a restored draft and
  // silently clobber it; title/description/location are the fields with real
  // loss cost anyway.
  const draftKey = `activity-dialog:${preselectedContactId ?? 'unassigned'}`;

  useEffect(() => {
    if (!open) return;
    const draft = readSessionDraft<ActivityDraft>(draftKey);
    if (!draft) return;
    setTitle(draft.title);
    setDescription(draft.description);
    setLocation(draft.location);
    setDate(draft.date);
  }, [open, draftKey]);

  const isDirty = Boolean(title.trim() || description.trim() || location.trim());
  const { clearDraft } = useSessionDraft(
    draftKey,
    { title, description, location, date },
    open && isDirty,
  );
  // Clears the draft too -- reached after a successful save and after a
  // confirmed discard (a Cancel/Escape close or a blocked in-app navigation),
  // and in every case the draft is no longer wanted.
  const handleClose = useCallback(() => {
    clearDraft();
    setTitle('');
    setDescription('');
    setLocation('');
    setDate(new Date().toISOString().split('T')[0]);
    setSelectedContacts([]);
    setSearchInput('');
    setContacts([]);
    setError('');
    onClose();
  }, [clearDraft, onClose]);
  // Issue #557 + #805: one discard guard covers every way of leaving a dirty
  // activity -- Cancel/Escape (guardedClose), tab close/reload/external nav
  // (beforeunload), and in-app route navigation (the data-router blocker,
  // which discards through handleClose before letting the navigation go).
  const { guardedClose, confirmDialogProps, navigationGuardElement } = useDiscardGuard(isDirty, {
    onNavigationDiscard: handleClose,
  });

  const loadContacts = useCallback(async (search: string = '') => {
    setLoading(true);
    try {
      const response = await getContacts({ limit: 100, search });
      setContacts(response.contacts || []);
    } catch (err) {
      console.error('Failed to fetch contacts:', err);
    } finally {
      setLoading(false);
    }
  }, []);

  // Load preselected contact on open
  useEffect(() => {
    if (open && preselectedContactId) {
      const loadPreselected = async () => {
        try {
          const response = await getContacts({ limit: 100 });
          const preselected = response.contacts.find((c: Contact) => c.ID === preselectedContactId);
          if (preselected) {
            setSelectedContacts([preselected]);
          }
          setContacts(response.contacts || []);
        } catch (err) {
          console.error('Failed to load preselected contact:', err);
        }
      };
      loadPreselected();
    } else if (open) {
      loadContacts('');
    }
  }, [open, preselectedContactId, loadContacts]);

  // Debounced search effect
  useEffect(() => {
    if (!open) return;

    const timeoutId = setTimeout(() => {
      loadContacts(searchInput);
    }, 300);

    return () => clearTimeout(timeoutId);
  }, [searchInput, open, loadContacts]);

  const handleSave = async () => {
    if (!title.trim() || !date) {
      setError(t('activityDialog.required'));
      return;
    }

    setSaving(true);
    try {
      await onSave({
        title,
        description,
        location,
        date,
        contact_ids: selectedContacts.map((c) => c.ID),
      });
      handleClose();
    } catch {
      setError('Failed to save activity');
    } finally {
      setSaving(false);
    }
  };

  // Issue #557: Cancel and Escape (AppDialog already blocks a backdrop
  // click for every dialog) go through the discard guard -- a dirty
  // activity gets a confirmation instead of vanishing.
  const handleRequestClose = () => guardedClose(handleClose);

  const getContactLabel = (contact: Contact) => {
    return `${contact.firstname}${contact.nickname ? ` "${contact.nickname}"` : ''} ${contact.lastname}`;
  };

  return (
    <AppDialog open={open} onClose={handleRequestClose} maxWidth="sm" fullWidth>
      <DialogTitle>{t('activityDialog.title')}</DialogTitle>
      <DialogContent>
        <Box sx={{ pt: 1, display: 'flex', flexDirection: 'column', gap: 2 }}>
          <TextField
            label={t('activityDialog.activityTitle')}
            placeholder={t('activityDialog.titlePlaceholder')}
            value={title}
            onChange={(e) => {
              setTitle(e.target.value);
              setError('');
            }}
            error={!!error}
            helperText={error}
            fullWidth
            required
            autoFocus
          />

          <TextField
            label={t('activityDialog.description')}
            placeholder={t('activityDialog.descriptionPlaceholder')}
            multiline
            rows={3}
            value={description}
            onChange={(e) => setDescription(e.target.value)}
            fullWidth
          />

          <TextField
            label={t('activityDialog.location')}
            placeholder={t('activityDialog.locationPlaceholder')}
            value={location}
            onChange={(e) => setLocation(e.target.value)}
            fullWidth
          />

          <TextField
            label={t('activityDialog.date')}
            type="date"
            value={date}
            onChange={(e) => setDate(e.target.value)}
            fullWidth
            required
            slotProps={{
              inputLabel: {
                shrink: true,
              },
            }}
          />

          <Autocomplete
            multiple
            options={contacts}
            getOptionLabel={getContactLabel}
            value={selectedContacts}
            onChange={(_, newValue) => setSelectedContacts(newValue)}
            onInputChange={(_, value) => setSearchInput(value)}
            inputValue={searchInput}
            loading={loading}
            filterOptions={(x) => x}
            isOptionEqualToValue={(option, value) => option.ID === value.ID}
            renderInput={(params) => (
              <TextField
                {...params}
                label={t('activityDialog.contacts')}
                placeholder={t('activityDialog.selectContacts')}
                slotProps={{
                  ...params.slotProps,

                  input: {
                    ...params.slotProps.input,
                    endAdornment: (
                      <>
                        {loading ? <CircularProgress color="inherit" size={20} /> : null}
                        {params.slotProps.input.endAdornment}
                      </>
                    ),
                  },
                }}
              />
            )}
            renderValue={(value, getItemProps) =>
              value.map((contact, index) => (
                <Chip
                  label={getContactLabel(contact)}
                  {...getItemProps({ index })}
                  key={contact.ID}
                />
              ))
            }
          />
        </Box>
      </DialogContent>
      <DialogActions>
        <Button onClick={handleRequestClose} disabled={saving}>
          {t('activityDialog.cancel')}
        </Button>
        <Button onClick={handleSave} variant="contained" disabled={saving}>
          {t('activityDialog.save')}
        </Button>
      </DialogActions>
      {navigationGuardElement}
      <ConfirmDiscardDialog {...confirmDialogProps} />
    </AppDialog>
  );
}
