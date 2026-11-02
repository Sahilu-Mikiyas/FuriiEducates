package httpapi

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// StartMaintenance purges expired sessions and old password reset links once
// at start and then on every interval until ctx is cancelled.
func StartMaintenance(ctx context.Context, db *pgxpool.Pool, log *slog.Logger, every time.Duration) {
	go func() {
		run := func() {
			sessions, resets, err := purgeExpiredRecords(ctx, db)
			if err != nil {
				if ctx.Err() == nil {
					log.Error("maintenance failed", "error", err)
				}
				return
			}
			if sessions > 0 || resets > 0 {
				log.Info("maintenance purged expired records", "sessions", sessions, "password_reset_links", resets)
			}
		}
		run()
		ticker := time.NewTicker(every)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				run()
			}
		}
	}()
}

// StartAttemptFinalizer grades attempts whose deadline has passed, once at
// start and then on every interval until ctx is cancelled. A sleeping free-tier
// instance catches up when it wakes.
func StartAttemptFinalizer(ctx context.Context, db *pgxpool.Pool, log *slog.Logger, every time.Duration) {
	go func() {
		run := func() {
			count, err := finalizeExpiredAttempts(ctx, db)
			if err != nil {
				if ctx.Err() == nil {
					log.Error("finalizing expired attempts failed", "error", err)
				}
			} else if count > 0 {
				log.Info("finalized expired attempts", "attempts", count)
			}
			practice, err := finalizeExpiredPrepSessions(ctx, db)
			if err != nil {
				if ctx.Err() == nil {
					log.Error("closing expired practice failed", "error", err)
				}
			} else if practice > 0 {
				log.Info("closed expired practice", "sessions", practice)
			}
		}
		run()
		ticker := time.NewTicker(every)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				run()
			}
		}
	}()
}

// purgeExpiredRecords deletes sessions that expired or were revoked more than
// a day ago, and password reset links that expired more than 30 days ago.
func purgeExpiredRecords(ctx context.Context, db *pgxpool.Pool) (int64, int64, error) {
	sessions, err := db.Exec(ctx, `DELETE FROM sessions WHERE expires_at < now() - interval '1 day' OR revoked_at < now() - interval '1 day'`)
	if err != nil {
		return 0, 0, err
	}
	resets, err := db.Exec(ctx, `DELETE FROM password_reset_tokens WHERE expires_at < now() - interval '30 days'`)
	if err != nil {
		return sessions.RowsAffected(), 0, err
	}
	return sessions.RowsAffected(), resets.RowsAffected(), nil
}
