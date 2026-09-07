import React from 'react';
import ReactDOM from 'react-dom/client';
import './colors.css';
import './index.css';
import App from './App';
import ErrorBoundary from './components/ErrorBoundary';
import ServerStartingGate from './components/ServerStartingGate';
import ServiceWorkerUpdatePrompt from './components/ServiceWorkerUpdatePrompt';
import SessionExpiredGate from './components/SessionExpiredGate';
import StaleClientGate from './components/StaleClientGate';
import reportWebVitals from './reportWebVitals';
import * as serviceWorkerRegistration from './serviceWorkerRegistration';
import { notifyUpdateAvailable } from './serviceWorkerUpdates';
import { startStaleClientDetector } from './staleClient/detector';
import './i18n/config';
import { AppThemeProvider } from './AppThemeProvider';
import { AnnouncerProvider } from './context/AnnouncerContext';
import { SnackbarProvider } from './context/SnackbarContext';
import { DateFormatProvider } from './DateFormatProvider';

const logError = (error: Error, errorInfo: React.ErrorInfo) => {
  console.error('Application Error:', error);
  console.error('Error Info:', errorInfo);
};

const root = ReactDOM.createRoot(document.getElementById('root') as HTMLElement);

root.render(
  <React.StrictMode>
    <AppThemeProvider>
      <DateFormatProvider>
        <SnackbarProvider>
          <AnnouncerProvider>
            {/* Issue #477 (WEB-03): ServerStartingGate is the outermost gate --
                while the backend reports up-but-not-ready (mid-migration), the
                tree below it is held back behind a "starting up" screen so a
                client loading during a deploy does not fire a wall of failed
                requests. It fails open (an unreachable/ambiguous /health/ready
                mounts the app as before) and, once passed, stays passed. Issue
                #557: SessionExpiredGate sits outside the ErrorBoundary on
                purpose -- a page crash or a route navigation must not take the
                re-auth prompt down with it. StaleClientGate sits here for the
                same reason: a forced reload must keep working even if the
                routed page (or the whole app) has crashed. */}
            <ServerStartingGate>
              <SessionExpiredGate />
              <StaleClientGate />
              <ErrorBoundary
                name="Application"
                onError={logError}
                showDetails={import.meta.env.DEV}
              >
                <App />
                <ServiceWorkerUpdatePrompt />
              </ErrorBoundary>
            </ServerStartingGate>
          </AnnouncerProvider>
        </SnackbarProvider>
      </DateFormatProvider>
    </AppThemeProvider>
  </React.StrictMode>,
);

// The service worker is REGISTERED, not unregistered (CRA scaffolds the
// opposite and it stayed that way until 2026-08-06).
//
// This is not primarily about offline support: Web Push (N9) delivers to a
// service worker, and a PushSubscription is owned by the registration. While
// this called unregister(), every page load tore down the registration and
// took any push subscription with it -- so enabling browser notifications
// appeared to work and then silently stopped, with the server's stored
// subscription going stale and being pruned on the next 404/410.
//
// The cost of registering is that the app is now served cache-first, so a
// deployed update is not picked up until the new worker takes over.
// ServiceWorkerUpdatePrompt turns that into an explicit "reload to update"
// notice rather than a user sitting on a stale bundle indefinitely.
//
// register() is a no-op outside production builds and on insecure origins
// (browsers refuse to register a service worker over plain HTTP), which is why
// browser push needs HTTPS or localhost.
serviceWorkerRegistration.register({ onUpdate: notifyUpdateAvailable });

// The stale-client detector (issue #475) is the backstop to the update prompt
// above: it polls /health so a tab left open through a deploy notices the
// server moved, and forces a reload when this bundle drops below the server's
// declared floor. It is inert in dev/test builds (no stamped version), same as
// register(). Safe to call unconditionally.
startStaleClientDetector();

// If you want to start measuring performance in your app, pass a function
// to log results (for example: reportWebVitals(console.log))
// or send to an analytics endpoint. Learn more: https://bit.ly/CRA-vitals
reportWebVitals();
