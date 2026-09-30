// ===================================================================
// PyPlayground Worker — the public entry point
// ===================================================================
// Visitors reach https://pyplayground.<subdomain>.workers.dev. This Worker
// forwards every request to the Go app through a Workers VPC service, which
// travels over a Cloudflare Tunnel to the host (cloudflared → 127.0.0.1:8080).
// The host has no ports open to the internet.
//
// The app rate-limits by visitor IP and trusts exactly one header for it,
// X-Client-IP (see CLIENT_IP_HEADER). This Worker is what makes that safe: it
// removes every IP header the visitor sent and sets X-Client-IP itself from
// CF-Connecting-IP, which Cloudflare fills in and visitors can't forge.
// ===================================================================

// Headers a visitor could send to claim a different IP.
const CLIENT_IP_HEADERS = ['x-client-ip', 'x-forwarded-for', 'x-real-ip', 'true-client-ip', 'forwarded'];

// The VPC service decides where requests go; this origin only sets the Host header.
const APP_ORIGIN = 'http://127.0.0.1:8080';

/**
 * Build the request sent to the app from the visitor's request.
 * @param {Request} request
 * @returns {Request}
 */
export function toAppRequest(request) {
    const url = new URL(request.url);

    const headers = new Headers(request.headers);
    for (const name of CLIENT_IP_HEADERS) headers.delete(name);
    const visitorIP = request.headers.get('cf-connecting-ip');
    if (visitorIP) headers.set('x-client-ip', visitorIP);

    return new Request(APP_ORIGIN + url.pathname + url.search, {
        method: request.method,
        headers,
        body: request.body,
        // Pass the app's redirects (e.g. to GitHub for sign-in) back to the browser
        // instead of following them here.
        redirect: 'manual',
        duplex: 'half', // required by the fetch spec for streamed request bodies
    });
}

export default {
    async fetch(request, env) {
        try {
            return await env.APP.fetch(toAppRequest(request));
        } catch (err) {
            // The tunnel is down (e.g. the host is asleep or docker compose is stopped)
            console.error('app unreachable:', err);
            return new Response('PyPlayground is offline right now. Please try again in a few minutes.\n', {
                status: 503,
                headers: { 'content-type': 'text/plain; charset=utf-8', 'retry-after': '120' },
            });
        }
    },
};
