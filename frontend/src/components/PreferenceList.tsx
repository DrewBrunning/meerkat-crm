import DeleteIcon from '@mui/icons-material/Delete';
import EditIcon from '@mui/icons-material/Edit';
import { Box, Chip, IconButton, Paper, Stack, Typography } from '@mui/material';
import { Fragment } from 'react';
import { useTranslation } from 'react-i18next';
import {
  PREFERENCE_CATEGORY_CONFIG,
  type Preference,
  type PreferenceSection,
} from '../api/preferences';

interface PreferenceListProps {
  preferences: Preference[];
  onEdit: (preference: Preference) => void;
  onDelete: (id: string) => void;
}

// Sections mirror the dialog's category groups (PREFERENCE_CATEGORY_CONFIG),
// so a category's section membership is defined in exactly one place.
// Anything not in the config (legacy categories, free-typed ones) falls
// through to "Other" so no data ever hides.
const CATEGORY_TO_SECTION: Record<string, PreferenceSection> = Object.fromEntries(
  PREFERENCE_CATEGORY_CONFIG.map((c) => [c.category, c.section]),
);
const SECTION_ORDER: PreferenceSection[] = [
  'foodDrink',
  'media',
  'hobby',
  'jewelry',
  'giftPreferences',
  'giftAvoid',
];

export default function PreferenceList({ preferences, onEdit, onDelete }: PreferenceListProps) {
  const { t } = useTranslation();

  if (preferences.length === 0) {
    return (
      <Typography
        variant="body2"
        sx={{
          color: 'text.secondary',
          py: 2,
          textAlign: 'center',
        }}
      >
        {t('preference.empty')}
      </Typography>
    );
  }

  const bySection: Record<PreferenceSection, Preference[]> = {
    foodDrink: [],
    media: [],
    hobby: [],
    jewelry: [],
    giftPreferences: [],
    giftAvoid: [],
  };
  const other: Preference[] = [];
  for (const p of preferences) {
    const section = CATEGORY_TO_SECTION[p.category];
    if (section) bySection[section].push(p);
    else other.push(p);
  }

  const renderItem = (pref: Preference) => (
    <Paper
      key={pref.id}
      variant="outlined"
      sx={{
        p: 2,
        // #196: :focus-within reveals the actions once a button inside is
        // keyboard-focused, not just on mouse hover -- see EditableField.tsx's
        // matching comment.
        '&:hover .preference-actions, &:focus-within .preference-actions': { opacity: 1 },
      }}
    >
      <Box sx={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start' }}>
        <Box sx={{ flex: 1 }}>
          <Box sx={{ display: 'flex', alignItems: 'center', gap: 1, mb: 0.5, flexWrap: 'wrap' }}>
            <Chip label={t(`preference.categories.${pref.category}`, pref.category)} size="small" />
            {pref.key && <Chip label={t(`preference.keys.${pref.key}`, pref.key)} size="small" />}
            {pref.sensitivity !== 'normal' && (
              <Chip
                label={t(`preference.sensitivities.${pref.sensitivity}`)}
                size="small"
                sx={{ height: 18 }}
              />
            )}
          </Box>
          <Typography variant="body1" sx={{ overflowWrap: 'anywhere' }}>
            {pref.value}
          </Typography>
          {pref.notes && (
            <Typography
              variant="body2"
              sx={{
                color: 'text.secondary',
                overflowWrap: 'anywhere',
                mt: 0.25,
              }}
            >
              {pref.notes}
            </Typography>
          )}
        </Box>
        <Box
          className="preference-actions"
          sx={{ display: 'flex', gap: 0.5, opacity: 0, transition: 'opacity 0.2s ease-in-out' }}
        >
          <IconButton size="small" onClick={() => onEdit(pref)} aria-label={t('common.edit')}>
            <EditIcon fontSize="small" />
          </IconButton>
          <IconButton
            size="small"
            color="error"
            onClick={() => onDelete(pref.id)}
            aria-label={t('common.delete')}
          >
            <DeleteIcon fontSize="small" />
          </IconButton>
        </Box>
      </Box>
    </Paper>
  );

  const renderSection = (label: string, items: Preference[]) => {
    if (items.length === 0) return null;
    return (
      <Box>
        <Typography
          variant="overline"
          sx={{
            color: 'text.secondary',
            letterSpacing: 0.08,
            fontSize: '0.72rem',
          }}
        >
          {label}
        </Typography>
        <Stack spacing={1.5} sx={{ mt: 0.5 }}>
          {items.map(renderItem)}
        </Stack>
      </Box>
    );
  };

  return (
    <Stack spacing={2.5}>
      {SECTION_ORDER.map((section) => (
        <Fragment key={section}>
          {renderSection(t(`preference.sections.${section}`), bySection[section])}
        </Fragment>
      ))}
      {renderSection(t('preference.sections.other'), other)}
    </Stack>
  );
}
