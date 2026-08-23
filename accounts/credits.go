package accounts

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

func (store *Store) GrantCredits(ctx context.Context, accountID, source string, units int64, startsAt time.Time, expiresAt *time.Time, externalRef string) (CreditGrant, bool, error) {
	if ctx == nil || strings.TrimSpace(accountID) == "" || (source != "signup" && source != "subscription" && source != "admin") || units <= 0 || units > 1_000_000_000 || strings.TrimSpace(externalRef) == "" || len(externalRef) > 512 {
		return CreditGrant{}, false, ErrInvalidInput
	}
	startsAt = startsAt.UTC().Round(0)
	if startsAt.IsZero() {
		return CreditGrant{}, false, ErrInvalidInput
	}
	var canonicalExpiry *time.Time
	if expiresAt != nil {
		value := expiresAt.UTC().Round(0)
		if !value.After(startsAt) {
			return CreditGrant{}, false, ErrInvalidInput
		}
		canonicalExpiry = &value
	}
	grantID, err := store.newID("grant_")
	if err != nil {
		return CreditGrant{}, false, err
	}
	ledgerID, err := store.newID("led_")
	if err != nil {
		return CreditGrant{}, false, err
	}
	now := store.now().UTC().Round(0)
	grant := CreditGrant{
		ID: grantID, AccountID: accountID, Source: source, Original: units, Remaining: units,
		StartsAt: startsAt, ExpiresAt: canonicalExpiry, ExternalRef: externalRef, CreatedAt: now,
	}
	created := false
	err = store.withWriteTx(ctx, func(tx *sql.Tx) error {
		existing, found, err := getGrantByExternalRef(ctx, tx, externalRef)
		if err != nil {
			return err
		}
		if found {
			if existing.AccountID != accountID || existing.Source != source || existing.Original != units || !existing.StartsAt.Equal(startsAt) || !equalOptionalTime(existing.ExpiresAt, canonicalExpiry) {
				return ErrConflict
			}
			grant = existing
			return nil
		}
		var status AccountStatus
		if err := tx.QueryRowContext(ctx, `SELECT status FROM accounts WHERE id=?`, accountID).Scan(&status); err != nil {
			if isNoRows(err) {
				return ErrNotFound
			}
			return err
		}
		if status != AccountActive {
			return ErrSuspended
		}
		var expiry any
		if canonicalExpiry != nil {
			expiry = canonicalExpiry.Unix()
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO credit_grants(id,account_id,source,original_units,remaining_units,starts_at,expires_at,external_ref,created_at) VALUES(?,?,?,?,?,?,?,?,?)`,
			grant.ID, grant.AccountID, grant.Source, grant.Original, grant.Remaining, grant.StartsAt.Unix(), expiry, grant.ExternalRef, grant.CreatedAt.Unix()); err != nil {
			return translateConstraint(err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO credit_ledger(id,account_id,kind,units,grant_id,reservation_id,external_ref,created_at) VALUES(?,?,?,?,?,NULL,?,?)`,
			ledgerID, accountID, "grant", units, grant.ID, externalRef, now.Unix()); err != nil {
			return translateConstraint(err)
		}
		created = true
		return nil
	})
	return grant, created, err
}

func (store *Store) ReserveCredits(ctx context.Context, accountID, apiKeyID, requestID string, units int64) (CreditReservation, int64, error) {
	if strings.TrimSpace(apiKeyID) == "" {
		return CreditReservation{}, 0, ErrInvalidInput
	}
	return store.reserveCredits(ctx, accountID, apiKeyID, requestID, units)
}

func (store *Store) ReserveSessionCredits(ctx context.Context, accountID, requestID string, units int64) (CreditReservation, int64, error) {
	return store.reserveCredits(ctx, accountID, "", requestID, units)
}

