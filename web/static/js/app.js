// ===================================================================
// app.js — Main Application Controller
// ===================================================================
// This is the orchestrator — it wires together the editor, Python runner,
// snippet manager, and all UI interactions.
//
// ARCHITECTURE:
// - editor.js    → Monaco Editor (code input)
// - pyodide-worker.js → Web Worker (Python execution)
// - snippets.js  → Server API (save/load snippets via fetch)
// - app.js       → THIS FILE (glue + UI logic)
//
// The app follows a simple event-driven pattern:
// 1. User clicks a button → event listener fires
// 2. Event listener calls the appropriate module function
// 3. Module function does its work and returns a result
// 4. UI is updated based on the result
// ===================================================================

// === CONFIGURATION ===
const CONFIG = {
    EXECUTION_TIMEOUT: 10000,  // 10 seconds max for code execution
    WORKER_PATH: '/static/js/pyodide-worker.js',
};

// === STATE ===
let worker = null;          // Pyodide Web Worker instance
let isRunning = false;      // Whether code is currently executing
let isPyodideReady = false; // Whether Pyodide has finished loading
let executionTimer = null;  // Timeout timer for long-running code

// === DOM REFERENCES ===
// We cache DOM element references for performance (avoiding repeated lookups)
const elements = {};

function cacheElements() {
    elements.runBtn = document.getElementById('run-btn');
    elements.clearBtn = document.getElementById('clear-btn');
    elements.saveBtn = document.getElementById('save-btn');
    elements.deleteBtn = document.getElementById('delete-btn');
    elements.output = document.getElementById('output');
    elements.outputContainer = document.getElementById('output-container');
    elements.loadingOverlay = document.getElementById('loading-overlay');
    elements.statusIndicator = document.getElementById('status-indicator');
    elements.statusText = document.getElementById('status-text');
    elements.execTime = document.getElementById('exec-time');
    elements.snippetSelect = document.getElementById('snippet-select');
    elements.themeToggle = document.getElementById('theme-toggle');
    elements.themeIconDark = document.getElementById('theme-icon-dark');
    elements.themeIconLight = document.getElementById('theme-icon-light');
    elements.shortcutsBtn = document.getElementById('shortcuts-btn');
    elements.shortcutsModal = document.getElementById('shortcuts-modal');
    elements.shortcutsClose = document.getElementById('shortcuts-close');
    elements.saveModal = document.getElementById('save-modal');
    elements.saveModalClose = document.getElementById('save-modal-close');
    elements.saveConfirm = document.getElementById('save-confirm');
    elements.saveCancel = document.getElementById('save-cancel');
    elements.snippetName = document.getElementById('snippet-name');
    elements.wasmError = document.getElementById('wasm-error');
}

// ===================================================================
// INITIALISATION
// ===================================================================

/**
 * Main initialisation — runs when the page loads.
 * Sets up all components in the correct order.
 */
document.addEventListener('DOMContentLoaded', async function () {
    // 1. Cache DOM elements
    cacheElements();

    // 2. Check WebAssembly support
    if (!checkWasmSupport()) return;

    // 3. Initialise the Monaco Editor
    try {
        await initEditor('editor-container');
    } catch (err) {
        console.error('Editor init failed:', err);
        showOutput(`⚠️ Failed to load code editor: ${err.message}`, 'error');
    }

    // 4. Start the Pyodide Web Worker
    initWorker();

    // 5. Set up event listeners
    setupEventListeners();

    // 6. Load saved snippets into the dropdown (async — fetches from server)
    await refreshSnippetList();

    // 7. Restore theme preference
    restoreTheme();
});

// ===================================================================
// WEBASSEMBLY SUPPORT CHECK
// ===================================================================

function checkWasmSupport() {
    if (typeof WebAssembly === 'undefined') {
        elements.wasmError.style.display = 'flex';
        document.querySelector('.playground-container').style.display = 'none';
        return false;
    }
    return true;
}

// ===================================================================
// PYODIDE WEB WORKER
// ===================================================================

/**
 * Creates and initialises the Pyodide Web Worker.
 *
 * WEB WORKERS:
 * - Run in a separate thread (don't block the UI)
 * - Communicate via postMessage/onmessage (no shared memory by default)
 * - Can be terminated (important for killing infinite loops!)
 * - Cannot access the DOM
 */
