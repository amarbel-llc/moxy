package main

import (
	"fmt"
	"os"
	"strings"
)

// editFile performs exact string replacement in a file. Returns the number of
// replacements made. Errors if old_string is not found, or if old_string is
// ambiguous (multiple matches) and replaceAll is false.
func editFile(path, oldStr, newStr string, replaceAll bool) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}

	content := string(data)
	count := strings.Count(content, oldStr)

	if count == 0 {
		return 0, fmt.Errorf("no match found for the provided old_string in %s", path)
	}

	if count > 1 && !replaceAll {
		return 0, fmt.Errorf(
			"%d matches found for old_string in %s — use replace_all or provide more context to make the match unique",
			count, path,
		)
	}

	var result string
	if replaceAll {
		result = strings.ReplaceAll(content, oldStr, newStr)
	} else {
		result = strings.Replace(content, oldStr, newStr, 1)
	}

	if err := atomicWrite(path, []byte(result)); err != nil {
		return 0, fmt.Errorf("writing %s: %w", path, err)
	}

	if replaceAll {
		return count, nil
	}
	return 1, nil
}