func (store *Store) reserveCredits(ctx context.Context, accountID, apiKeyID, requestID string, units int64) (CreditReservation, int64, error) {
	if ctx == nil || strings.TrimSpace(accountID) == "" || strings.TrimSpace(requestID) == "" || len(requestID) > 256 || units <= 0 || units > 1000 {
		return CreditReservation{}, 0, ErrInvalidInput
	}
	reservationID, err := store.newID("res_")
	if err != nil {
		return CreditReservation{}, 0, err
	}
	now := store.now().UTC().Round(0)
	reservation := CreditReservation{ID: reservationID, AccountID: accountID, APIKeyID: apiKeyID, RequestID: requestID, Units: units, State: "pending", CreatedAt: now}
	var remaining int64
	err = store.withWriteTx(ctx, func(tx *sql.Tx) error {
		existing, found, err := getReservationByRequestID(ctx, tx, requestID)
		if err != nil {
			return err
		}
		if found {
			if existing.AccountID != accountID || existing.APIKeyID != apiKeyID || existing.Units != units {
				return ErrConflict
			}
			reservation = existing
			remaining, err = availableCreditsTx(ctx, tx, accountID, now)
			return err
		}
		var status AccountStatus
		var keyActive = 1
		var identityErr error
		if apiKeyID == "" {
			identityErr = tx.QueryRowContext(ctx, `SELECT status FROM accounts WHERE id=?`, accountID).Scan(&status)
		} else {
			identityErr = tx.QueryRowContext(ctx, `SELECT a.status, CASE WHEN k.revoked_at IS NULL THEN 1 ELSE 0 END FROM accounts a JOIN api_keys k ON k.account_id=a.id WHERE a.id=? AND k.id=?`, accountID, apiKeyID).Scan(&status, &keyActive)
		}
		if identityErr != nil {
			if isNoRows(identityErr) {
				return ErrNotFound
			}
			return identityErr
		}
		if status != AccountActive || keyActive != 1 {
			return ErrSuspended
		}
		rows, err := tx.QueryContext(ctx, `
			SELECT id,remaining_units FROM credit_grants
			WHERE account_id=? AND remaining_units>0 AND starts_at<=? AND (expires_at IS NULL OR expires_at>?)
			ORDER BY expires_at IS NULL, expires_at, created_at, id`, accountID, now.Unix(), now.Unix())
		if err != nil {
			return err
		}
		type allocation struct {
			grantID string
			units   int64
		}
		allocations := make([]allocation, 0, 2)
		needed := units
		for rows.Next() && needed > 0 {
			var grantID string
			var available int64
			if err := rows.Scan(&grantID, &available); err != nil {
				rows.Close()
				return err
			}
			take := min(available, needed)
			allocations = append(allocations, allocation{grantID: grantID, units: take})
			needed -= take
		}
		if err := rows.Close(); err != nil {
			return err
		}
		if needed != 0 {
			return ErrInsufficientCredits
		}
		var keyValue any
		if apiKeyID != "" {
			keyValue = apiKeyID
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO credit_reservations(id,account_id,api_key_id,request_id,units,state,created_at,settled_at) VALUES(?,?,?,?,?,'pending',?,NULL)`,
			reservation.ID, accountID, keyValue, requestID, units, now.Unix()); err != nil {
			return translateConstraint(err)
		}
		for _, allocation := range allocations {
			result, err := tx.ExecContext(ctx, `UPDATE credit_grants SET remaining_units=remaining_units-? WHERE id=? AND remaining_units>=?`, allocation.units, allocation.grantID, allocation.units)
			if err != nil {
				return err
			}
			affected, _ := result.RowsAffected()
			if affected != 1 {
				return ErrConflict
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO credit_reservation_allocations(reservation_id,grant_id,units) VALUES(?,?,?)`, reservation.ID, allocation.grantID, allocation.units); err != nil {
				return err
			}
		}
		remaining, err = availableCreditsTx(ctx, tx, accountID, now)
		return err
	})
	return reservation, remaining, err
}

func (store *Store) CommitReservation(ctx context.Context, reservationID string) (int64, error) {
	return store.settleReservation(ctx, reservationID, true)
}

func (store *Store) ReleaseReservation(ctx context.Context, reservationID string) (int64, error) {
	return store.settleReservation(ctx, reservationID, false)
}

