import CakeIcon from '@mui/icons-material/Cake';
import DeleteIcon from '@mui/icons-material/Delete';
import EditIcon from '@mui/icons-material/Edit';
import { Box, Chip, IconButton, Paper, Tooltip, Typography } from '@mui/material';
import { useTranslation } from 'react-i18next';
import type { Contact } from '../api/contacts';
import { type LifeEvent, partialDateDisplay } from '../api/lifeEvents';

interface LifeEventListProps {
  events: LifeEvent[];
  contactsByUid: Map<string, Contact>;
  onEdit: (event: LifeEvent) => void;
  onDelete: (id: string) => void;
}

export default function LifeEventList({
  events,
  contactsByUid,
  onEdit,
  onDelete,
}: LifeEventListProps) {
  const { t } = useTranslation();

  if (events.length === 0) {
    return (
      <Paper sx={{ p: 3, textAlign: 'center' }}>
        <Typography
          variant="body1"
          sx={{
            color: 'text.secondary',
          }}
        >
          {t('lifeEvent.empty')}
        </Typography>
      </Paper>
    );
  }

  return (
    <Box sx={{ display: 'flex', flexDirection: 'column', gap: 1.5 }}>
      {events.map((event) => (
        <Paper
          key={event.id}
          sx={{
            p: 2,
            // #196: :focus-within reveals the actions once a button inside
            // is keyboard-focused, not just on mouse hover.
            '&:hover .life-event-actions, &:focus-within .life-event-actions': { opacity: 1 },
          }}
        >
          <Box
            sx={{
              display: 'flex',
              justifyContent: 'space-between',
              alignItems: 'flex-start',
            }}
          >
            <Box>
              <Box
                sx={{
                  display: 'flex',
                  alignItems: 'center',
                  gap: 1,
                  mb: 0.5,
                }}
              >
                <CakeIcon fontSize="small" color="action" />
                {/* #211: a per-item title in a list, not a page heading. */}
                <Typography variant="subtitle2" component="p">
                  {t(`lifeEvent.types.${event.type}`, event.type)}
                </Typography>
                {event.category && (
                  <Chip
                    label={t(`lifeEvent.categories.${event.category}`, event.category)}
                    size="small"
                  />
                )}
                <Chip label={partialDateDisplay(event.date)} size="small" variant="outlined" />
                {event.remind && (
                  <Tooltip title={t('lifeEvent.remindTooltip')}>
                    <Chip label={t('lifeEvent.remind')} size="small" />
                  </Tooltip>
                )}
              </Box>
              {event.description && (
                <Typography
                  variant="body2"
                  sx={{
                    color: 'text.secondary',
                    whiteSpace: 'pre-wrap',
                    overflowWrap: 'anywhere',
                    mb: 0.5,
                  }}
                >
                  {event.description}
                </Typography>
              )}
              {event.related_entity_ids && event.related_entity_ids.length > 0 && (
                <Box
                  sx={{
                    display: 'flex',
                    flexWrap: 'wrap',
                    gap: 0.5,
                  }}
                >
                  {event.related_entity_ids.map((uid) => {
                    const c = contactsByUid.get(uid);
                    return (
                      <Chip
                        key={uid}
                        label={c ? `${c.firstname} ${c.lastname}` : uid}
                        size="small"
                        variant="outlined"
                      />
                    );
                  })}
                </Box>
              )}
            </Box>
            <Box
              className="life-event-actions"
              sx={{ opacity: 0, transition: 'opacity 0.2s', display: 'flex', gap: 1 }}
            >
              <IconButton size="small" onClick={() => onEdit(event)} aria-label={t('common.edit')}>
                <EditIcon fontSize="small" />
              </IconButton>
              <IconButton
                size="small"
                onClick={() => onDelete(event.id)}
                aria-label={t('common.delete')}
              >
                <DeleteIcon fontSize="small" />
              </IconButton>
            </Box>
          </Box>
        </Paper>
      ))}
    </Box>
  );
}
