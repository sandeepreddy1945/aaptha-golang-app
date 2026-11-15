# Aaptha Backend – API & curl Guide

Go (gofr) backend that stores caregiver location data in SQLite (`locations.db`) and sends push notifications through Firebase Cloud Messaging.

## Run

```bash
cd /Users/vn59wmn/projects/learning/golang-apps/example
go run .          # NOT `go run main.go` – the package spans several files
```

Run from the project directory so `./configs/.env` and the service-account key resolve. The server listens on port `8000` (gofr metrics on `2121`).

Set these once so the curls below can be pasted as-is:

```bash
export BASE=http://localhost:8000      # from a phone/emulator use http://192.168.0.115:8000
export KEY=change-me                   # ADMIN_API_KEY in configs/.env
```

## Configuration (`configs/.env`)

| Key                         | Purpose                                                              |
| --------------------------- | -------------------------------------------------------------------- |
| `HTTP_PORT`                 | HTTP port (default `8000`)                                           |
| `DB_DIALECT` / `DB_NAME`    | `sqlite` / `locations` (creates `locations.db` in the working dir)   |
| `ADMIN_API_KEY`             | Shared key required in the `X-API-Key` header for admin endpoints    |
| `FIREBASE_CREDENTIALS_FILE` | Path to the Firebase service-account JSON (`./configs/<key>.json`)   |
| `FIREBASE_PROJECT_ID`       | `aaptha-mob-app`                                                     |

`configs/.env`, `configs/*.json` and `*.db` are git-ignored. Change `ADMIN_API_KEY` from `change-me` before exposing the server.

## Authentication

| Endpoint                     | Auth                      |
| ---------------------------- | ------------------------- |
| `POST /locations`            | None (called by the app)  |
| Everything else              | `X-API-Key: $KEY` header  |

## Endpoint summary

| Method   | Path                              | Purpose                                              |
| -------- | --------------------------------- | ---------------------------------------------------- |
| `POST`   | `/locations`                      | Store a single or batch location payload             |
| `GET`    | `/locations/{employeeId}`         | Location history for one employee                    |
| `GET`    | `/locations/{employeeId}/latest`  | Latest location for one employee                     |
| `GET`    | `/employees`                      | Employees, last location, and registered Firebase users |
| `GET`    | `/employees/count`                | Registered-user and location counts                  |
| `POST`   | `/notifications`                  | Send a push notification                             |
| `DELETE` | `/locations/{employeeId}`         | Delete one employee's locations (dev/testing)        |
| `DELETE` | `/locations?confirm=true`         | Delete all locations (dev/testing)                   |

## 1. Post location data

```bash
# Batch (same shape the app sends, plus employeeId and uid)
curl -X POST $BASE/locations -H 'Content-Type: application/json' -d '{
  "employeeId": "AAP-1048",
  "uid": "<firebase-uid>",
  "platform": "android",
  "sessionId": "sess-1",
  "sentAt": "2026-11-16T10:00:00Z",
  "locations": [
    {"latitude": 12.9716, "longitude": 77.5946, "accuracy": 10, "altitude": null, "speed": 0, "heading": 0, "timestamp": 1789000000000},
    {"latitude": 12.9720, "longitude": 77.5950, "accuracy": 8,  "altitude": null, "speed": 1, "heading": 90, "timestamp": 1789000120000}
  ]}'

# Single location
curl -X POST $BASE/locations -H 'Content-Type: application/json' \
  -d '{"employeeId":"AAP-1048","latitude":12.9716,"longitude":77.5946,"timestamp":1789000240000}'
```

- `employeeId` is required (400 if missing). `latitude` must be -90..90 and `longitude` -180..180.
- `uid` (Firebase auth uid) is optional. It lets notifications target the employee by `employeeId`.
- `timestamp` is epoch milliseconds (seconds are auto-converted; `0` or missing means "now").
- Re-sending the same point is ignored and reported as `"duplicates"`, so app retries are safe.
- Max 5000 locations per request.

Response:

```json
{"data": {"employeeId": "AAP-1048", "received": 2, "stored": 2, "duplicates": 0}}
```

## 2. View location data

```bash
# History for one employee (limit ≤ 1000, offset, from, to, order=asc|desc)
curl -H "X-API-Key: $KEY" "$BASE/locations/AAP-1048?limit=50&order=desc"
curl -H "X-API-Key: $KEY" "$BASE/locations/AAP-1048?from=2026-11-01T00:00:00Z&to=2026-11-30T00:00:00Z"

# Latest position (404 if the employee has no locations)
curl -H "X-API-Key: $KEY" $BASE/locations/AAP-1048/latest
```

