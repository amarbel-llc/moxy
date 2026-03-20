package paginate

import (
	"encoding/json"
	"errors"
	"net/url"
	"strconv"
	"strings"
)

const DefaultLimit = 50

var (
	ErrNotArray  = errors.New("content is not a JSON array")
	ErrNotActive = errors.New("pagination not active")
)

type Params struct {
	Active bool
	Offset int
	Limit  int
}

type Result struct {
	Items  json.RawMessage `json:"items"`
	Total  int             `json:"total"`
	Offset int             `json:"offset"`
	Limit  int             `json:"limit"`
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

func SliceArray(text string, params Params) (*Result, error) {
	if !params.Active {
		return nil, ErrNotActive
	}

	var items []json.RawMessage
	if err := json.Unmarshal([]byte(text), &items); err != nil {
		return nil, ErrNotArray
	}

	total := len(items)
	start := params.Offset
	if start > total {
		start = total
	}
	end := start + params.Limit
	if end > total {
		end = total
	}

	page := items[start:end]
	pageJSON, err := json.Marshal(page)
	if err != nil {
		return nil, err
	}

	return &Result{
		Items:  pageJSON,
		Total:  total,
		Offset: params.Offset,
		Limit:  params.Limit,
	}, nil
}