function initWorker() {
    setStatus('loading', 'Loading Python...');

    worker = new Worker(CONFIG.WORKER_PATH);

    // Handle messages FROM the worker
    worker.onmessage = function (event) {
        const { type, text, error, status, message } = event.data;

        switch (type) {
            case 'ready':
                isPyodideReady = true;
                setStatus('ready', 'Ready');
                showOutput('✅ Python runtime loaded successfully!\n\n', 'success');
                break;

            case 'status':
                setStatus('loading', message);
                break;

            case 'started':
                // Execution has started — start the timeout timer
                startTimeout();
                break;

            case 'stdout':
                appendOutput(text + '\n', 'stdout');
                break;

            case 'stderr':
                appendOutput(text + '\n', 'stderr');
                break;

            case 'result':
                appendOutput(text + '\n', 'stdout');
                break;

            case 'error':
                appendOutput('❌ ' + error + '\n', 'error');
                break;

            case 'completed':
                finishExecution();
                break;
        }
    };

    // Handle worker errors (e.g., script load failure)
    worker.onerror = function (err) {
        console.error('Worker error:', err);
        setStatus('error', 'Worker Error');
        showOutput(`❌ Python worker error: ${err.message}\n`, 'error');
        isPyodideReady = false;
        finishExecution();
    };

    // Tell the worker to start loading Pyodide
    worker.postMessage({ type: 'init' });
}

// ===================================================================
// CODE EXECUTION
// ===================================================================

let executionStartTime = 0;

/**
 * Run the current code from the editor.
 */
function runCode() {
    if (isRunning) return;
    if (!isPyodideReady) {
        showToast('Python is still loading. Please wait...', 'error');
        return;
    }

    const code = getEditorCode();
    if (!code.trim()) {
        showToast('No code to run!', 'error');
        return;
    }

    // Start execution
    isRunning = true;
    executionStartTime = performance.now();
    updateRunButton(true);
    setStatus('loading', 'Running...');
    showLoadingOverlay(true);
    clearOutput();

    // Send code to the worker
    worker.postMessage({ type: 'run', code: code });
}

/**
 * Start the execution timeout timer.
 * If code takes longer than CONFIG.EXECUTION_TIMEOUT, terminate the worker.
 */
function startTimeout() {
    clearTimeout(executionTimer);
    executionTimer = setTimeout(() => {
        if (isRunning) {
            // Kill the worker — this is the ONLY way to stop an infinite loop
            worker.terminate();

            appendOutput(
                `\n⏱️ Execution timed out after ${CONFIG.EXECUTION_TIMEOUT / 1000} seconds.\n` +
                'Your code may contain an infinite loop or very long computation.\n' +
                'Tip: Use loops with clear exit conditions.\n',
                'error'
            );

            finishExecution();

            // Restart the worker (the old one is dead)
            initWorker();
        }
    }, CONFIG.EXECUTION_TIMEOUT);
}

/**
 * Clean up after code execution completes (success or failure).
 */
function finishExecution() {
    clearTimeout(executionTimer);
    isRunning = false;
    updateRunButton(false);
    showLoadingOverlay(false);
    setStatus('ready', 'Ready');

    // Show execution time
    if (executionStartTime > 0) {
        const duration = ((performance.now() - executionStartTime) / 1000).toFixed(2);
        elements.execTime.textContent = `${duration}s`;
        elements.execTime.style.display = 'inline-block';
        executionStartTime = 0;
    }
}

// ===================================================================
// OUTPUT MANAGEMENT
// ===================================================================

function clearOutput() {
    elements.output.innerHTML = '';
    elements.execTime.style.display = 'none';
}

function showOutput(text, className) {
    clearOutput();
    appendOutput(text, className);
}

function appendOutput(text, className) {
    const span = document.createElement('span');
    span.className = `output-${className}`;
    span.textContent = text;
    elements.output.appendChild(span);

    // Auto-scroll to bottom
    elements.outputContainer.scrollTop = elements.outputContainer.scrollHeight;
}

// ===================================================================
// UI HELPERS
// ===================================================================

