# Hello Kupe game implementation

Build the main Hello Kupe experience as a Next.js matching game served by the
existing Go application, with infrastructure owned by `infra/`.

1. Model six capability pairs. Keep the shuffled deck private on the backend;
   validate each reveal and calculate moves and elapsed time there.
2. Store sessions and submitted names in PostgreSQL using row locks so replicas
   share a consistent game. Rank by fewest moves, then shortest completion time.
   Offer clearly labelled temporary practice mode without a database.
3. Build the responsive Next.js interface: animated cards, feature discoveries,
   custom-domain hint dialog, completion form, leaderboard and replay.
4. Package the static Next.js export with the Go image. Mount readwrite bindings
   in the application and the owner binding only in a migration namespace.
5. Align Terraform bindings, chart settings, local development and signup-to-app
   instructions. Add frontend and infrastructure checks to CI.
6. Verify game rules, invalid requests, concurrency, PostgreSQL persistence,
   frontend build, keyboard interaction, mobile layout, image and Helm rendering.

Public demo scope: names are public and unverified, and automated players can
play too. This is an educational scoreboard, not a competitive service with
accounts or bot prevention. No synthetic logs or seeded leaderboard entries.

## Verification completed

- Go unit, HTTP and race tests; PostgreSQL transactions and role separation.
- Frontend production build, type checks and time/ranking tests.
- Browser playthrough, mismatches, all six matches, public name submission,
  leaderboard refresh and session recovery after reload; hint dialog and Escape.
- Responsive mobile and desktop layouts, reduced-motion styles and keyboard controls.
- Helm rendering with and without database bindings; infrastructure schema validation.
- Container build with Go 1.26.10; Go lint, security analysis and vulnerability scan.

Cloud infrastructure has not been applied and the new image has not been published.
