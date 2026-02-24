// ===================================================================
// pyodide-worker.js — Web Worker for Python Code Execution
// ===================================================================
// This file runs inside a Web Worker — a separate JavaScript thread that
// doesn't block the main UI. This is critical because:
//
// 1. Loading Pyodide (~15MB) would freeze the page if done on the main thread
// 2. Running Python code could take a long time (or infinite loop!)
// 3. Web Workers can be terminated to enforce timeouts
//
// COMMUNICATION:
// Web Workers communicate with the main thread via postMessage/onmessage.
// Main thread → Worker:  { type: 'init' } or { type: 'run', code: '...' }
// Worker → Main thread:  { type: 'ready' } or { type: 'stdout', text: '...' }, etc.
//
// The worker has NO access to the DOM — it's a pure computation environment.
// ===================================================================

// Import Pyodide from CDN
importScripts('https://cdn.jsdelivr.net/pyodide/v0.27.4/full/pyodide.js');

let pyodide = null;

/**
 * Initialise Pyodide — this downloads and compiles the Python runtime.
 * Takes several seconds on first load (cached by the browser after that).
 */
async function initPyodide() {
    try {
        postMessage({ type: 'status', status: 'loading', message: 'Loading Python runtime...' });

        pyodide = await loadPyodide({
            // Redirect Python's stdout and stderr to the main thread
            stdout: (text) => {
                postMessage({ type: 'stdout', text: text });
            },
            stderr: (text) => {
                postMessage({ type: 'stderr', text: text });
            },
        });

        postMessage({ type: 'ready' });
    } catch (err) {
        postMessage({ type: 'error', error: `Failed to initialise Python: ${err.message}` });
    }
}

/**
 * Run Python code and send the output back to the main thread.
 * Catches all Python exceptions and sends them as structured error messages.
 */
async function runCode(code) {
    if (!pyodide) {
        postMessage({ type: 'error', error: 'Python runtime not loaded yet. Please wait...' });
        return;
    }

    try {
        postMessage({ type: 'started' });

        // Run the Python code
        // runPythonAsync handles top-level await and async code
        const result = await pyodide.runPythonAsync(code);

        // If the code returns a value (last expression), send it
        if (result !== undefined && result !== null) {
            // Convert Python objects to JS string representation
            const resultStr = String(result);
            if (resultStr !== '' && resultStr !== 'None') {
                postMessage({ type: 'result', text: resultStr });
            }
        }

        postMessage({ type: 'completed' });
    } catch (err) {
        // Python exceptions come through as JavaScript errors.
        // The error message contains the full Python traceback.
        let errorMessage = err.message || String(err);

        // Clean up the error message for better readability
        // Pyodide wraps Python exceptions — extract the Python traceback
        if (errorMessage.includes('PythonError')) {
            errorMessage = errorMessage.replace('PythonError: ', '');
        }

        postMessage({ type: 'error', error: errorMessage });
        postMessage({ type: 'completed' });
    }
}

// === MESSAGE HANDLER ===
// Listen for messages from the main thread
onmessage = async function (event) {
    const { type, code } = event.data;

    switch (type) {
        case 'init':
            await initPyodide();
            break;

        case 'run':
            await runCode(code);
            break;

        default:
            postMessage({ type: 'error', error: `Unknown message type: ${type}` });
    }
};
