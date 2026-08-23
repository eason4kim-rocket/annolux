# SSH hardening runbook

This change is deliberately staged. Creating a dedicated Ed25519 deployment
identity and confirming provider-console recovery are `WAITING_USER`; password
root login must stay enabled until every verification below passes.

1. Generate a dedicated Ed25519 key on the trusted operator machine. Do not
   reuse a personal key and do not put either path or material in Git.
2. Add the public key to a non-root deployment user with only the Docker and
   reviewed `/opt/annolux` permissions it needs. Keep destructive data commands
   outside that role.
3. In a second terminal, prove a fresh key-only login, host-key verification,
   `docker compose config`, read-only container inspection, and one safe
   deployment restart. Keep the original session open.
4. Prove that the VPS provider console or rescue workflow is accessible from a
   separate browser session and record recovery ownership outside Git.
5. Back up the active SSH configuration and validate changes with
   `sshd -t`. Disable password authentication and direct root login only after
   steps 3 and 4 pass.
6. Open a third new session after reload. If it fails, restore through the
   still-open session or provider console. Do not close either recovery path
   until the new session and deployment workflow both pass.

No automation in this repository edits `sshd_config`, firewall rules,
authorized keys, or provider-console state.
