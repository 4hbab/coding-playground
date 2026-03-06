// ===================================================================
// auth.js — Authentication UI Module
// ===================================================================
// Handles the "Sign in with GitHub" button in the navbar and the
// save-prompt flow that encourages users to sign in before saving.
//
// ON PAGE LOAD:
//   1. checkAuthStatus() calls GET /api/me
//   2. If 200 → user is logged in, show avatar + username
//   3. If 401/404/error → not logged in, show "Sign in" button
//
// SAVE PROMPT FLOW:
//   When a user clicks "Save" without being logged in:
//   1. Show a prompt: "Sign in to keep your snippets safe"
//   2. Option A: "Sign in with GitHub" → redirects to OAuth
//   3. Option B: "Save without account" → saves anonymously (user_id = null)
//
// The JWT is stored in an HttpOnly cookie by the server — this module
// doesn't handle tokens directly. fetch() sends cookies automatically.
// ===================================================================

let currentUser = null;
// Always treat auth as available — show the sign-in button by default.
// Only switch to "logged in" when /api/me returns 200.
let authAvailable = true;

/**
 * Check the current authentication status by calling /api/me.
 * Updates the navbar UI accordingly.
 */
async function checkAuthStatus() {
    try {
        const response = await fetch('/api/me');

        if (response.ok) {
            // User is authenticated — show avatar + username
            currentUser = await response.json();
            renderLoggedIn(currentUser);
        } else {
            // Any non-200 (401 = not logged in, 404 = routes not registered)
            // → show the "Sign in" button either way
            currentUser = null;
            renderLoggedOut();
        }
    } catch (err) {
        // Network error or server down — still show "Sign in" button
        console.warn('Auth check failed:', err);
        currentUser = null;
        renderLoggedOut();
    }
}

/**
 * Returns the current user or null.
 */
function getCurrentUser() {
    return currentUser;
}

/**
 * Returns true if the user is logged in.
 */
function isLoggedIn() {
    return currentUser !== null;
}

/**
 * Log the user out by calling POST /auth/logout, then refresh the UI.
 */
async function logout() {
    try {
        await fetch('/auth/logout', { method: 'POST' });
    } catch (err) {
        console.warn('Logout request failed:', err);
    }
    currentUser = null;
    renderLoggedOut();
    // Close dropdown if open
    const dropdown = document.getElementById('auth-dropdown');
    if (dropdown) dropdown.classList.remove('open');
}

// ===================================================================
// UI RENDERING
// ===================================================================

function renderLoggedIn(user) {
    const container = document.getElementById('auth-section');
    if (!container) return;

    container.innerHTML = `
        <div class="auth-user" id="auth-user-btn">
            <img class="auth-avatar" src="${escapeHtml(user.avatarUrl || '')}" 
                 alt="${escapeHtml(user.login)}" 
                 onerror="this.style.display='none'; this.nextElementSibling.style.display='flex';">
            <div class="auth-avatar-fallback" style="display:none">
                ${escapeHtml(user.login.charAt(0).toUpperCase())}
            </div>
            <span class="auth-username">${escapeHtml(user.login)}</span>
            <svg class="auth-chevron" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                <polyline points="6 9 12 15 18 9"></polyline>
            </svg>
        </div>
        <div class="auth-dropdown" id="auth-dropdown">
            <div class="auth-dropdown-header">
                <span class="auth-dropdown-name">${escapeHtml(user.login)}</span>
                <span class="auth-dropdown-email">${escapeHtml(user.email || '')}</span>
            </div>
            <div class="auth-dropdown-divider"></div>
            <button class="auth-dropdown-item auth-logout-btn" id="auth-logout-btn">
                <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                    <path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4"></path>
                    <polyline points="16 17 21 12 16 7"></polyline>
                    <line x1="21" y1="12" x2="9" y2="12"></line>
                </svg>
                Sign out
            </button>
        </div>
    `;

    // Toggle dropdown on click
    const userBtn = document.getElementById('auth-user-btn');
    const dropdown = document.getElementById('auth-dropdown');
    userBtn.addEventListener('click', (e) => {
        e.stopPropagation();
        dropdown.classList.toggle('open');
    });

    // Close dropdown on outside click
    document.addEventListener('click', () => {
        dropdown.classList.remove('open');
    });

    // Logout button
    document.getElementById('auth-logout-btn').addEventListener('click', (e) => {
        e.stopPropagation();
        logout();
    });
}

