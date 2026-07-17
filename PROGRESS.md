# Progress Notes — Folder Tools

Working notes for the folder-management work on the ReiSearch MCP server, so
this can be picked up on another machine. Last updated: 2026-07-07.

## TL;DR

- Added folder-management MCP tools. After reconciling with the backend's
  authoritative public spec, the shipped set is **5 folder tools**, all live
  and verified against production (`https://mcp.reisearch.com`).
- Latest commit on `main`: **`291b963`** (pushed). Deployed and confirmed
  working end-to-end.

## 2026-07-17 — Buyers & Buy Boxes sync

Added the Buyer + Buy Box tool set. **Tool count is now 41** (was 27). Mirrors the
documented public surface only — deliberately NO buyer notes, NO deletes, NO
`PUT /buyers/{id}/info` (excluded from the public docs).

New tools (14):
- Buyers: `create_buyer`, `list_buyers`, `get_buyer`, `update_buyer`
- My buy boxes: `list_my_buyboxes`, `create_my_buyboxes`, `get_my_buybox`, `update_my_buybox`
- Buyer buy boxes: `list_buyer_buyboxes`, `create_buyer_buyboxes`, `get_buyer_buybox`, `update_buyer_buybox`
- Read any user's (deal matching): `get_user_buyboxes`, `get_buybox_details`

Implementation notes:
- **TS envelope** (`tsEnvelope`): these domains put `lastKey` TOP-LEVEL and can populate
  `error` on a 2xx — a partial buy-box create returns 201 with `data`=created and
  `error`=failed locations. Shared `tsGet`/`tsWrite` helpers; `BuyBoxCreateResult`
  surfaces `created` + `failed` (per location). Buyer/box list types carry top-level `lastKey`.
- Buy box = purchase criteria; ONE box per `locations[]` entry. `/me` = the user's OWN
  criteria; `/buyers/{id}` = a buyer's; `/users/{id}` = read any (deal matching).
- Path gotcha handled: literal `buyboxes` segment on `/buyers/buyboxes/{id}` and
  `/users/buyboxes/{id}` vs the variable segment on `/buyers/{buyerID}/buyboxes`.
- Tool inputs nest `criteria` (safe schema reflection, like `push_lead_to_crm`); the wire
  request flattens it via embedding. List `limit` defaults to 50.
- Verified vs `../reisearch-pub-client/public/openapi.yaml` (8 paths / 14 ops match; TS
  envelope confirmed). go build + gofmt + registration smoke test pass. NOT yet live-tested
  through `/mcp` (needs push → redeploy → fresh token; exercise the duplicate-location path
  to see `failed`).

Follow-ups: tell the docs session to bump the MCP page tool count (27 → 41) and add a
Buyers/Buy Boxes group; `REISEARCH_MCP_DOCS.md` / `MCP_SERVER_REFERENCE.md` / the connect
artifact also still say 27.

## 2026-07-08 — public-API sync (search / CRM / folders)

Synced the MCP to the July-8 public-API changes. Tool count is now **27**.
Verified `go build ./...`, gofmt, and every changed/new path + response shape
cross-checked against `../reisearch-pub-client/public/openapi.yaml`. Not yet
live-smoke-tested through `/mcp` (needs redeploy + a fresh token).

Required fixes:
- `search_users` → `GET /connect/v1/search/connections` (was `/search/users`;
  tool name kept as `search_users`).
- `add_crm_note` → `.../crm-notes` (was `.../notes`).
- `list_my_folders` no longer drills in — `/folders/all` now ignores `folder_id`.
  Dropped `folderID` from the tool; drilling in moved to `get_folder`.

New tools:
- `get_folder` — `GET /folders/{folder_id}` → `{folder, folders, properties}`.
- `list_created_folders` — `GET /folders/created` (root folders I created).
- `get_property_call_activity` — `GET /crm/property-call-activity/{propertyId}`
  → CRM contacts + call activity (call_id feeds get_call_data).
- `get_call_data` — `GET /crm/call-data/{callId}` (callId has a `#`,
  percent-encoded client-side; contactId + locationId required).

Refactor: `ListFolders` and `ListCreatedFolders` share a `listFoldersAtPath` helper.

## Architecture recap

- This repo is the **MCP server** (`cmd/reisearch-http`). It does NOT talk to
  the internal folders service directly — it calls the **public "connect"
  gateway** at `REISEARCH_PUB_URL` (`https://api-pub.reisearch.com`) under the
  `/connect/v1/...` prefix.
- Auth: the MCP server takes a Cognito JWT from the `Authorization: Bearer`
  header ([internal/oauth/middleware.go](internal/oauth/middleware.go)) and
  forwards it to the gateway. `REISEARCH_API_KEY` is a separate thing and is
  NOT what the folder/CRM endpoints use.
- Everything lives in two files:
  - `internal/reisearch/client.go` — HTTP client methods + response types
  - `internal/tools/property.go` — tool input structs, handlers, and `Register`

