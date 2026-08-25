# State Fabric hero video

This Remotion project visualizes a real two-machine State Fabric run:

1. this host pushes a Git commit through a local cache to an off-box Linux authority;
2. the remote machine clones, commits, and pushes back with normal Git commands;
3. a fresh local cache clones the returned commit and verifies both file hashes;
4. authority and cache audits agree on 15 objects, 2 transitions, and 2 receipts.

The captured values in `src/hero-data.ts` are sanitized evidence from the run.
Sandbox API endpoints, credentials, capability files, and trust-domain bundles are
not retained.

```bash
npm install
npm run dev
npm run lint
npm run render
```