func (store *Store) settleReservation(ctx context.Context, reservationID string, commit bool) (int64, error) {
	if ctx == nil || strings.TrimSpace(reservationID) == "" {
		return 0, ErrInvalidInput
	}
	now := store.now().UTC().Round(0)
	var remaining int64
	err := store.withWriteTx(ctx, func(tx *sql.Tx) error {
		var reservation CreditReservation
		var createdAt int64
		var settledAt sql.NullInt64
		var apiKeyID sql.NullString
		err := tx.QueryRowContext(ctx, `SELECT id,account_id,api_key_id,request_id,units,state,created_at,settled_at FROM credit_reservations WHERE id=?`, reservationID).
			Scan(&reservation.ID, &reservation.AccountID, &apiKeyID, &reservation.RequestID, &reservation.Units, &reservation.State, &createdAt, &settledAt)
		if isNoRows(err) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		reservation.APIKeyID = nullableString(apiKeyID)
		desired := "released"
		if commit {
			desired = "committed"
		}
		if reservation.State == desired {
			remaining, err = availableCreditsTx(ctx, tx, reservation.AccountID, now)
			return err
		}
		if reservation.State != "pending" {
			return ErrConflict
		}
		if commit {
			var status AccountStatus
			var keyActive = 1
			var authorizationErr error
			if reservation.APIKeyID == "" {
				authorizationErr = tx.QueryRowContext(ctx, `SELECT status FROM accounts WHERE id=?`, reservation.AccountID).Scan(&status)
			} else {
				authorizationErr = tx.QueryRowContext(ctx, `SELECT a.status,CASE WHEN k.revoked_at IS NULL THEN 1 ELSE 0 END FROM accounts a JOIN api_keys k ON k.account_id=a.id WHERE a.id=? AND k.id=?`, reservation.AccountID, reservation.APIKeyID).Scan(&status, &keyActive)
			}
			if authorizationErr != nil {
				if isNoRows(authorizationErr) {
					return ErrSuspended
				}
				return authorizationErr
			}
			if status != AccountActive || keyActive != 1 {
				return ErrSuspended
			}
			ledgerID, err := store.newID("led_")
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO credit_ledger(id,account_id,kind,units,grant_id,reservation_id,external_ref,created_at) VALUES(?,?,? ,?,NULL,?,?,?)`,
				ledgerID, reservation.AccountID, "usage", -reservation.Units, reservation.ID, "usage:"+reservation.ID, now.Unix()); err != nil {
				return translateConstraint(err)
			}
		} else {
			rows, err := tx.QueryContext(ctx, `SELECT grant_id,units FROM credit_reservation_allocations WHERE reservation_id=?`, reservation.ID)
			if err != nil {
				return err
			}
			type allocation struct {
				grantID string
				units   int64
			}
			var allocations []allocation
			for rows.Next() {
				var value allocation
				if err := rows.Scan(&value.grantID, &value.units); err != nil {
					rows.Close()
					return err
				}
				allocations = append(allocations, value)
			}
			if err := rows.Close(); err != nil {
				return err
			}
			for _, allocation := range allocations {
				if _, err := tx.ExecContext(ctx, `UPDATE credit_grants SET remaining_units=remaining_units+? WHERE id=?`, allocation.units, allocation.grantID); err != nil {
					return err
				}
			}
		}
		if _, err := tx.ExecContext(ctx, `UPDATE credit_reservations SET state=?,settled_at=? WHERE id=? AND state='pending'`, desired, now.Unix(), reservation.ID); err != nil {
			return err
		}
		remaining, err = availableCreditsTx(ctx, tx, reservation.AccountID, now)
		return err
	})
	return remaining, err
}

// ReleaseStaleReservations repairs credits left reserved when a process dies
// after the durable reservation but before commit/release. The minimum age is
// deliberately longer than the public request timeout, so a live request can
// never be reclaimed by the recovery loop.
func (store *Store) ReleaseStaleReservations(ctx context.Context, olderThan time.Duration) (int, error) {
	if ctx == nil || olderThan < time.Minute || olderThan > 24*time.Hour {
		return 0, ErrInvalidInput
	}
	now := store.now().UTC().Round(0)
	cutoff := now.Add(-olderThan).Unix()
	released := 0
	err := store.withWriteTx(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT id FROM credit_reservations WHERE state='pending' AND created_at<=? ORDER BY created_at,id`, cutoff)
		if err != nil {
			return err
		}
		var reservationIDs []string
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			reservationIDs = append(reservationIDs, id)
		}
		if err := rows.Close(); err != nil {
			return err
		}
		for _, reservationID := range reservationIDs {
			allocationRows, err := tx.QueryContext(ctx, `SELECT grant_id,units FROM credit_reservation_allocations WHERE reservation_id=?`, reservationID)
			if err != nil {
				return err
			}
			type allocation struct {
				grantID string
				units   int64
			}
			var allocations []allocation
			for allocationRows.Next() {
				var item allocation
				if err := allocationRows.Scan(&item.grantID, &item.units); err != nil {
					allocationRows.Close()
					return err
				}
				allocations = append(allocations, item)
			}
			if err := allocationRows.Close(); err != nil {
				return err
			}
			for _, item := range allocations {
				if _, err := tx.ExecContext(ctx, `UPDATE credit_grants SET remaining_units=remaining_units+? WHERE id=?`, item.units, item.grantID); err != nil {
					return err
				}
			}
			result, err := tx.ExecContext(ctx, `UPDATE credit_reservations SET state='released',settled_at=? WHERE id=? AND state='pending'`, now.Unix(), reservationID)
			if err != nil {
				return err
			}
			affected, err := result.RowsAffected()
			if err != nil {
				return err
			}
			if affected != 1 {
				return ErrConflict
			}
			released++
		}
		return nil
	})
	return released, err
}

