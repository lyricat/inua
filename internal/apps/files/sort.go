package files

import (
	"cmp"
	"slices"
)

// SortBy is what a folder is sorted by.
type SortBy string

// GroupBy separates a folder's entries into labelled sections.
type GroupBy string

const (
	GroupNone     GroupBy = ""
	GroupType     GroupBy = "type"
	GroupModified GroupBy = "modified"
	GroupSize     GroupBy = "size"
)

func (g GroupBy) Valid() bool {
	return g == GroupNone || g == GroupType || g == GroupModified || g == GroupSize
}

const (
	ByName     SortBy = "name"
	BySize     SortBy = "size"
	ByModified SortBy = "modified"
	ByKind     SortBy = "kind"
	ByCreated  SortBy = "created"
	ByOpened   SortBy = "opened"
	ByPerms    SortBy = "permissions"
	ByOwner    SortBy = "owner"
	ByGroup    SortBy = "group"
)

// Order is how a folder is sorted and grouped.
type Order struct {
	By          SortBy  `json:"by"`
	GroupBy     GroupBy `json:"group_by,omitempty"` // view grouping, after sorting
	Reverse     bool    `json:"reverse,omitempty"`
	FilesFirst  bool    `json:"files_first,omitempty"` // folders are first unless set
	ShowsHidden bool    `json:"hidden,omitempty"`
}

// Sort sorts entries in place. Folders come first (unless FilesFirst),
// and ties fall back to the name, so the order is always the same. Reverse
// turns the order of the chosen key, not the folders' place.
func Sort(es []Entry, o Order) {
	slices.SortFunc(es, func(a, b Entry) int { return compare(&a, &b, o) })
}

func compare(a, b *Entry, o Order) int {
	if a.Dir != b.Dir {
		if a.Dir != o.FilesFirst {
			return -1
		}
		return 1
	}
	var c int
	switch o.By {
	case BySize:
		c = cmp.Compare(a.Size, b.Size)
	case ByModified:
		c = a.ModTime.Compare(b.ModTime)
	case ByKind:
		c = cmp.Compare(a.ext, b.ext)
	case ByCreated:
		c = a.Created.Compare(b.Created)
	case ByOpened:
		c = a.Opened.Compare(b.Opened)
	case ByPerms:
		c = cmp.Compare(a.Mode.Perm(), b.Mode.Perm())
	case ByOwner:
		c = cmp.Compare(a.UID, b.UID)
	case ByGroup:
		c = cmp.Compare(a.GID, b.GID)
	}
	if c == 0 {
		c = Natural(a.key, b.key)
		if c == 0 {
			c = cmp.Compare(a.Name, b.Name)
		}
	}
	if o.Reverse {
		return -c
	}
	return c
}

// Natural compares two folded names, digit runs by their value: "file2"
// before "file10". It does not allocate.
func Natural(a, b string) int {
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		ca, cb := a[i], b[j]
		if isDigit(ca) && isDigit(cb) {
			// Skip leading zeros, then the longer run is the larger number.
			si, sj := i, j
			for si < len(a) && a[si] == '0' {
				si++
			}
			for sj < len(b) && b[sj] == '0' {
				sj++
			}
			ei, ej := si, sj
			for ei < len(a) && isDigit(a[ei]) {
				ei++
			}
			for ej < len(b) && isDigit(b[ej]) {
				ej++
			}
			if c := cmp.Compare(ei-si, ej-sj); c != 0 {
				return c
			}
			if c := cmp.Compare(a[si:ei], b[sj:ej]); c != 0 {
				return c
			}
			i, j = ei, ej
			continue
		}
		if ca != cb {
			return cmp.Compare(ca, cb)
		}
		i++
		j++
	}
	return cmp.Compare(len(a)-i, len(b)-j)
}

func isDigit(c byte) bool { return '0' <= c && c <= '9' }
