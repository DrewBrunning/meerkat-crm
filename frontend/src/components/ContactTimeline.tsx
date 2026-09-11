import { mdiLinkVariant, mdiNoteOutline } from '@mdi/js';
import CakeIcon from '@mui/icons-material/Cake';
import DeleteIcon from '@mui/icons-material/Delete';
import EditIcon from '@mui/icons-material/Edit';
import EventIcon from '@mui/icons-material/Event';
import NotificationsIcon from '@mui/icons-material/Notifications';
import RedeemIcon from '@mui/icons-material/Redeem';
import {
  Timeline,
  TimelineConnector,
  TimelineContent,
  TimelineDot,
  TimelineItem,
  TimelineOppositeContent,
  TimelineSeparator,
} from '@mui/lab';
import { Box, IconButton, Link, Paper, SvgIcon, Typography } from '@mui/material';
import { Fragment } from 'react';
import { useTranslation } from 'react-i18next';
import { useNavigate } from 'react-router';
import type { Activity } from '../api/activities';
import type { ExternalActivity } from '../api/externalLinks';
import type { Gift } from '../api/gifts';
import { type LifeEvent, partialDateDisplay } from '../api/lifeEvents';
import type { Note } from '../api/notes';
import type { ReminderCompletion } from '../api/reminders';
import { useDateFormat } from '../DateFormatProvider';

interface ContactTimelineProps {
  timelineItems: Array<{
    type: 'note' | 'activity' | 'completion' | 'life_event' | 'external_activity' | 'gift';
    data: Note | Activity | ReminderCompletion | LifeEvent | ExternalActivity | Gift;
    date: string;
  }>;
  onEditItem: (type: 'note' | 'activity', item: Note | Activity) => void;
  onDeleteCompletion?: (completionId: number) => void;
  // T78: override for the empty-state message. The default reads "no notes
  // or activities yet", which is wrong for the filtered explorer -- a
  // contact can have plenty of events while a given type/bucket filter
  // matches none.
  emptyText?: string;
}

function isLifeEvent(item: {
  type: string;
  data: unknown;
}): item is { type: 'life_event'; data: LifeEvent } {
  return item.type === 'life_event';
}

function isGift(item: { type: string; data: unknown }): item is { type: 'gift'; data: Gift } {
  return item.type === 'gift';
}

function isExternalActivity(item: {
  type: string;
  data: unknown;
}): item is { type: 'external_activity'; data: ExternalActivity } {
  return item.type === 'external_activity';
}

