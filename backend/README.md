# Travo platform setup

## Run locally

1. Create a PostgreSQL database and copy `.env.example` to `.env`.
2. Set `DB_DSN` and a random `JWT_SECRET` of at least 32 characters.
3. Configure `SUPERADMIN_NAME`, `SUPERADMIN_EMAIL`, and `SUPERADMIN_PASSWORD`.
   The first startup creates that superadmin; subsequent startups do not reset
   the password. Admin roles cannot be selected through public registration.
4. Start the API with `go run .` from this directory.
5. Copy `frontend/.env.example` to `frontend/.env`, then run `npm run dev`
   from `frontend`.

To reset the password for an existing, active superadmin, set
`SUPERADMIN_EMAIL` and the new `SUPERADMIN_PASSWORD` in this directory's
private `.env`, then run `go run . reset-superadmin-password`. This command
updates only the matching superadmin account and exits without starting the
API. It does not activate suspended or disabled accounts.

The backend runs database migrations and ensures Cameroon is active on startup.
The superadmin can add countries and create country-scoped administrators.
Country administrators can review drivers and manage users in their assigned
country.

## Payments

The app uses Fapshi-hosted checkout for MTN Mobile Money and Orange Money.
Set the Fapshi sandbox credentials first, and configure the service webhook URL
as `https://<api-host>/api/payments/fapshi/webhook` with the same webhook secret
as `FAPSHI_WEBHOOK_SECRET`. Set `FAPSHI_MODE=live` only with activated live
credentials and a publicly reachable HTTPS webhook.

The platform calculates a 5% commission on the trip fare. Fapshi currently
documents a 3% collection fee; the passenger total is grossed up so the
estimated provider fee is shown separately. The actual provider fee is recorded
from Fapshi's transaction response. Driver amount due is the provider's net
revenue less the platform commission; it is recorded for reconciliation and is
not automatically disbursed.

Fapshi currently supports payments in Cameroon, so trip publishing and booking
are enabled for Cameroon only. The country-admin and country-management
workflows are country-scoped, but another country's trips require a compatible
payment provider before checkout can be enabled there.

Fapshi does not support idempotency keys. If payment initialization times out
with an unknown outcome, the booking is deliberately blocked from retrying to
avoid charging the passenger twice. Check the transaction in the Fapshi
dashboard and contact support with the booking ID.

## Main API workflows

- Public: `POST /api/auth/register`, `POST /api/auth/login`, `GET /api/countries`,
  `GET /api/trips`, `GET /api/trips/:id`.
- Passenger: search trips, `POST /api/trips/:id/bookings`, initiate payment with
  `POST /api/bookings/:id/payment`, then check status with
  `GET /api/bookings/:id/payment-status`.
- Driver: upload the front and back of a national ID and driver license
  (each side PDF, JPEG, or PNG, up to 5 MB), wait for country-admin
  approval and activation; view verification status and document decisions in
  the dashboard. Then publish and manage trips.
  Add a valid phone number in Edit profile before publishing or republishing.
  Drivers can edit their name and phone and upload their documents and a
  consented JPEG or PNG selfie (up to 5 MB and 25 megapixels) there.
- Country admin: review identity documents and activate verified drivers,
  manage country users, and view country-level totals.
- Superadmin: manage supported countries and create country administrators.

Run backend checks with `go test ./...` from this directory. Run frontend
checks with `npm run build` and `npm run lint` from `frontend`.

The driver's current phone number is visible only to the passenger associated
with a confirmed booking whose payment succeeded, including after departure.
Changing it also changes the phone number usable for account login. The driver
dashboard's amount due totals successful, confirmed bookings; it is not a
record of payouts.

Older one-sided document submissions remain available to administrators, but
cannot be newly approved; drivers should upload both sides again. Previously
approved driver accounts are not automatically deactivated. When both required
two-sided documents are approved, rejecting an extra submission does not revoke
that approval; activation also repairs an outdated verification status.

Selfies are stored privately and retained along with prior replacements for
security. The current selfie is accessible to its driver, authorized
administrators, and passengers with an active (pending-payment or confirmed)
booking for that driver's trip. Administrators can compare it manually with a
specific national ID and record an observation. This is not automated face
verification or a liveness check, and does not change document approval or
account activation. While a driver is on the dashboard awaiting review or
activation, the dashboard refreshes these statuses automatically. No OCR is
performed. Uploaded files may contain image
metadata; protect and back up the private upload directory and database.