function renderLoggedOut() {
    const container = document.getElementById('auth-section');
    if (!container) return;

    container.innerHTML = `
        <a href="/auth/github/login" class="auth-login-btn" id="auth-login-btn" title="Sign in with GitHub">
            <svg width="16" height="16" viewBox="0 0 24 24" fill="currentColor">
                <path d="M12 0C5.37 0 0 5.37 0 12c0 5.31 3.435 9.795 8.205 11.385.6.105.825-.255.825-.57 0-.285-.015-1.23-.015-2.235-3.015.555-3.795-.735-4.035-1.41-.135-.345-.72-1.41-1.23-1.695-.42-.225-1.02-.78-.015-.795.945-.015 1.62.87 1.845 1.23 1.08 1.815 2.805 1.305 3.495.99.105-.78.42-1.305.765-1.605-2.67-.3-5.46-1.335-5.46-5.925 0-1.305.465-2.385 1.23-3.225-.12-.3-.54-1.53.12-3.18 0 0 1.005-.315 3.3 1.23.96-.27 1.98-.405 3-.405s2.04.135 3 .405c2.295-1.56 3.3-1.23 3.3-1.23.66 1.65.24 2.88.12 3.18.765.84 1.23 1.905 1.23 3.225 0 4.605-2.805 5.625-5.475 5.925.435.375.81 1.095.81 2.22 0 1.605-.015 2.895-.015 3.3 0 .315.225.69.825.57A12.02 12.02 0 0 0 24 12c0-6.63-5.37-12-12-12z"/>
            </svg>
            <span>Sign in</span>
        </a>
    `;
}

// renderAuthUnavailable is no longer needed — we always show the sign-in button.

// ===================================================================
// SAVE PROMPT (sign-in nudge before saving)
// ===================================================================

/**
 * Shows the sign-in prompt modal before saving a snippet.
 * Returns a Promise that resolves to:
 *   - 'signin' → user chose to sign in (redirect happens)
 *   - 'anonymous' → user chose to save without signing in
 *   - 'cancel' → user cancelled
 */
function showSignInPrompt() {
    return new Promise((resolve) => {
        const modal = document.getElementById('signin-prompt-modal');
        if (!modal) {
            resolve('anonymous'); // fallback if modal doesn't exist
            return;
        }

        modal.style.display = 'flex';

        const signinBtn = document.getElementById('signin-prompt-signin');
        const anonBtn = document.getElementById('signin-prompt-anonymous');
        const closeBtn = document.getElementById('signin-prompt-close');

        function cleanup() {
            modal.style.display = 'none';
            signinBtn.removeEventListener('click', onSignin);
            anonBtn.removeEventListener('click', onAnon);
            closeBtn.removeEventListener('click', onClose);
            modal.removeEventListener('click', onOverlay);
        }

        function onSignin() { cleanup(); window.location.href = '/auth/github/login'; }
        function onAnon() { cleanup(); resolve('anonymous'); }
        function onClose() { cleanup(); resolve('cancel'); }
        function onOverlay(e) { if (e.target === modal) { cleanup(); resolve('cancel'); } }

        signinBtn.addEventListener('click', onSignin);
        anonBtn.addEventListener('click', onAnon);
        closeBtn.addEventListener('click', onClose);
        modal.addEventListener('click', onOverlay);
    });
}

// ===================================================================
// UTILITY
// ===================================================================

function escapeHtml(str) {
    const div = document.createElement('div');
    div.textContent = str;
    return div.innerHTML;
}