func (store *Store) CreditSummary(ctx context.Context, accountID string) (CreditSummary, error) {
	if store == nil || store.db == nil || ctx == nil || strings.TrimSpace(accountID) == "" {
		return CreditSummary{}, ErrInvalidInput
	}
	now := store.now().UTC().Round(0)
	var summary CreditSummary
	err := store.db.QueryRowContext(ctx, `SELECT
		COALESCE(SUM(CASE WHEN starts_at<=? AND (expires_at IS NULL OR expires_at>?) THEN remaining_units ELSE 0 END),0),
		COALESCE(SUM(original_units),0)
		FROM credit_grants WHERE account_id=?`, now.Unix(), now.Unix(), accountID).Scan(&summary.Available, &summary.Granted)
	if err != nil {
		return CreditSummary{}, err
	}
	if err := store.db.QueryRowContext(ctx, `SELECT
		COALESCE(-SUM(CASE WHEN kind='usage' THEN units ELSE 0 END),0),
		COALESCE(-SUM(CASE WHEN kind='expiry' THEN units ELSE 0 END),0),
		COALESCE(-SUM(CASE WHEN kind='revocation' THEN units ELSE 0 END),0)
		FROM credit_ledger WHERE account_id=?`, accountID).Scan(&summary.Used, &summary.Expired, &summary.Revoked); err != nil {
		return CreditSummary{}, err
	}
	if err := store.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(units),0) FROM credit_reservations WHERE account_id=? AND state='pending'`, accountID).Scan(&summary.Reserved); err != nil {
		return CreditSummary{}, err
	}
	rows, err := store.db.QueryContext(ctx, `SELECT id,account_id,source,original_units,remaining_units,starts_at,expires_at,external_ref,created_at FROM credit_grants WHERE account_id=? ORDER BY starts_at,created_at,id`, accountID)
	if err != nil {
		return CreditSummary{}, err
	}
	defer rows.Close()
	for rows.Next() {
		grant, err := scanGrant(rows)
		if err != nil {
			return CreditSummary{}, err
		}
		summary.Grants = append(summary.Grants, grant)
	}
	return summary, rows.Err()
}

func (store *Store) ExpireCredits(ctx context.Context) (int, error) {
	if ctx == nil {
		return 0, ErrInvalidInput
	}
	now := store.now().UTC().Round(0)
	expired := 0
	err := store.withWriteTx(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT id,account_id,remaining_units FROM credit_grants WHERE remaining_units>0 AND expires_at IS NOT NULL AND expires_at<=? ORDER BY id`, now.Unix())
		if err != nil {
			return err
		}
		type item struct {
			id, accountID string
			units         int64
		}
		var items []item
		for rows.Next() {
			var value item
			if err := rows.Scan(&value.id, &value.accountID, &value.units); err != nil {
				rows.Close()
				return err
			}
			items = append(items, value)
		}
		if err := rows.Close(); err != nil {
			return err
		}
		for _, value := range items {
			ledgerID, err := store.newID("led_")
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `UPDATE credit_grants SET remaining_units=0 WHERE id=? AND remaining_units=?`, value.id, value.units); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO credit_ledger(id,account_id,kind,units,grant_id,reservation_id,external_ref,created_at) VALUES(?,?,?,?,?,NULL,?,?)`,
				ledgerID, value.accountID, "expiry", -value.units, value.id, "expiry:"+value.id, now.Unix()); err != nil {
				return translateConstraint(err)
			}
			expired++
		}
		return nil
	})
	return expired, err
}

// RevokeGrant removes only unused credits associated with an external billing
// event. Already consumed credits are never driven negative.
func (store *Store) RevokeGrant(ctx context.Context, externalRef, reasonRef string) (int64, error) {
	if ctx == nil || strings.TrimSpace(externalRef) == "" || strings.TrimSpace(reasonRef) == "" {
		return 0, ErrInvalidInput
	}
	now := store.now().UTC().Round(0)
	var revoked int64
	err := store.withWriteTx(ctx, func(tx *sql.Tx) error {
		grant, found, err := getGrantByExternalRef(ctx, tx, externalRef)
		if err != nil {
			return err
		}
		if !found {
			return ErrNotFound
		}
		revocationRef := "revocation:" + reasonRef
		var existing int
		err = tx.QueryRowContext(ctx, `SELECT 1 FROM credit_ledger WHERE external_ref=?`, revocationRef).Scan(&existing)
		if err == nil {
			return nil
		}
		if !isNoRows(err) {
			return err
		}
		revoked = grant.Remaining
		if revoked == 0 {
			return nil
		}
		ledgerID, err := store.newID("led_")
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE credit_grants SET remaining_units=0 WHERE id=?`, grant.ID); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO credit_ledger(id,account_id,kind,units,grant_id,reservation_id,external_ref,created_at) VALUES(?,?,?,?,?,NULL,?,?)`,
			ledgerID, grant.AccountID, "revocation", -revoked, grant.ID, revocationRef, now.Unix())
		return translateConstraint(err)
	})
	return revoked, err
}

func availableCreditsTx(ctx context.Context, tx *sql.Tx, accountID string, now time.Time) (int64, error) {
	var available int64
	err := tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(remaining_units),0) FROM credit_grants WHERE account_id=? AND remaining_units>0 AND starts_at<=? AND (expires_at IS NULL OR expires_at>?)`, accountID, now.Unix(), now.Unix()).Scan(&available)
	return available, err
}