function updateRunButton(running) {
    if (running) {
        elements.runBtn.disabled = true;
        elements.runBtn.classList.add('running');
        elements.runBtn.querySelector('span').textContent = 'Running...';
    } else {
        elements.runBtn.disabled = false;
        elements.runBtn.classList.remove('running');
        elements.runBtn.querySelector('span').textContent = 'Run';
    }
}

function setStatus(state, text) {
    elements.statusIndicator.className = `status-indicator ${state}`;
    elements.statusText.textContent = text;
}

function showLoadingOverlay(show) {
    elements.loadingOverlay.style.display = show ? 'flex' : 'none';
}

/**
 * Show a toast notification.
 * @param {string} message
 * @param {'success'|'error'} type
 */
function showToast(message, type) {
    // Remove any existing toast
    const existing = document.querySelector('.toast');
    if (existing) existing.remove();

    const toast = document.createElement('div');
    toast.className = `toast ${type}`;
    toast.textContent = type === 'success' ? `✅ ${message}` : `⚠️ ${message}`;
    document.body.appendChild(toast);

    // Remove after animation
    setTimeout(() => toast.remove(), 3000);
}

// ===================================================================
// SNIPPET MANAGEMENT UI
// ===================================================================

// refreshSnippetList is now ASYNC because getSnippets() calls the server API.
//
// ASYNC/AWAIT:
// When a function is marked `async`, it always returns a Promise.
// Inside an async function, `await` pauses execution until the Promise resolves.
// This lets us write asynchronous code that READS like synchronous code:
//
//   const snippets = await getSnippets();  ← pauses here until server responds
//   snippets.forEach(...)                  ← runs after data arrives
//
// Without async/await, we'd need nested .then() callbacks ("callback hell").
async function refreshSnippetList() {
    const snippets = await getSnippets();
    const select = elements.snippetSelect;

    // Clear existing options (keep the placeholder)
    select.innerHTML = '<option value="">— Load Snippet —</option>';

    // Add each snippet as an option
    snippets.forEach(s => {
        const option = document.createElement('option');
        option.value = s.id;
        option.textContent = s.name;
        select.appendChild(option);
    });
}

function openSaveModal() {
    elements.saveModal.style.display = 'flex';
    elements.snippetName.value = '';
    elements.snippetName.focus();
}

function closeSaveModal() {
    elements.saveModal.style.display = 'none';
}

// confirmSave is now ASYNC because saveSnippet() calls the server API.
//
// TRY/CATCH WITH ASYNC:
// With async/await, errors are handled using try/catch (just like synchronous code).
// If the fetch() fails or the server returns an error, it goes to the catch block.
// This is much cleaner than .then().catch() chains.
async function confirmSave() {
    const name = elements.snippetName.value.trim();
    if (!name) {
        showToast('Please enter a snippet name', 'error');
        return;
    }

    const code = getEditorCode();
    const result = await saveSnippet(name, code);

    if (result.success) {
        showToast(`Snippet "${name}" saved!`, 'success');
        await refreshSnippetList();
        closeSaveModal();
    } else {
        showToast(result.error, 'error');
    }
}

async function loadSelectedSnippet() {
    const id = elements.snippetSelect.value;
    if (!id) return;

    const snippet = await loadSnippet(id);
    if (snippet) {
        setEditorCode(snippet.code);
        showToast(`Loaded "${snippet.name}"`, 'success');
    }
}

async function deleteSelectedSnippet() {
    const id = elements.snippetSelect.value;
    if (!id) {
        showToast('Select a snippet to delete', 'error');
        return;
    }

    // Fetch the snippet name for the confirmation dialog.
    // We need to await this since loadSnippet is now async.
    const snippet = await loadSnippet(id);
    if (snippet && confirm(`Delete snippet "${snippet.name}"?`)) {
        const result = await deleteSnippet(id);
        if (result.success) {
            showToast(`Deleted "${snippet.name}"`, 'success');
            elements.snippetSelect.value = '';
            await refreshSnippetList();
        } else {
            showToast(result.error, 'error');
        }
    }
}

// ===================================================================
// THEME TOGGLE
// ===================================================================

