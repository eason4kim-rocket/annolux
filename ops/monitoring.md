# Monitoring and status

External monitoring is an account-side launch gate. Code and endpoints are
ready; creating the Better Stack account, monitor IDs, heartbeat URLs, and
custom status-domain verification is `WAITING_USER`.

## External monitors

Create checks from at least two regions:

| Name | URL | Interval | Success condition |
|---|---|---:|---|
| Website | `https://annolux.com/healthz` | 60 s | HTTP 200, body `ok` |
| API readiness | `https://api.annolux.com/api/v1/ready` | 60 s | HTTP 200, JSON `status=ready` |
| Static route | `https://annolux.com/docs` | 5 min | HTTP 200 and `Annolux` in body |

The API readiness endpoint validates both `accounts.db` and a nonempty,
queryable index. It requires no API key and must never expose row counts,
filesystem paths, or database errors. Monitor Search separately only with a
dedicated free test account because every successful Search consumes one
credit; use `ops/smoke.sh` deliberately rather than a 60-second paid monitor.

Create `status.annolux.com` as the independent public status page. Publish only
service-level state for Website, Search API, Login, and Billing; do not publish
hostnames, ports, database paths, or vendor tokens. Link incident updates from
the Status page only after the independent page exists.

## Job heartbeats

Configure distinct HTTPS heartbeat URLs in the ignored host `.env`:

```text
ANNOLUX_HEARTBEAT_BACKUP_URL=
ANNOLUX_HEARTBEAT_RECONCILE_URL=
ANNOLUX_HEARTBEAT_REFRESH_URL=
```

Expected schedules and overdue tolerances:

| Job | Schedule | Alert after |
|---|---|---:|
| Encrypted accounts backup | every 6 hours | 8 hours |
| Paddle reconciliation | daily | 30 hours |
| Existing-page refresh | weekly | 9 days |

The systemd units send a heartbeat only after the command exits successfully.
An absent heartbeat must alert privately; it must not automatically restart an
indexer or mutate billing state.

## Operator metrics

Run `docker exec "${ANNOLUX_API_CONTAINER_NAME:-annolux-api}" annolux-admin metrics` for aggregate launch
signals. The output intentionally contains no email, identity, key prefix,
query, domain, IP address, or request ID. The beta release gate is at least ten
accounts with a committed Search and at least three accounts with committed
Searches on two distinct UTC days.
