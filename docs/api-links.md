# Share Links API

The file-sharing service exposes a bearer-token protected API for creating and
managing share links. The API is separate from the SFTP admin UI: the UI is
served by `sftp-admin`, while these endpoints are served by the file-sharing
service at `PUBLIC_URL`.

## Before you start

Set the service URL and current API token in your shell:

```sh
export BASE_URL='http://192.168.11.196:18080'
export ADMIN_TOKEN='replace-with-the-current-admin-token'
```

Every API request must include:

```http
Authorization: Bearer <ADMIN_TOKEN>
```

The token is not the password assigned to an individual share link. Link
passwords protect public access to one link; `ADMIN_TOKEN` protects this
management API.

## List links

```sh
curl --fail-with-body \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  "$BASE_URL/api/links"
```

Response: `200 OK` with an array of link summaries.

## Get one link

```sh
curl --fail-with-body \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  "$BASE_URL/api/links/LINK_ID"
```

Response: `200 OK`. An unknown ID returns `404 Not Found`.

## Create an admin link

The `scope.type` value must be `admin` or `user`. An admin link can access all
shared files.

```sh
curl --fail-with-body -X POST \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H 'Content-Type: application/json' \
  "$BASE_URL/api/links" \
  -d '{
    "scope": {"type": "admin"},
    "password": "abcabc",
    "description": "temporary admin access",
    "expires_at": "2026-12-31T23:59:59Z"
  }'
```

Response: `201 Created`. The response includes the generated `id` and public
`url`. Omit `password` or send an empty string for an open link. Omit
`expires_at` for a link that does not expire.

## Create a user-scoped link

```sh
curl --fail-with-body -X POST \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H 'Content-Type: application/json' \
  "$BASE_URL/api/links" \
  -d '{
    "scope": {"type": "user", "user": "jonathan"},
    "password": "user-secret",
    "description": "Jonathan upload review"
  }'
```

Usernames must match the SFTP username format. `expires_at`, when present,
must be a future RFC 3339 timestamp; it is stored in UTC.

## Update a link

`PATCH` accepts any combination of `password`, `expires_at`, and `description`.
Only fields included in the JSON body change.

```sh
curl --fail-with-body -X PATCH \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H 'Content-Type: application/json' \
  "$BASE_URL/api/links/LINK_ID" \
  -d '{"password":"new-secret","description":"rotated access"}'
```

Use an empty password to remove protection:

```sh
-d '{"password":""}'
```

Use an empty `expires_at` to remove an expiry, or provide a future RFC 3339
timestamp to replace it:

```sh
-d '{"expires_at":""}'
-d '{"expires_at":"2026-12-31T23:59:59Z"}'
```

Response: `200 OK` with the updated link. An unknown ID returns `404 Not Found`.

## Delete a link

```sh
curl --fail-with-body -X DELETE \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  "$BASE_URL/api/links/LINK_ID"
```

Response: `204 No Content`.

## Link access URLs

The API creates links whose public page is:

```text
PUBLIC_URL/l/LINK_ID
```

Open links can be viewed directly. Protected links require the link password;
the password is hashed by the service and is never returned by the API.

## Errors

| Status | Meaning |
|---|---|
| `400 Bad Request` | Invalid JSON, scope, password length, or expiry value. |
| `401 Unauthorized` | Missing or incorrect bearer token. |
| `404 Not Found` | The requested link ID does not exist. |
| `500 Internal Server Error` | The service could not persist or process the request. |

Passwords are limited to 72 bytes. Do not place `ADMIN_TOKEN` in URLs, JSON
bodies, shell history, source control, or client-side code.

## Rotating `ADMIN_TOKEN`

The token can be changed from the admin UI's **Settings** page by entering a
new non-empty value and saving settings. The file-sharing service reads the
shared configuration for each request, so the new token takes effect without a
restart and the old token stops working immediately.

The API accepts only one token at a time, so rotation requires a brief
coordinated switchover:

1. Generate and stage the new token in the API clients' secret stores.
2. Save the new token in **Settings**.
3. Restart or reload API clients so they use the new token.
4. Verify one authenticated request with the new token.
5. Remove the old token from clients and secret stores.

There is no dual-token overlap period; clients using the old token fail as
soon as the new value is saved.

Changing the token also invalidates existing protected-link sessions because
those sessions are signed with the same token. Users may need to enter their
link passwords again.
