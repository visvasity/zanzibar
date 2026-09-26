// Copyright (c) 2026 Visvasity LLC

package zanzibar

import (
	"context"
	"strings"
	"time"

	"github.com/visvasity/kv"
)

// DefaultGCBatch is the number of deleted objects [Service.CollectGarbage]
// processes per call when the caller passes max <= 0.
const DefaultGCBatch = 100

// CollectGarbage processes up to max deleted objects from the deletion log
// (§8.5), removing dangling references to each — tuples elsewhere that name the
// deleted object as a subject (a userset `X#r@object#rel` or a bare-object
// subject `X#r@object`). It returns the number of tombstones processed; call it
// again (or use [Service.RunGarbageCollector]) until it returns 0. It is a no-op
// unless the Service was constructed with [WithDeletionLog]. max <= 0 uses
// [DefaultGCBatch].
//
// An object that has been re-created since deletion (its tombstone cleared by a
// grant) is skipped, and its references are preserved; the collector reads the
// tombstone inside the same transaction, so a concurrent re-create is serialized
// against it.
func (s *Service) CollectGarbage(ctx context.Context, max int) (int, error) {
	if max <= 0 {
		max = DefaultGCBatch
	}
	prefix := s.opts.keyPrefix

	snap, err := s.db.NewSnapshot(ctx)
	if err != nil {
		return 0, fromKV(err)
	}
	beg, end := gcRange(prefix)
	var objects []string
	var ierr error
	for k := range snap.Ascend(ctx, beg, end, &ierr) {
		objects = append(objects, strings.TrimPrefix(k, prefix+classGC))
		if len(objects) >= max {
			break
		}
	}
	snap.Discard(ctx)
	if ierr != nil {
		return 0, fromKV(ierr)
	}

	processed := 0
	for _, object := range objects {
		if err := s.collectObject(ctx, object); err != nil {
			return processed, err
		}
		processed++
	}
	return processed, nil
}

// RunGarbageCollector drains the deletion log, then repeats every interval until
// ctx is done. It is meant to be launched in a goroutine as the background GC
// job; it returns ctx.Err() on cancellation, or the first fatal error from
// CollectGarbage.
func (s *Service) RunGarbageCollector(ctx context.Context, interval time.Duration) error {
	for {
		for {
			n, err := s.CollectGarbage(ctx, 0)
			if err != nil {
				return err
			}
			if n == 0 {
				break
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(interval):
		}
	}
}

// collectObject sweeps references to one deleted object within a single
// transaction. Reading the tombstone joins the transaction's read set, so a
// concurrent grant that revives the object (clearing the tombstone) conflicts and
// the sweep is retried and then skipped.
func (s *Service) collectObject(ctx context.Context, object string) error {
	prefix := s.opts.keyPrefix
	return s.inTx(ctx, func(tx kv.Transaction) error {
		var rec gcRecord
		found, err := getGob(ctx, tx, gcKey(prefix, object), &rec)
		if err != nil {
			return err
		}
		if !found {
			return nil // already collected or revived
		}
		// References to the object as a bare-object subject.
		bb, be := reverseSubjectRange(prefix, object)
		if err := s.sweepReverse(ctx, tx, bb, be); err != nil {
			return err
		}
		// References to the object as a userset subject.
		ub, ue := reverseUsersetRange(prefix, object)
		if err := s.sweepReverse(ctx, tx, ub, ue); err != nil {
			return err
		}
		return del(ctx, tx, gcKey(prefix, object))
	})
}

// sweepReverse deletes every tuple in the given reverse-index range, from both
// the forward and reverse indexes.
func (s *Service) sweepReverse(ctx context.Context, tx kv.Transaction, beg, end string) error {
	prefix := s.opts.keyPrefix
	type triple struct{ object, relation, subject string }
	var matches []triple
	var ierr error
	for k := range tx.Ascend(ctx, beg, end, &ierr) {
		object, relation, subject, ok := decodeTupleKey(k, prefix, true)
		if !ok {
			continue
		}
		matches = append(matches, triple{object, relation, subject})
	}
	if ierr != nil {
		return fromKV(ierr)
	}
	for _, m := range matches {
		if err := del(ctx, tx, forwardKey(prefix, m.object, m.relation, m.subject)); err != nil {
			return err
		}
		if err := del(ctx, tx, reverseKey(prefix, m.subject, m.object, m.relation)); err != nil {
			return err
		}
	}
	return nil
}