`from` / `to` accept epoch milliseconds, epoch seconds, or RFC3339.

## 3. Registered users and counts

```bash
# registeredUsers / deviceTokens come from Firestore; the other counts come from SQLite
curl -H "X-API-Key: $KEY" $BASE/employees/count

# All employees, including registered Firebase uids that haven't posted a location yet
curl -H "X-API-Key: $KEY" $BASE/employees
```

`/employees/count` fields:

| Field                    | Meaning                                                           |
| ------------------------ | ----------------------------------------------------------------- |
| `registeredUsers`        | Distinct Firebase uids with an FCM token in `users/{uid}/fcmTokens` |
| `deviceTokens`           | Total FCM tokens in Firestore                                     |
| `employeesWithLocations` | Distinct employee IDs that have posted locations                  |
| `mappedEmployees`        | Employees whose `employeeId` is linked to a Firebase `uid`        |
| `totalLocations`         | Rows in the locations table                                       |
| `warning`                | Present if Firebase isn't configured or the Firestore read failed |

In `/employees`, registered users that haven't posted a location appear with `"employeeId": null`.

## 4. Send notifications

```bash
# One employee (needs the uid link from POST /locations)
curl -X POST $BASE/notifications -H "X-API-Key: $KEY" -H 'Content-Type: application/json' \
  -d '{"employeeId":"AAP-1048","title":"Shift update","body":"Your shift starts at 9am","data":{"screen":"notifications"}}'

# By Firebase uid (copy it from GET /employees)
curl -X POST $BASE/notifications -H "X-API-Key: $KEY" -H 'Content-Type: application/json' \
  -d '{"uid":"<firebase-uid>","title":"Test","body":"Hello from Go"}'

# Several employees
curl -X POST $BASE/notifications -H "X-API-Key: $KEY" -H 'Content-Type: application/json' \
  -d '{"employeeIds":["AAP-1048","AAP-2000"],"title":"Policy","body":"New policy posted"}'

# Everyone registered
curl -X POST $BASE/notifications -H "X-API-Key: $KEY" -H 'Content-Type: application/json' \
  -d '{"all":true,"title":"Announcement","body":"Hello team"}'
```

Request fields:

| Field                  | Notes                                                       |
| ---------------------- | ----------------------------------------------------------- |
| `title`, `body`        | At least one is required                                    |
| `employeeId` / `employeeIds` | Resolved to a Firebase uid via the mapping            |
| `uid` / `uids`         | Firebase auth uid(s)                                        |
| `all`                  | `true` sends to every registered token                      |
| `data`                 | Optional string-to-string map delivered with the message    |
| `imageUrl`             | Optional notification image                                 |

The response contains `targetedTokens`, `sent`, `failed`, a per-token `results` list, `employeeIdsWithoutUid` and `uidsWithoutTokens`. Tokens that FCM reports as unregistered are deleted from Firestore automatically.

Tokens only reach Firestore from a native dev build (`expo run:android` / `run:ios`), not Expo Go, on a physical device with notification permission granted.

## 5. Dev-only deletes

```bash
# Delete one employee's locations
curl -i -X DELETE -H "X-API-Key: $KEY" $BASE/locations/AAP-1048

# Delete all locations (requires confirm=true, otherwise 400)
curl -i -X DELETE -H "X-API-Key: $KEY" "$BASE/locations?confirm=true"
```

Both return `204 No Content` with no body, so use `-i`. The number of rows removed is in the `X-Deleted-Count` response header. These only delete locations, not the employee↔uid mapping or Firebase tokens.

## Troubleshooting

| Status / symptom                        | Cause                                                                              |
| --------------------------------------- | ---------------------------------------------------------------------------------- |
| `401` missing or invalid `X-API-Key`    | Header missing or wrong; check `ADMIN_API_KEY` in `configs/.env`                   |
| `503` from `/notifications`             | Firebase isn't configured; check `FIREBASE_CREDENTIALS_FILE` and the working dir   |
| `502` from `/notifications`             | Firestore read failed; check the service account's permissions and enabled APIs    |
| `400` from `POST /locations`            | `employeeId` missing, or coordinates out of range                                  |
| `404` from `/locations/{id}/latest`     | No locations stored for that employee                                              |
| `undefined: server` on `go run main.go` | Use `go run .` – the package spans several files                                   |
