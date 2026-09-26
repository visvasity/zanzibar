// Copyright (c) 2026 Visvasity LLC

package zanzibar

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"

	"github.com/visvasity/kv"
)

// getGob reads key through the caller-supplied reader and gob-decodes it into v
// (a pointer). It returns found=false with a nil error when the key does not
// exist, so callers can distinguish "absent" from a real failure. Every read in
// one operation MUST pass the same reader — a single kv.Snapshot for read
// operations, or the operation's kv.Transaction for read-modify-write — so all
// reads observe one consistent point-in-time view (§6.1, §9).
func getGob(ctx context.Context, r kv.Getter, key string, v any) (found bool, err error) {
	rd, err := r.Get(ctx, key)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, fromKV(err)
	}
	data, err := io.ReadAll(rd)
	if err != nil {
		return false, fromKV(err)
	}
	if err := gobDecode(data, v); err != nil {
		return false, err
	}
	return true, nil
}

// setGob gob-encodes v and writes it at key through the operation's writer
// (a kv.Transaction).
func setGob(ctx context.Context, w kv.Setter, key string, v any) error {
	data, err := gobEncode(v)
	if err != nil {
		return err
	}
	if err := w.Set(ctx, key, bytes.NewReader(data)); err != nil {
		return fromKV(err)
	}
	return nil
}

// del removes key through the operation's writer. Deleting an absent key is not
// an error.
func del(ctx context.Context, w kv.Deleter, key string) error {
	if err := w.Delete(ctx, key); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fromKV(err)
	}
	return nil
}

// scanGob iterates the half-open key range [beg, end) through reader r in
// ascending key order, gob-decoding each value into a fresh T. It is paired with
// the pure *Range helpers, which compute the bounds. The reader must be the
// operation's shared snapshot/transaction.
func scanGob[T any](ctx context.Context, r kv.Ranger, beg, end string) ([]T, error) {
	var out []T
	var ierr error
	for _, rd := range r.Ascend(ctx, beg, end, &ierr) {
		data, err := io.ReadAll(rd)
		if err != nil {
			return nil, fromKV(err)
		}
		var v T
		if err := gobDecode(data, &v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	if ierr != nil {
		return nil, fromKV(ierr)
	}
	return out, nil
}

// inTx runs fn inside a single transaction and commits it, retrying the whole
// closure on a retryable commit conflict up to the configured limit (§9.3). fn
// receives the transaction as both reader and writer, so its reads participate
// in conflict detection. An error returned by fn aborts immediately (no retry):
// application errors such as a compare-and-set mismatch are the caller's to
// surface, not transient conflicts to retry.
func (s *Service) inTx(ctx context.Context, fn func(tx kv.Transaction) error) error {
	var lastCommitErr error
	for attempt := 0; attempt <= s.opts.commitRetries; attempt++ {
		tx, err := s.db.NewTransaction(ctx)
		if err != nil {
			return fromKV(err)
		}
		if err := fn(tx); err != nil {
			_ = tx.Rollback(ctx)
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			_ = tx.Rollback(ctx)
			if !isRetryableCommitErr(err) {
				return fromKV(err)
			}
			lastCommitErr = err
			continue
		}
		return nil
	}
	return &Error{Code: CodeConflict, Message: "transaction conflict after retries: " + lastCommitErr.Error()}
}

// isRetryableCommitErr reports whether a commit error is a transient conflict
// worth retrying. Misuse errors (invalid/closed transaction) are not retryable.
func isRetryableCommitErr(err error) bool {
	if errors.Is(err, os.ErrInvalid) || errors.Is(err, os.ErrClosed) {
		return false
	}
	return true
}
