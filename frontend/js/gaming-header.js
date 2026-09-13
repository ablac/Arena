import { startSessionSync } from './account-session.js?v=20260905u';

// Display the existing Arena session. The account link opens the existing
// dashboard, which owns sign-in, sign-out and all account mutations.
const status = document.querySelector('[data-gaming-account-status]');
if (status && !document.body.classList.contains('dashboard-embedded')) {
  startSessionSync((session) => {
    status.textContent = session?.authenticated === true ? 'Signed in to Arena' : 'Guest · sign in to Arena';
  });
}