func getGrantByExternalRef(ctx context.Context, tx *sql.Tx, externalRef string) (CreditGrant, bool, error) {
	row := tx.QueryRowContext(ctx, `SELECT id,account_id,source,original_units,remaining_units,starts_at,expires_at,external_ref,created_at FROM credit_grants WHERE external_ref=?`, externalRef)
	grant, err := scanGrant(row)
	if isNoRows(err) {
		return CreditGrant{}, false, nil
	}
	return grant, err == nil, err
}

func scanGrant(row interface{ Scan(...any) error }) (CreditGrant, error) {
	var grant CreditGrant
	var startsAt, createdAt int64
	var expiresAt sql.NullInt64
	err := row.Scan(&grant.ID, &grant.AccountID, &grant.Source, &grant.Original, &grant.Remaining, &startsAt, &expiresAt, &grant.ExternalRef, &createdAt)
	if err != nil {
		return CreditGrant{}, err
	}
	grant.StartsAt, grant.CreatedAt = unixTime(startsAt), unixTime(createdAt)
	grant.ExpiresAt = nullableTime(expiresAt)
	return grant, nil
}

func getReservationByRequestID(ctx context.Context, tx *sql.Tx, requestID string) (CreditReservation, bool, error) {
	var reservation CreditReservation
	var createdAt int64
	var settledAt sql.NullInt64
	var apiKeyID sql.NullString
	err := tx.QueryRowContext(ctx, `SELECT id,account_id,api_key_id,request_id,units,state,created_at,settled_at FROM credit_reservations WHERE request_id=?`, requestID).
		Scan(&reservation.ID, &reservation.AccountID, &apiKeyID, &reservation.RequestID, &reservation.Units, &reservation.State, &createdAt, &settledAt)
	if isNoRows(err) {
		return CreditReservation{}, false, nil
	}
	if err != nil {
		return CreditReservation{}, false, err
	}
	reservation.APIKeyID = nullableString(apiKeyID)
	reservation.CreatedAt, reservation.SettledAt = unixTime(createdAt), nullableTime(settledAt)
	return reservation, true, nil
}

func equalOptionalTime(left, right *time.Time) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}
