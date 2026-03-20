package paginate

import (
	"net/url"
	"strconv"
	"strings"
)

const DefaultLimit = 50

type Params struct {
	Active bool
	Offset int
	Limit  int
}

func ParseParams(uri string) (string, Params) {
	base, query, hasQuery := strings.Cut(uri, "?")
	if !hasQuery {
		return uri, Params{}
	}

	values, err := url.ParseQuery(query)
	if err != nil {
		return uri, Params{}
	}

	offsetStr := values.Get("offset")
	limitStr := values.Get("limit")

	values.Del("offset")
	values.Del("limit")
	clean := base
	if remaining := values.Encode(); remaining != "" {
		clean = base + "?" + remaining
	}

	if offsetStr == "" {
		return clean, Params{}
	}

	offset, err := strconv.Atoi(offsetStr)
	if err != nil {
		return clean, Params{}
	}

	limit := DefaultLimit
	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
		}
	}

	return clean, Params{Active: true, Offset: offset, Limit: limit}
}
