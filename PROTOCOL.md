# Toss protocol (v1)

The computer runs `toss`, an HTTPS server on port 47800 with a self-signed certificate.
The phone trusts the server only if the SHA-256 of its certificate (DER) matches the
fingerprint it learned from the pairing QR code, so nobody else on the network can pose
as the computer. Every request after pairing carries the device key.

## Pairing link (the QR code)

    toss://pair?v=1&n=<computer name>&h=<host>,<host>…&p=<port>&fp=<cert fingerprint>&t=<pairing token>

- `h`: hosts to try in order: `<hostname>.local` first, then the computer's private IPv4 addresses.
- `fp`: base64url (no padding) SHA-256 of the certificate's DER bytes.
- `t`: one-time token, base64url, valid while `toss pair` runs (10 minutes at most).

The iOS Camera app opens Toss for `toss://` links, so scanning works from the Camera too.

## Endpoints

All bodies are JSON unless noted. Errors: non-2xx with `{"error": "…"}`.

### `POST /v1/pair`
Request: `{"token": "…", "deviceName": "Hiha", "deviceId": "<uuid>"}`
Response: `{"deviceKey": "<base64url 32 bytes>", "computerName": "…", "computerId": "<uuid>"}`
The token works once. The computer stores only the SHA-256 of the device key.

### `GET /v1/ping`
Header `Authorization: Bearer <deviceKey>`. Response: `{"computerName": "…", "version": "…"}`.

### `POST /v1/items`
Headers:
- `Authorization: Bearer <deviceKey>`
- `X-Toss-Kind: text | url | file`
- `X-Toss-Name: <percent-encoded file name>` (files)
- `X-Toss-Batch: <uuid>`, `X-Toss-Index: <0-based>`, `X-Toss-Count: <n>`: items shared together, so the
  computer can show one notification for "3 photos".
- `Content-Length`

- `X-Toss-Modified: <RFC 3339>` (files, optional): when the photo or video was taken; the computer
  sets the saved file's modification time to it.
- Resumable files (optional): `X-Toss-Upload: <id>` (8–64 of `[A-Za-z0-9_-]`), `X-Toss-Size: <total bytes>`,
  `X-Toss-Offset: <bytes already sent>`. See below.

Body: the raw text (UTF-8), the URL, or the file's bytes.
Response: `{"saved": "<path on the computer>"}` for files, `{}` otherwise.

### Resumable uploads
With `X-Toss-Upload`, the computer appends the body to a partial file for that id and keeps it if
the connection drops (for 48 hours). The answer is `200` with `saved` once `X-Toss-Size` bytes are
there, or `409 {"error": …, "received": n}` when the file is still incomplete or `X-Toss-Offset`
isn't what it has. `GET /v1/uploads/{id}` → `{"received": n}`. The phone then sends the rest with
`X-Toss-Offset: n`. Computers that don't know these headers simply save the body.

### `GET /v1/clipboard`
Header `Authorization: Bearer <deviceKey>`. Response: `{"text": "<the computer's clipboard text>"}`
(empty when the clipboard holds no text). Used by the phone's "Get computer's clipboard" action.

### Inbox: computer → phone
Things sent with `toss send`, the file manager's "Send to iPhone" or the local page wait on the
computer for 7 days, because iOS can't receive in the background without Apple's push service.

- `GET /v1/inbox` → `{"items": [{"id", "kind": "text|url|file", "text"?, "name"?, "size", "mime"?, "created"}]}`
- `GET /v1/inbox/{id}` → the file's bytes (`X-Toss-Name`, Range requests supported) or the text.
- `DELETE /v1/inbox/{id}` → `{}`: the phone has it; the computer drops it. The phone saves before
  deleting, so a lost reply can cause a duplicate but never a loss.

All three need the device key. An inbox item goes to whichever paired phone picks it up first.
