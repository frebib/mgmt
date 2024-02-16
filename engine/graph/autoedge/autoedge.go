// Mgmt
// Copyright (C) 2013-2024+ James Shubin and the project contributors
// Written by James Shubin <james@shubin.ca> and the project contributors
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with this program.  If not, see <http://www.gnu.org/licenses/>.

package autoedge

import (
	"bytes"
	"crypto/sha256"
	"encoding/gob"
	"fmt"

	"github.com/purpleidea/mgmt/engine"
	"github.com/purpleidea/mgmt/pgraph"
	"github.com/purpleidea/mgmt/util/errwrap"
)

type AutoEdger struct {
	Logf  func(format string, v ...any) error
	Debug bool
}

// AutoEdge adds the automatic edges to the graph.
func AutoEdge(graph *pgraph.Graph, debug bool, logf func(format string, v ...interface{})) error {
	logf("adding autoedges...")

	// initially get all the autoedges to seek out all possible errors
	uidMap := make(map[[sha256.Size]byte]engine.EdgeableRes)
	autoEdgeMap := make(map[engine.EdgeableRes]engine.AutoEdge)

	var err error
	for _, v := range graph.VerticesSorted() {
		res, ok := v.(engine.EdgeableRes)
		if !ok {
			continue
		}
		// Even if this resource doesn't want to autogroup to anything else,
		// things may want to autogroup to it, so we need to track the uids
		for _, uid := range res.UIDs() {
			key, e := mapKey(uid)
			if e != nil {
				err = errwrap.Append(err, e)
				break
			}
			uidMap[key] = res
		}

		meta := res.AutoEdgeMeta()
		if meta != nil && meta.Disabled {
			// skip if this res has autoedges disabled
			continue
		}

		autoedges, e := res.AutoEdges()
		if e != nil {
			err = errwrap.Append(err, e) // collect all errors
			continue
		}
		if autoedges != nil {
			autoEdgeMap[res] = autoedges
		}
	}
	if err != nil {
		return errwrap.Wrapf(err, "the auto edges had errors")
	}

	for res, autoedges := range autoEdgeMap {
		for uids := autoedges.Next(); uids != nil; uids = autoedges.Next() { // while the autoEdgeObj has more uids to add...
			if debug {
				logf("autoedge: UIDs for %s:", res.String())
				for i, u := range uids {
					logf("autoedge: UID#%d: %v", i, u)
				}
			}

			// match and add edges
			var results []bool

			// loop through each uid, and see if it matches any vertex
			for _, uid := range uids {
				key, err := mapKey(uid)
				if err != nil {
					return err
				}
				other, found := uidMap[key]
				results = append(results, found)

				if found {
					txt := fmt.Sprintf("%s -> %s (autoedge)", res, other)
					logf("autoedge: adding: %s", txt)
					edge := &engine.Edge{Name: txt}
					graph.AddEdge(res, other, edge)
					break
				}
			}

			// report back, and find out if we should continue
			if !autoedges.Test(results) {
				break
			}
		}
	}

	// It would be great to ensure we didn't add any graph cycles here, but
	// instead of checking now, we'll move the check into the main loop.
	return nil
}

func mapKey(uid engine.ResUID) ([sha256.Size]byte, error) {
	var buffer bytes.Buffer
	err := gob.NewEncoder(&buffer).Encode(uid)
	if err != nil {
		return [sha256.Size]byte{0}, err
	}
	return sha256.Sum256(buffer.Bytes()), nil
}
