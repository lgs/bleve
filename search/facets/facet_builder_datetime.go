//  Copyright (c) 2014 Couchbase, Inc.
//  Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file
//  except in compliance with the License. You may obtain a copy of the License at
//    http://www.apache.org/licenses/LICENSE-2.0
//  Unless required by applicable law or agreed to in writing, software distributed under the
//  License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND,
//  either express or implied. See the License for the specific language governing permissions
//  and limitations under the License.

package facets

import (
	"container/list"
	"time"

	"github.com/blevesearch/bleve/index"
	"github.com/blevesearch/bleve/numeric_util"
	"github.com/blevesearch/bleve/search"
)

type dateTimeRange struct {
	start time.Time
	end   time.Time
}

type DateTimeFacetBuilder struct {
	size       int
	field      string
	termsCount map[string]int
	total      int
	missing    int
	ranges     map[string]*dateTimeRange
}

func NewDateTimeFacetBuilder(field string, size int) *DateTimeFacetBuilder {
	return &DateTimeFacetBuilder{
		size:       size,
		field:      field,
		termsCount: make(map[string]int),
		ranges:     make(map[string]*dateTimeRange, 0),
	}
}

func (fb *DateTimeFacetBuilder) AddRange(name string, start, end time.Time) {
	r := dateTimeRange{
		start: start,
		end:   end,
	}
	fb.ranges[name] = &r
}

func (fb *DateTimeFacetBuilder) Update(ft index.FieldTerms) {
	terms, ok := ft[fb.field]
	if ok {
		for _, term := range terms {
			// only consider the values which are shifted 0
			prefixCoded := numeric_util.PrefixCoded(term)
			shift, err := prefixCoded.Shift()
			if err == nil && shift == 0 {
				i64, err := prefixCoded.Int64()
				if err == nil {
					t := time.Unix(0, i64)

					// look at each of the ranges for a match
					for rangeName, r := range fb.ranges {

						if (r.start.IsZero() || t.After(r.start) || t.Equal(r.start)) && (r.end.IsZero() || t.Before(r.end)) {

							existingCount, existed := fb.termsCount[rangeName]
							if existed {
								fb.termsCount[rangeName] = existingCount + 1
							} else {
								fb.termsCount[rangeName] = 1
							}
							fb.total++
						}
					}
				}
			}
		}
	} else {
		fb.missing++
	}
}

func (fb *DateTimeFacetBuilder) Result() search.FacetResult {
	rv := search.FacetResult{
		Field:   fb.field,
		Total:   fb.total,
		Missing: fb.missing,
	}

	// FIXME better implementation needed here this is quick and dirty
	topN := list.New()

	// walk entries and find top N
OUTER:
	for term, count := range fb.termsCount {
		dateRange := fb.ranges[term]
		tf := &search.DateRangeFacet{
			Name:  term,
			Count: count,
		}
		if !dateRange.start.IsZero() {
			start := dateRange.start.Format(time.RFC3339Nano)
			tf.Start = &start
		}
		if !dateRange.end.IsZero() {
			end := dateRange.end.Format(time.RFC3339Nano)
			tf.End = &end
		}

		for e := topN.Front(); e != nil; e = e.Next() {
			curr := e.Value.(*search.DateRangeFacet)
			if tf.Count < curr.Count {

				topN.InsertBefore(tf, e)
				// if we just made the list too long
				if topN.Len() > fb.size {
					// remove the head
					topN.Remove(topN.Front())
				}
				continue OUTER
			}
		}
		// if we got to the end, we still have to add it
		topN.PushBack(tf)
		if topN.Len() > fb.size {
			// remove the head
			topN.Remove(topN.Front())
		}

	}

	// we now have the list of the top N facets
	rv.DateRanges = make([]*search.DateRangeFacet, topN.Len())
	i := 0
	notOther := 0
	for e := topN.Back(); e != nil; e = e.Prev() {
		rv.DateRanges[i] = e.Value.(*search.DateRangeFacet)
		i++
		notOther += e.Value.(*search.DateRangeFacet).Count
	}
	rv.Other = fb.total - notOther

	return rv
}
