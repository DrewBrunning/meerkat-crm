import EditIcon from '@mui/icons-material/Edit';
import { Box, Button, IconButton, Stack, Typography } from '@mui/material';
import { type ReactNode, useState } from 'react';
import { useTranslation } from 'react-i18next';

interface EditableArrayFieldProps<T> {
  icon: ReactNode;
  label: string;
  value: T;
  /** Renders the read-only view of the current value */
  renderDisplay: (value: T) => ReactNode;
  /** Renders the editor; receives the working draft and a setter */
  renderEditor: (draft: T, setDraft: (next: T) => void) => ReactNode;
  /** Deep-clones the value into an editable draft */
  cloneValue: (value: T) => T;
  onSave: (draft: T) => Promise<void>;
  // T88: lets a caller (ContactInformation's speakToAs row) react to this
  // field's own internal edit toggle -- e.g. to widen its grid span only
  // while editing. Optional: every other consumer ignores it.
  onEditingChange?: (editing: boolean) => void;
}

export default function EditableArrayField<T>({
  icon,
  label,
  value,
  renderDisplay,
  renderEditor,
  cloneValue,
  onSave,
  onEditingChange,
}: EditableArrayFieldProps<T>) {
  const { t } = useTranslation();
  const [editing, setEditingRaw] = useState(false);
  const [draft, setDraft] = useState<T>(value);
  const [saving, setSaving] = useState(false);

  const setEditing = (next: boolean) => {
    setEditingRaw(next);
    onEditingChange?.(next);
  };

  const startEdit = () => {
    setDraft(cloneValue(value));
    setEditing(true);
  };

  const cancel = () => {
    setEditing(false);
  };

  const save = async () => {
    try {
      setSaving(true);
      await onSave(draft);
      setEditing(false);
    } finally {
      setSaving(false);
    }
  };

  if (editing) {
    return (
      <Box>
        <Stack
          direction="row"
          spacing={1}
          sx={{
            alignItems: 'flex-start',
          }}
        >
          <Box sx={{ pt: 0.5 }}>{icon}</Box>
          <Box sx={{ flexGrow: 1 }}>
            {renderEditor(draft, setDraft)}
            <Stack direction="row" spacing={1} sx={{ mt: 1 }}>
              <Button size="small" variant="contained" onClick={save} disabled={saving}>
                {saving ? t('common.saving') : t('common.save')}
              </Button>
              <Button size="small" onClick={cancel} disabled={saving}>
                {t('common.cancel')}
              </Button>
            </Stack>
          </Box>
        </Stack>
      </Box>
    );
  }

  return (
    <Box
      sx={{
        display: 'flex',
        alignItems: 'flex-start',
        // #188 / #196: :focus-within reveals the pencil once the button itself
        // is focused, not just on mouse hover -- opacity:0 alone keeps a
        // keyboard user's focus on an invisible control. Mirrors
        // EditableField.tsx's wrapper.
        '&:hover .edit-button, &:focus-within .edit-button': { opacity: 1 },
      }}
    >
      {icon}
      <Box sx={{ flexGrow: 1, minWidth: 0 }}>
        {/* T63: see EditableField.tsx's matching comment -- same reasoning. */}
        {/* T109: the edit pencil sits beside the field-name label (matching
            Name and Circles/Tags, T89) instead of at the far right of the
            row -- see EditableField.tsx's matching comment. */}
        <Box sx={{ display: 'flex', alignItems: 'center' }}>
          <Typography
            variant="caption"
            sx={{
              color: 'text.secondary',
              fontFamily: '"IBM Plex Mono", monospace',
            }}
          >
            {label}
          </Typography>
          <IconButton
            className="edit-button"
            size="small"
            onClick={startEdit}
            aria-label={t('common.edit')}
            // #188 / #196: the tight p:0.25 padding measured 22x22, under the
            // 2.5.8 (AA) 24x24 minimum -- floor it. Mirrors EditableField.tsx.
            sx={{
              ml: 0.5,
              p: 0.25,
              opacity: 0,
              transition: 'opacity 0.2s',
              minWidth: 24,
              minHeight: 24,
            }}
          >
            <EditIcon sx={{ fontSize: 18 }} />
          </IconButton>
        </Box>
        <Box sx={{ overflowWrap: 'anywhere', wordBreak: 'break-word' }}>{renderDisplay(value)}</Box>
      </Box>
    </Box>
  );
}
