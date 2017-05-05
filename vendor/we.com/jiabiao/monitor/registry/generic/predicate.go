package generic

import (
	log "github.com/golang/glog"
	"we.com/jiabiao/common/fields"
	"we.com/jiabiao/common/labels"
)

// AttrFunc returns label and field sets for List or Watch to match.
// In any failure to parse given object, it returns error.
type AttrFunc func(obj interface{}) (labels.Set, fields.Set, error)

// MatchValue defines a pair (<index name>, <value for that index>).
type MatchValue struct {
	IndexName string
	Value     string
}

// SelectionPredicate is used to represent the way to select objects from api storage.
type SelectionPredicate struct {
	Label       labels.Selector
	Field       fields.Selector
	GetAttrs    AttrFunc
	IndexFields []string
}

// Matches returns true if the given object's labels and fields (as
// returned by s.GetAttrs) match s.Label and s.Field. An error is
// returned if s.GetAttrs fails.
func (s *SelectionPredicate) Matches(obj interface{}) (bool, error) {
	if s.Label.Empty() && s.Field.Empty() {
		return true, nil
	}
	labels, fields, err := s.GetAttrs(obj)
	if err != nil {
		return false, err
	}
	matched := s.Label.Matches(labels)
	if s.Field != nil {
		matched = (matched && s.Field.Matches(fields))
	}
	return matched, nil
}

// MatchesSingle will return (name, true) if and only if s.Field matches on the object's
// name.
func (s *SelectionPredicate) MatchesSingle() (string, bool) {
	// TODO: should be namespace.name
	if name, ok := s.Field.RequiresExactMatch("metadata.name"); ok {
		return name, true
	}
	return "", false
}

// MatcherIndex For any index defined by IndexFields, if a matcher can match only (a subset)
// of objects that return <value> for a given index, a pair (<index name>, <value>)
// wil be returned.
// TODO: Consider supporting also labels.
func (s *SelectionPredicate) MatcherIndex() []MatchValue {
	var result []MatchValue
	for _, field := range s.IndexFields {
		if value, ok := s.Field.RequiresExactMatch(field); ok {
			result = append(result, MatchValue{IndexName: field, Value: value})
		}
	}
	return result
}

// SimpleFilter converts a selection predicate into a FilterFunc.
// It ignores any error from Matches().
func SimpleFilter(p SelectionPredicate) FilterFunc {
	return func(obj interface{}) bool {
		matches, err := p.Matches(obj)
		if err != nil {
			log.Errorf("invalid object for matching. Obj: %v. Err: %v", obj, err)
			return false
		}
		return matches
	}
}

// Everything accepts all objects.
var Everything = SelectionPredicate{
	Label: labels.Everything(),
	Field: fields.Everything(),
}