## KEY LESSON: the public API surface ≠ the internal folders API

The internal folders service exposes ~26 routes, but the **public connect
gateway exposes only a hand-picked subset**. The authoritative source is the
backend dev's spec: **`folders.swagger.yaml`** (documents only the *active*
public routes; delete/rename/move/favorites-toggle/reorder/permission-
templates/tags are commented out on the backend).

Active public folder routes (from the swagger):

| Method | Path                          | Implemented as            |
| ------ | ----------------------------- | ------------------------- |
| POST   | `/connect/v1/folders`         | `create_folder`           |
| GET    | `/connect/v1/folders/all`     | `list_my_folders`         |
| GET    | `/connect/v1/folders/members` | `get_folder_members`      |
| POST   | `/connect/v1/folders/members` | `add_folder_member`       |
| POST   | `/connect/v1/folders/property`| `add_property_to_folder`  |
| POST   | `/connect/v1/folders/members/bulk` | (not implemented)    |
| GET    | `/connect/v1/folders/mine`    | (not implemented)         |
| GET    | `/connect/v1/folders/favorites` | (not implemented)       |

Gotcha that cost us time: the list route is **`/folders/all`**, NOT `/folders`.
`GET /folders` returns `404 page not found` (Gin router-level not-found).

## Shipped folder tools (5) — all verified live

| Tool | Endpoint | Verified |
| ---- | -------- | -------- |
| `create_folder` | `POST /folders` | ✅ returns created folder + id |
| `list_my_folders` | `GET /folders/all` | ✅ returns folders + count + last_key |
| `get_folder_members` | `GET /folders/members` | ✅ returns member records |
| `add_folder_member` | `POST /folders/members` | ✅ reachable (single member; query params folder_id, member_id, existing_property_access) |
| `add_property_to_folder` | `POST /folders/property` | ✅ reachable (mode=move\|copy; copy flags only sent on copy) |

Notes:
- `list_my_folders`: pass `folderID` to OPEN a folder (returns its subfolders +
  properties, different shape) instead of listing root folders. Paginate with
  `limit` / `lastKey`.
- `add_folder_member` is single-member only; the swagger's bulk variant was
  intentionally skipped.

## Removed tools (routes not on the public gateway)

Registered these initially, then removed after live 404s + confirming against
the swagger they're commented out on the backend:

- `delete_folder` — `DELETE /folders` (removed in `7239f90`)
- `get_folder_info` — `GET /folders/info` (not in public API; removed in `291b963`)
- `rename_folder` — `PUT /folders/rename` (commented out; removed in `291b963`)
- `move_folder` — `POST /folders/move` (commented out; removed in `291b963`)

If/when the backend un-comments these on the gateway, re-add them following the
same pattern (client method + input struct + handler + `mcp.AddTool`).

## Deferred / not needed (per product call)

- Remaining active routes: `GET /folders/mine`, `GET /folders/favorites`,
  `POST /folders/members/bulk` — decided not needed right now.
- Phase 2 (tags 20–26, permission templates 17–18) — deprioritized; not
  exposed on the public gateway anyway.

## How to smoke-test the deployed server

The MCP endpoint is `https://mcp.reisearch.com/mcp` (streamable HTTP,
JSON-RPC 2.0, Cognito bearer required).

1. Get a token: the deploy has a dev PKCE harness (`/testlogin`, enabled with
   `ENABLE_TESTLOGIN=1`) that mints a real Cognito access token. Use the
   `access_token` field.
2. Flow: `initialize` (capture the `Mcp-Session-Id` response header) →
   `notifications/initialized` → `tools/list` / `tools/call`.
   Headers: `Authorization: Bearer <token>`,
   `Accept: application/json, text/event-stream`. Responses are SSE
   (`data: {json}`).
3. Quick check of what's deployed: `tools/list` and confirm the folder tool
   names match the 5 above (if the removed tools still show, the server is
   running stale code — needs a rebuild+redeploy).
4. You can also bypass MCP and hit the gateway directly with the same token:
   `curl https://api-pub.reisearch.com/connect/v1/folders/all -H "Authorization: Bearer <token>"`.

## Deploy note

Redeploy must **rebuild** (new code isn't picked up by a plain restart). With
the repo compose file: `git pull && docker compose up -d --build`. Confirmed:
after redeploy from `291b963`, `list_my_folders` returns real data and the
removed tools are gone from `tools/list`.

## Loose ends / cleanup

- Throwaway folders left in the test account (`abrhamgg@gmail.com`) from live
  testing — no delete tool, so remove manually if desired:
  `__mcp_smoke_test__`, `__mcp_test_A__`, `__mcp_test_B__`.

## Related prior work (context)

Recent tool additions before folders: CRM push flow (`push_lead_to_crm`, CRM
pickers, `create_crm_opportunity`, `add_crm_note` — `439d5da` and earlier),
`search_users`, `share_property`, `search_properties`, comps tools. The CRM
push flow was not live-tested (needs a token + a connected CRM + a property).
