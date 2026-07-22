package catalog

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

func sortSnapshots(snaps []Snapshot) {
	sort.SliceStable(snaps, func(i, j int) bool {
		if snaps[i].Timestamp.Equal(snaps[j].Timestamp) {
			return snaps[i].ID < snaps[j].ID
		}
		return snaps[i].Timestamp.Before(snaps[j].Timestamp)
	})
}

// Selector describes how to resolve a snapshot for restore/exploration.
//
// Exactly one of ID, At, or Event should drive the resolution:
//   - ID: an explicit snapshot id (or "latest").
//   - At: the latest snapshot at or before a timestamp (point-in-time).
//   - Event: the latest snapshot carrying an event label, optionally further
//     constrained to at/before Before (event-relative-to-a-timestamp).
type Selector struct {
	ID     string
	At     *time.Time
	Event  string
	Before *time.Time
}

// Resolve selects a snapshot from snaps (assumed sorted oldest-first) per sel.
func Resolve(snaps []Snapshot, sel Selector) (Snapshot, error) {
	if len(snaps) == 0 {
		return Snapshot{}, fmt.Errorf("catalog: repository has no snapshots")
	}
	switch {
	case sel.Event != "":
		var match *Snapshot
		for i := range snaps {
			s := snaps[i]
			if s.EventLabel != sel.Event {
				continue
			}
			if sel.Before != nil && s.Timestamp.After(*sel.Before) {
				continue
			}
			cp := s
			match = &cp // snaps sorted ascending: keep latest match
		}
		if match == nil {
			return Snapshot{}, fmt.Errorf("catalog: no snapshot for event %q", sel.Event)
		}
		return *match, nil

	case sel.At != nil:
		var match *Snapshot
		for i := range snaps {
			if snaps[i].Timestamp.After(*sel.At) {
				break
			}
			cp := snaps[i]
			match = &cp
		}
		if match == nil {
			return Snapshot{}, fmt.Errorf("catalog: no snapshot at or before %s", sel.At.Format(time.RFC3339))
		}
		return *match, nil

	case sel.ID == "" || sel.ID == "latest":
		return snaps[len(snaps)-1], nil

	default:
		// Exact id wins.
		for _, s := range snaps {
			if s.ID == sel.ID {
				return s, nil
			}
		}
		// Otherwise accept an unambiguous prefix (git-style).
		var matches []Snapshot
		for _, s := range snaps {
			if strings.HasPrefix(s.ID, sel.ID) {
				matches = append(matches, s)
			}
		}
		switch len(matches) {
		case 1:
			return matches[0], nil
		case 0:
			return Snapshot{}, fmt.Errorf("catalog: snapshot %q not found", sel.ID)
		default:
			return Snapshot{}, fmt.Errorf("catalog: snapshot prefix %q is ambiguous (%d matches)", sel.ID, len(matches))
		}
	}
}
