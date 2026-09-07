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
import { handleFetchError } from '../utils/errorHandler';
import AppDialog from './AppDialog';
import ConfirmDiscardDialog from './ConfirmDiscardDialog';

interface ContactBrief {
  ID: number;
  firstname: string;
  lastname: string;
  nickname?: string;
}

function toContactBrief(c: Contact): ContactBrief {
  return { ID: c.ID, firstname: c.firstname, lastname: c.lastname, nickname: c.nickname };
}

function contactLabel(contact: ContactBrief): string {
  if (contact.nickname) {
    return `${contact.firstname} "${contact.nickname}" ${contact.lastname}`;
  }
  return `${contact.firstname} ${contact.lastname}`;
}

interface AddNoteDialogProps {
  open: boolean;
  onClose: () => void;
  onSave: (content: string, date: string, contactId?: number) => Promise<void>;
  noteContactId?: number;
  noteContactName?: string;
}

interface NoteDraft {
  content: string;
  date: string;
  selectedContact: ContactBrief | null;
}

export default function AddNoteDialog({
  open,
  onClose,
  onSave,
  noteContactId,
  noteContactName,
}: AddNoteDialogProps) {
  const { t } = useTranslation();
  const [content, setContent] = useState('');
  const [date, setDate] = useState(new Date().toISOString().split('T')[0]);
  const [error, setError] = useState('');
  const [saving, setSaving] = useState(false);

  const lockedContact = noteContactId != null;
  // Issue #557: scoped per target contact (or "unassigned" for the global
  // notes page) so drafts for different contacts never bleed into each
  // other.
  const draftKey = `note-dialog:${noteContactId ?? 'unassigned'}`;

  const [contacts, setContacts] = useState<ContactBrief[]>([]);
  const [contactsLoading, setContactsLoading] = useState(false);
  const [searchInput, setSearchInput] = useState('');
  const [selectedContact, setSelectedContact] = useState<ContactBrief | null>(null);

  // Restore a draft left behind by a session expiry, a crash, or an
  // accidental tab close -- once per dialog open, before anything else can
  // overwrite it.
  useEffect(() => {
    if (!open) return;
    const draft = readSessionDraft<NoteDraft>(draftKey);
    if (!draft) return;
    setContent(draft.content);
    setDate(draft.date);
    if (!lockedContact) setSelectedContact(draft.selectedContact);
  }, [open, draftKey, lockedContact]);

  const isDirty = content.trim().length > 0 || (!lockedContact && selectedContact !== null);
  const { clearDraft } = useSessionDraft(
    draftKey,
    { content, date, selectedContact },
    open && isDirty,
  );
  // Clears the draft too -- reached after a successful save and after a
  // confirmed discard (a Cancel/Escape close or a blocked in-app navigation),
  // and in every case the draft is no longer wanted.
  const handleClose = useCallback(() => {
    clearDraft();
    setContent('');
    setDate(new Date().toISOString().split('T')[0]);
    setError('');
    setSelectedContact(null);
    setSearchInput('');
    setContacts([]);
    onClose();
  }, [clearDraft, onClose]);
  // Issue #557 + #805: one discard guard covers every way of leaving a dirty
  // note -- Cancel/Escape (guardedClose), tab close/reload/external nav
  // (beforeunload), and in-app route navigation (the data-router blocker,
  // which discards through handleClose before letting the navigation go).
  const { guardedClose, confirmDialogProps, navigationGuardElement } = useDiscardGuard(isDirty, {
    onNavigationDiscard: handleClose,
  });

  const loadContacts = useCallback(async (search: string = '') => {
    setContactsLoading(true);
    try {
      const response = await getContacts({ limit: 40, search });
      setContacts((response.contacts || []).map(toContactBrief));
    } catch (err) {
      handleFetchError(err, 'loading contacts');
    } finally {
      setContactsLoading(false);
    }
  }, []);

  useEffect(() => {
    if (!open) return;
    const timeoutId = setTimeout(() => {
      loadContacts(searchInput);
    }, 300);
    return () => clearTimeout(timeoutId);
  }, [searchInput, open, loadContacts]);

  useEffect(() => {
    if (open) {
      loadContacts();
    }
  }, [open, loadContacts]);

  const handleSave = async () => {
    if (!content.trim()) {
      setError(t('noteDialog.required'));
      return;
    }

    setSaving(true);
    try {
      await onSave(content, date, lockedContact ? noteContactId : selectedContact?.ID);
      handleClose();
    } catch {
      setError(t('noteDialog.saveError'));
    } finally {
      setSaving(false);
    }
  };

  // Issue #557: Cancel and Escape (AppDialog already blocks a backdrop
  // click for every dialog) go through the discard guard -- a dirty note
  // gets a confirmation instead of vanishing.
  const handleRequestClose = () => guardedClose(handleClose);

  return (
    <AppDialog open={open} onClose={handleRequestClose} maxWidth="sm" fullWidth>
      <DialogTitle>{t('noteDialog.title')}</DialogTitle>
      <DialogContent>
        <Box sx={{ pt: 1, display: 'flex', flexDirection: 'column', gap: 2 }}>
          {lockedContact ? (
            <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
              <Chip label={noteContactName} size="small" color="primary" />
            </Box>
          ) : (
            <Autocomplete
              options={contacts}
              getOptionLabel={contactLabel}
              value={selectedContact}
              onChange={(_, value) => setSelectedContact(value)}
              onInputChange={(_, value, reason) => {
                if (reason === 'input') setSearchInput(value);
              }}
              loading={contactsLoading}
              filterOptions={(x) => x}
              renderInput={(params) => (
                <TextField
                  {...params}
                  label={t('notes.assignContact')}
                  placeholder={t('notes.searchContacts')}
                  slotProps={{
                    ...params.slotProps,

                    input: {
                      ...params.slotProps.input,
                      endAdornment: (
                        <>
                          {contactsLoading ? <CircularProgress color="inherit" size={20} /> : null}
                          {params.slotProps.input.endAdornment}
                        </>
                      ),
                    },
                  }}
                />
              )}
              isOptionEqualToValue={(option, value) => option.ID === value.ID}
              noOptionsText={searchInput ? t('notes.noContactsFound') : t('notes.typeToSearch')}
            />
          )}
          <TextField
            label={t('noteDialog.content')}
            placeholder={t('noteDialog.contentPlaceholder')}
            multiline
            rows={4}
            value={content}
            onChange={(e) => {
              setContent(e.target.value);
              setError('');
            }}
            error={!!error}
            helperText={error}
            fullWidth
            required
            autoFocus
          />
          <TextField
            label={t('noteDialog.date')}
            type="date"
            value={date}
            onChange={(e) => setDate(e.target.value)}
            fullWidth
            slotProps={{
              inputLabel: {
                shrink: true,
              },
            }}
          />
        </Box>
      </DialogContent>
      <DialogActions>
        <Button onClick={handleRequestClose} disabled={saving}>
          {t('noteDialog.cancel')}
        </Button>
        <Button onClick={handleSave} variant="contained" disabled={saving}>
          {t('noteDialog.save')}
        </Button>
      </DialogActions>
      {navigationGuardElement}
      <ConfirmDiscardDialog {...confirmDialogProps} />
    </AppDialog>
  );
}
