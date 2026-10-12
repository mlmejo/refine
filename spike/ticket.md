Ticket GEEK-11832: [VCP-13057] API integration for Starvault PHP SDK

Description: empty template (Problems, Solutions, Acceptance Criteria, Desired Outcome all "-").
Acceptance Criteria: empty. Technical Implementation: empty. Story points: 5.

Comments from the product owner:
- "D23 to D25, D01"
- "Update dating brands to send Starvault-Session-Id on header."

Repos: dating-backend (Laravel API the dating brands call), dating (Nuxt frontend of the brands), starvault.tech (StarVault, which ingests and reports events), starvault-php-sdk (the PHP SDK).

Developer's approach, written before seeing yours:
"Install the StarVault PHP SDK in dating-backend with composer and call StarVault::track() where the D23-D25 and D01 events happen. The SDK sends the events over HTTP to StarVault's ingest API. Then make the dating frontend send the Starvault-Session-Id header so events join the browser session."
