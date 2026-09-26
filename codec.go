// Copyright (c) 2026 Visvasity LLC

package zanzibar

import (
	"bytes"
	"encoding/gob"
)

// gcRecord is the value stored at a deletion tombstone (§8.5). It marks that an
// object was deleted so the garbage collector can sweep dangling references to it.
type gcRecord struct {
	DeletedAtUnixNano int64
}

// tupleMeta is the value stored for each tuple record (§11.3). It is small and
// never part of tuple identity. CreatedAtUnixNano never affects Check; the
// interval bounds gate membership per §6.6.
type tupleMeta struct {
	CreatedAtUnixNano int64
	NotBeforeUnixNano int64
	NotAfterUnixNano  int64
}

// hasInterval reports whether the tuple carries a validity interval on either
// side.
func (m tupleMeta) hasInterval() bool {
	return m.NotBeforeUnixNano != 0 || m.NotAfterUnixNano != 0
}

// activeAt reports whether a direct tuple with this metadata is active at the
// evaluation time asOf (§6.6). A tuple with no interval is always active. When
// an interval is present but asOf is not supplied (asOf <= 0), the tuple is
// treated as inactive (fail-closed). Otherwise the tuple is active iff
// asOf lies in the half-open window [NotBefore, NotAfter), with a 0 bound
// meaning unbounded on that side.
func (m tupleMeta) activeAt(asOf int64) bool {
	if !m.hasInterval() {
		return true
	}
	if asOf <= 0 {
		return false
	}
	if m.NotBeforeUnixNano != 0 && asOf < m.NotBeforeUnixNano {
		return false
	}
	if m.NotAfterUnixNano != 0 && asOf >= m.NotAfterUnixNano {
		return false
	}
	return true
}

// validInterval checks that a validity interval is well-formed (§8.4): bounds
// must be non-negative, and when both are set the lower bound must be strictly
// less than the upper bound.
func validInterval(notBefore, notAfter int64) error {
	if notBefore < 0 || notAfter < 0 {
		return &Error{Code: CodeInvalidArgument, Message: "validity interval bounds must not be negative"}
	}
	if notBefore != 0 && notAfter != 0 && notBefore >= notAfter {
		return &Error{Code: CodeInvalidArgument, Message: "validity interval: not-before must be < not-after"}
	}
	return nil
}

// gobEncode marshals v to gob-encoded bytes.
func gobEncode(v any) ([]byte, error) {
	var b bytes.Buffer
	if err := gob.NewEncoder(&b).Encode(v); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// gobDecode unmarshals gob-encoded bytes into v (a pointer).
func gobDecode(data []byte, v any) error {
	return gob.NewDecoder(bytes.NewReader(data)).Decode(v)
}
