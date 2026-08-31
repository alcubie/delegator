// Package run holds the functions needed for initiating a run of a ticket6
package run

import (
	"fmt"

	"github.com/gosimple/slug"
)

// branchPrefix is the prefix attached to all branch names
const branchPrefix = "delegator"

// maxSlug is the max length of the branch slug that can be returned
const maxSlug = 40

func init() { slug.MaxLength = maxSlug }

// branch returns the branch name for a ticket given its id and title.
// The slugified title is limited to at most 40 characters and will be only ASCII
// characters.
func branch(id int64, title string) string {
	generated := slug.Make(title)
	if generated == "" {
		return fmt.Sprintf("%s/%d", branchPrefix, id)
	}

	return fmt.Sprintf("%s/%d-%s", branchPrefix, id, generated)
}