export default function ContactTimeline({
  timelineItems,
  onEditItem,
  onDeleteCompletion,
  emptyText,
}: ContactTimelineProps) {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { formatDate } = useDateFormat();

  if (timelineItems.length === 0) {
    return (
      <Typography
        variant="body2"
        sx={{
          color: 'text.secondary',
        }}
      >
        {emptyText ?? t('contactDetail.noActivity')}
      </Typography>
    );
  }

  return (
    <Timeline position="right">
      {timelineItems.map((item, index) => {
        const itemDate = new Date(item.date);
        const isValidDate = !Number.isNaN(itemDate.getTime());

        if (isLifeEvent(item)) {
          const event = item.data;
          return (
            <TimelineItem key={`life_event-${event.id}`}>
              <TimelineOppositeContent
                sx={{
                  color: 'text.secondary',
                  flex: 0.3,
                }}
              >
                <Typography variant="caption">{partialDateDisplay(event.date)}</Typography>
              </TimelineOppositeContent>
              <TimelineSeparator>
                <TimelineDot color="warning">
                  <CakeIcon fontSize="small" />
                </TimelineDot>
                {index < timelineItems.length - 1 && <TimelineConnector />}
              </TimelineSeparator>
              <TimelineContent>
                <Paper elevation={1} sx={{ p: 1.5 }}>
                  <Typography variant="subtitle1" component="p" sx={{ fontWeight: 500 }}>
                    {t(`lifeEvent.types.${event.type}`, event.type)}
                  </Typography>
                  {event.description && (
                    <Typography variant="body2" sx={{ mt: 0.5, overflowWrap: 'anywhere' }}>
                      {event.description}
                    </Typography>
                  )}
                </Paper>
              </TimelineContent>
            </TimelineItem>
          );
        }

        if (isGift(item)) {
          const gift = item.data;
          return (
            <TimelineItem key={`gift-${gift.id}`}>
              <TimelineOppositeContent
                sx={{
                  color: 'text.secondary',
                  flex: 0.3,
                }}
              >
                <Typography variant="caption">
                  {isValidDate ? formatDate(item.date) : item.date || 'N/A'}
                </Typography>
              </TimelineOppositeContent>
              <TimelineSeparator>
                <TimelineDot color="secondary">
                  <RedeemIcon fontSize="small" />
                </TimelineDot>
                {index < timelineItems.length - 1 && <TimelineConnector />}
              </TimelineSeparator>
              <TimelineContent>
                <Paper elevation={1} sx={{ p: 1.5 }}>
                  <Typography variant="subtitle1" component="p" sx={{ fontWeight: 500 }}>
                    {t(`timeline.gift.${gift.status}`, gift.status)}
                  </Typography>
                  <Typography variant="body2" sx={{ mt: 0.5, overflowWrap: 'anywhere' }}>
                    {gift.description}
                  </Typography>
                  {gift.occasion && (
                    <Typography
                      variant="caption"
                      sx={{
                        color: 'text.secondary',
                        display: 'block',
                        mt: 0.5,
                      }}
                    >
                      {gift.occasion}
                    </Typography>
                  )}
                </Paper>
              </TimelineContent>
            </TimelineItem>
          );
        }

        if (isExternalActivity(item)) {
          const event = item.data;
          const personName =
            (event.payload?.person_name as string | undefined) ||
            (event.payload?.name as string | undefined) ||
            event.source_system;
          return (
            <TimelineItem key={`external_activity-${event.id}`}>
              <TimelineOppositeContent
                sx={{
                  color: 'text.secondary',
                  flex: 0.3,
                }}
              >
                <Typography variant="caption">
                  {isValidDate ? formatDate(item.date) : item.date || 'N/A'}
                </Typography>
              </TimelineOppositeContent>
              <TimelineSeparator>
                <TimelineDot color="info">
                  <SvgIcon fontSize="small">
                    <path d={mdiLinkVariant} />
                  </SvgIcon>
                </TimelineDot>
                {index < timelineItems.length - 1 && <TimelineConnector />}
              </TimelineSeparator>
              <TimelineContent>
                <Paper elevation={1} sx={{ p: 1.5 }}>
                  <Typography variant="subtitle1" component="p" sx={{ fontWeight: 500 }}>
                    {event.type === 'photo-appearance'
                      ? t('externalLinks.activity.photoAppearance')
                      : t('externalLinks.activity.unknown', { type: event.type })}
                  </Typography>
                  <Typography variant="body2" sx={{ mt: 0.5, overflowWrap: 'anywhere' }}>
                    {t('externalLinks.activity.fromSystem', {
                      system: event.source_system,
                      name: personName,
                    })}
                  </Typography>
                </Paper>
              </TimelineContent>
            </TimelineItem>
          );
        }

        return (
          <TimelineItem
            key={`${item.type}-${(item.data as Note | Activity | ReminderCompletion).ID}`}
          >
            <TimelineOppositeContent
              sx={{
                color: 'text.secondary',
                flex: 0.3,
              }}
            >
              <Typography variant="caption">
                {isValidDate ? formatDate(item.date) : item.date || 'N/A'}
              </Typography>
            </TimelineOppositeContent>
            <TimelineSeparator>
              <TimelineDot
                color={
                  item.type === 'note'
                    ? 'primary'
                    : item.type === 'activity'
                      ? 'secondary'
                      : 'success'
                }
              >
                {item.type === 'note' ? (
                  <SvgIcon fontSize="small">
                    <path d={mdiNoteOutline} />
                  </SvgIcon>
                ) : item.type === 'activity' ? (
                  <EventIcon fontSize="small" />
                ) : (
                  <NotificationsIcon fontSize="small" />
                )}
              </TimelineDot>
              {index < timelineItems.length - 1 && <TimelineConnector />}
            </TimelineSeparator>
            <TimelineContent>
              <Paper
                elevation={1}
                sx={{
                  p: 1.5,
                  position: 'relative',
                  // #196: :focus-within reveals the icon once it is
                  // keyboard-focused, not just on mouse hover.
                  '&:hover .action-icon, &:focus-within .action-icon': {
                    opacity: 1,
                  },
                }}
              >
                <Typography
                  variant="subtitle1"
                  component="p"
                  sx={{ fontWeight: 500, overflowWrap: 'anywhere' }}
                >
                  {item.type === 'note'
                    ? t('contactDetail.note')
                    : item.type === 'activity'
                      ? (item.data as Activity).title
                      : t('timeline.reminderCompleted')}
                </Typography>
                <Typography variant="body2" sx={{ mt: 1, overflowWrap: 'anywhere' }}>
                  {item.type === 'note'
                    ? (item.data as Note).content
                    : item.type === 'activity'
                      ? (item.data as Activity).description || t('contactDetail.noDescription')
                      : (item.data as ReminderCompletion).message}
                </Typography>
                {item.type === 'activity' && (item.data as Activity).location && (
                  <Typography
                    variant="caption"
                    sx={{
                      color: 'text.secondary',
                      mt: 1,
                      display: 'block',
                      overflowWrap: 'anywhere',
                    }}
                  >
                    📍 {(item.data as Activity).location}
                  </Typography>
                )}
                {item.type === 'activity' &&
                  (item.data as Activity).contacts &&
                  (item.data as Activity).contacts!.length > 0 && (
                    <Box sx={{ mt: 1.5, display: 'flex', flexWrap: 'wrap', alignItems: 'center' }}>
                      <Typography
                        variant="caption"
                        sx={{
                          color: 'text.secondary',
                          mr: 0.5,
                        }}
                      >
                        👥
                      </Typography>
                      {(item.data as Activity).contacts!.map((activityContact, idx) => (
                        <Fragment key={activityContact.ID}>
                          <Link
                            component="button"
                            variant="caption"
                            onClick={(e) => {
                              e.preventDefault();
                              navigate(`/contacts/${activityContact.ID}`);
                            }}
                            sx={{
                              cursor: 'pointer',
                              textDecoration: 'none',
                              '&:hover': { textDecoration: 'underline' },
                            }}
                          >
                            {activityContact.firstname}{' '}
                            {activityContact.nickname ? `"${activityContact.nickname}" ` : ''}
                            {activityContact.lastname}
                          </Link>
                          {idx < (item.data as Activity).contacts!.length - 1 && (
                            <Typography
                              variant="caption"
                              sx={{
                                color: 'text.secondary',
                              }}
                            >
                              ,&nbsp;
                            </Typography>
                          )}
                        </Fragment>
                      ))}
                    </Box>
                  )}

                {item.type === 'completion' ? (
                  <IconButton
                    className="action-icon"
                    size="small"
                    color="error"
                    aria-label={t('common.delete')}
                    onClick={() => onDeleteCompletion?.((item.data as ReminderCompletion).ID)}
                    sx={{
                      position: 'absolute',
                      top: 8,
                      right: 8,
                      opacity: 0,
                      transition: 'opacity 0.2s',
                    }}
                  >
                    <DeleteIcon fontSize="small" />
                  </IconButton>
                ) : item.type === 'note' || item.type === 'activity' ? (
                  <IconButton
                    className="action-icon"
                    size="small"
                    aria-label={t('common.edit')}
                    onClick={() =>
                      onEditItem(item.type as 'note' | 'activity', item.data as Note | Activity)
                    }
                    sx={{
                      position: 'absolute',
                      top: 8,
                      right: 8,
                      opacity: 0,
                      transition: 'opacity 0.2s',
                    }}
                  >
                    <EditIcon fontSize="small" />
                  </IconButton>
                ) : null}
              </Paper>
            </TimelineContent>
          </TimelineItem>
        );
      })}
    </Timeline>
  );
}