function toggleTheme() {
    const html = document.documentElement;
    const current = html.getAttribute('data-theme');
    const next = current === 'dark' ? 'light' : 'dark';

    html.setAttribute('data-theme', next);

    // Update icons
    elements.themeIconDark.style.display = next === 'dark' ? 'block' : 'none';
    elements.themeIconLight.style.display = next === 'light' ? 'block' : 'none';

    // Update Monaco Editor theme
    setEditorTheme(next);

    // Save preference
    localStorage.setItem('pyplayground_theme', next);
}

function restoreTheme() {
    const saved = localStorage.getItem('pyplayground_theme');
    if (saved && saved !== document.documentElement.getAttribute('data-theme')) {
        toggleTheme();
    }
}

// ===================================================================
// RESIZE HANDLE
// ===================================================================

function setupResize() {
    const handle = document.getElementById('resize-handle');
    const container = document.querySelector('.playground-container');
    const editorPanel = document.querySelector('.editor-panel');
    let isResizing = false;

    handle.addEventListener('mousedown', (e) => {
        isResizing = true;
        document.body.style.cursor = 'col-resize';
        document.body.style.userSelect = 'none';
        e.preventDefault();
    });

    document.addEventListener('mousemove', (e) => {
        if (!isResizing) return;

        const containerRect = container.getBoundingClientRect();
        const percentage = ((e.clientX - containerRect.left) / containerRect.width) * 100;

        // Clamp between 25% and 75%
        const clamped = Math.max(25, Math.min(75, percentage));

        editorPanel.style.flex = 'none';
        editorPanel.style.width = `${clamped}%`;
    });

    document.addEventListener('mouseup', () => {
        if (isResizing) {
            isResizing = false;
            document.body.style.cursor = '';
            document.body.style.userSelect = '';
        }
    });
}

// ===================================================================
// EVENT LISTENERS
// ===================================================================

function setupEventListeners() {
    // Run button
    elements.runBtn.addEventListener('click', runCode);

    // Clear output
    elements.clearBtn.addEventListener('click', () => {
        clearOutput();
        appendOutput('Output cleared.\n', 'info');
    });

    // Save snippet
    elements.saveBtn.addEventListener('click', openSaveModal);
    elements.saveConfirm.addEventListener('click', confirmSave);
    elements.saveCancel.addEventListener('click', closeSaveModal);
    elements.saveModalClose.addEventListener('click', closeSaveModal);

    // Save modal — enter key to confirm
    elements.snippetName.addEventListener('keydown', (e) => {
        if (e.key === 'Enter') confirmSave();
        if (e.key === 'Escape') closeSaveModal();
    });

    // Load snippet
    elements.snippetSelect.addEventListener('change', loadSelectedSnippet);

    // Delete snippet
    elements.deleteBtn.addEventListener('click', deleteSelectedSnippet);

    // Theme toggle
    elements.themeToggle.addEventListener('click', toggleTheme);

    // Keyboard shortcuts modal
    elements.shortcutsBtn.addEventListener('click', () => {
        elements.shortcutsModal.style.display = 'flex';
    });
    elements.shortcutsClose.addEventListener('click', () => {
        elements.shortcutsModal.style.display = 'none';
    });

    // Close modals on overlay click
    elements.shortcutsModal.addEventListener('click', (e) => {
        if (e.target === elements.shortcutsModal) elements.shortcutsModal.style.display = 'none';
    });
    elements.saveModal.addEventListener('click', (e) => {
        if (e.target === elements.saveModal) closeSaveModal();
    });

    // Global keyboard shortcuts
    document.addEventListener('keydown', (e) => {
        // Ctrl+Enter — Run code
        if (e.ctrlKey && e.key === 'Enter') {
            e.preventDefault();
            runCode();
        }

        // Ctrl+S — Save snippet
        if (e.ctrlKey && e.key === 's') {
            e.preventDefault();
            openSaveModal();
        }

        // Ctrl+L — Clear output
        if (e.ctrlKey && e.key === 'l') {
            e.preventDefault();
            clearOutput();
            appendOutput('Output cleared.\n', 'info');
        }

        // Escape — Close modals
        if (e.key === 'Escape') {
            elements.shortcutsModal.style.display = 'none';
            closeSaveModal();
        }
    });

    // Resize handle
    setupResize();
}
