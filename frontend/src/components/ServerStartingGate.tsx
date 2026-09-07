import { Box, Button, CircularProgress, Stack, Typography } from '@mui/material';
import { useEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { probeServerReadiness, READY_POLL_MS, type ServerReadiness } from '../readiness/readiness';

// ServerStartingGate (issue #477, WEB-03): the "starting up" screen.
//
// This app's server runs migrations before it binds its listener, so during a
// deploy there is a window where the frontend (served by nginx) loads but the
// backend behind it is up-but-not-ready: /health/ready answers 503 while
// migrations run, and nginx answers 502/504 while the backend is not listening
// yet. Without this gate the app would mount anyway and fire a wall of failed
// requests with no explanation. The gate holds the tree below it back until the
// readiness probe stops reporting "starting up", then mounts it once.
//
// Fail-open (src/readiness/readiness.ts): only an authoritative not-ready
// signal keeps the gate up. The first probe resolves to ready or unknown
// (offline, ambiguous, unreachable) and the app mounts exactly as it would
// have without this module. It is the boot-time counterpart to the stale-client
// detector's fail-open stance: a network error must never lock a user out.
//
// Mounted at the root (src/index.tsx) OUTSIDE the routed <ErrorBoundary>, like
// <SessionExpiredGate> and <StaleClientGate>. Once the gate has passed it stays
// passed for the life of the page — later in-session server hiccups are the
// stale-client detector's and SessionExpiredGate's job, not this one's.
export default function ServerStartingGate({ children }: { children: React.ReactNode }) {
  const { t } = useTranslation();
  // 'checking' is the very first probe (in flight); 'starting' means the server
  // has authoritatively said "not yet" and we are polling until it recovers.
  // Both render the same starting-up surface, so a healthy boot shows it only
  // for the one round trip the first probe takes.
  const [phase, setPhase] = useState<'checking' | 'starting' | 'ready'>('checking');
  const pollTimer = useRef<number | null>(null);

  // The probe loop, stored so the "Try again" button can re-enter it. Assigned
  // inside the effect (not during render) so the poll callback and the click
  // handler share one implementation — a re-probe after "Try again" must keep
  // polling, not stop after one check.
  const runProbeLoop = useRef<() => void>(() => {});

  useEffect(() => {
    runProbeLoop.current = () => {
      if (pollTimer.current !== null) {
        window.clearTimeout(pollTimer.current);
        pollTimer.current = null;
      }
      void probeServerReadiness().then((result: ServerReadiness) => {
        if (result === 'starting') {
          setPhase('starting');
          pollTimer.current = window.setTimeout(() => runProbeLoop.current(), READY_POLL_MS);
        } else {
          // ready or unknown — fail open. Never hold the app back on ambiguity.
          setPhase('ready');
        }
      });
    };
    runProbeLoop.current();
    return () => {
      if (pollTimer.current !== null) {
        window.clearTimeout(pollTimer.current);
        pollTimer.current = null;
      }
    };
  }, []);

  if (phase === 'ready') {
    return <>{children}</>;
  }

  const starting = phase === 'starting';

  return (
    <Box
      role="status"
      aria-live="polite"
      sx={{
        minHeight: '100vh',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
      }}
    >
      <Stack spacing={3} sx={{ alignItems: 'center', maxWidth: 480, textAlign: 'center', px: 3 }}>
        <CircularProgress aria-label={t('app.startingUp.loading')} />
        <Typography variant="h5">{t('app.title')}</Typography>
        {starting ? (
          <Typography variant="body2" data-testid="server-starting-message">
            {t('app.startingUp.message')}
          </Typography>
        ) : (
          <Typography variant="body2">{t('app.startingUp.checking')}</Typography>
        )}
        <Button
          variant="outlined"
          onClick={() => {
            setPhase('checking');
            runProbeLoop.current();
          }}
        >
          {t('app.startingUp.retry')}
        </Button>
      </Stack>
    </Box>
  );
}
