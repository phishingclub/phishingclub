package model

// MaxRecipientFieldValues is the most values the field values of groups list
const MaxRecipientFieldValues = 500

// RecipientFieldValue is one value of a recipient field as a language route
// matches it: the normalized key, the values recipients have that normalize
// to it, and how many recipients have them
type RecipientFieldValue struct {
	Key    string   `json:"key"`
	Values []string `json:"values"`
	Count  int      `json:"count"`
}

// RecipientFieldValues are the values of one recipient field over the
// members of groups, the most common first. At most MaxRecipientFieldValues
// are listed, Truncated tells when there were more and Total is how many
// there were (when there are very many values it counts each value it did not
// read as one, so it can be a little high). EmptyCount is how many members
// have no value. UnlistedCount is how many members have a value that is not
// listed, so members with a listed value, no value or an unlisted value add
// up to all members.
type RecipientFieldValues struct {
	Field         string                 `json:"field"`
	Values        []*RecipientFieldValue `json:"values"`
	Total         int                    `json:"total"`
	Truncated     bool                   `json:"truncated"`
	EmptyCount    int                    `json:"emptyCount"`
	UnlistedCount int                    `json:"unlistedCount"`
}
