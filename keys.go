// Copyright (c) 2026 Visvasity LLC

package zanzibar

import (
	"fmt"

	"github.com/visvasity/kv/kvutil"
)

// Record-class discriminators placed immediately after the key prefix (§11.1).
const (
	classConfigHead    = "c/" // c/<namespace>                     -> current NamespaceConfig
	classConfigHistory = "h/" // h/<namespace>/<enc(version)>      -> immutable NamespaceConfig
	classForward       = "t/" // t/<object>/<relation>/<subject>   -> TupleMeta
	classReverse       = "s/" // s/<subject>/<object>/<relation>   -> TupleMeta (or empty)
)

// versionWidth is the fixed decimal width used to encode a uint64 version so
// that history keys sort in ascending numeric order (uint64 max is 20 digits).
const versionWidth = 20

// encodeVersion renders a version as a zero-padded, fixed-width decimal string,
// which is order-preserving under lexicographic comparison (§11.1).
func encodeVersion(v uint64) string {
	return fmt.Sprintf("%0*d", versionWidth, v)
}

// configHeadKey is the key of a namespace's current config record.
func configHeadKey(prefix, namespace string) string {
	return prefix + classConfigHead + namespace
}

// configHistoryKey is the key of a namespace's immutable config record at a
// specific version.
func configHistoryKey(prefix, namespace string, version uint64) string {
	return prefix + classConfigHistory + namespace + "/" + encodeVersion(version)
}

// forwardKey is the key of the forward index record for a tuple.
func forwardKey(prefix, object, relation, subj string) string {
	return prefix + classForward + object + "/" + relation + "/" + subj
}

// reverseKey is the key of the reverse index record for a tuple.
func reverseKey(prefix, subj, object, relation string) string {
	return prefix + classReverse + subj + "/" + object + "/" + relation
}

// configHeadRange spans all namespace head records (for listing namespaces).
func configHeadRange(prefix string) (begin, end string) {
	return kvutil.PrefixRange(prefix + classConfigHead)
}

// configHistoryRange spans all stored versions of one namespace, in ascending
// version order.
func configHistoryRange(prefix, namespace string) (begin, end string) {
	return kvutil.PrefixRange(prefix + classConfigHistory + namespace + "/")
}

// forwardObjectRange spans all forward records for one object (any relation,
// any subject).
func forwardObjectRange(prefix, object string) (begin, end string) {
	return kvutil.PrefixRange(prefix + classForward + object + "/")
}

// forwardRelationRange spans all subjects directly bound to object#relation.
func forwardRelationRange(prefix, object, relation string) (begin, end string) {
	return kvutil.PrefixRange(prefix + classForward + object + "/" + relation + "/")
}

// reverseSubjectRange spans all objects a subject is directly bound to.
func reverseSubjectRange(prefix, subj string) (begin, end string) {
	return kvutil.PrefixRange(prefix + classReverse + subj + "/")
}
