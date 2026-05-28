// Thin pass-through Worker for the agorcha.dev docs site.
//
// All real serving is done by the Cloudflare static-assets runtime via the
// ASSETS binding declared in wrangler.jsonc. We keep this file (rather than
// using a bare-`assets` config) so the worker config is valid on the broad
// range of wrangler versions used in CI; a `main`-less static Worker is
// only supported on very recent wrangler releases.
//
// The fetch handler delegates every request to the ASSETS binding, which
// serves files from ./dist with the configured not_found_handling.
export default {
  /**
   * @param {Request} request
   * @param {{ ASSETS: { fetch: (req: Request) => Promise<Response> } }} env
   */
  async fetch(request, env) {
    return env.ASSETS.fetch(request);
  },
};
